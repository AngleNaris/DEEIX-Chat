package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
	"github.com/google/uuid"
)

const systemMultimodalAnalyzeToolName = "system_multimodal_analyze"

type multimodalDelegationInput struct {
	UserID          uint
	ConversationID  uint
	MessageID       uint
	RequestID       string
	RunID           string
	UserPrompt      string
	Attachments     []AttachmentInput
	MainRoute       *channel.ResolvedRoute
	TraceRecorder   *messageTraceRecorder
	SkipPersistence bool
}

type multimodalDelegationResult struct {
	Routed                bool
	RoutedImage           bool
	HandledFileIDs        map[string]struct{}
	Analyses              []imageAttachmentAnalysis
	Rows                  []domainconversation.ToolCall
	PersistedToolCallKeys map[string]struct{}
}

type multimodalDelegationAuditFile struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MIMEType string `json:"mime_type"`
	Size     int64  `json:"size"`
	Modality string `json:"modality"`
}

func (s *Service) delegateUnsupportedMedia(
	ctx context.Context,
	input multimodalDelegationInput,
) (multimodalDelegationResult, error) {
	result := multimodalDelegationResult{
		HandledFileIDs:        make(map[string]struct{}),
		PersistedToolCallKeys: make(map[string]struct{}),
	}
	if s == nil || s.cfg == nil || input.MainRoute == nil {
		return result, nil
	}
	cfg := s.cfg.Snapshot()
	if !cfg.MultimodalDelegationEnabled {
		return result, nil
	}
	delegateModel := strings.TrimSpace(cfg.MultimodalDelegationModel)
	if delegateModel == "" {
		return result, fmt.Errorf("%w: delegation model is not configured", ErrMultimodalDelegationFailed)
	}
	allowed := parseMultimodalDelegationModalities(cfg.MultimodalDelegationModalities)
	selected := make([]AttachmentInput, 0)
	auditFiles := make([]multimodalDelegationAuditFile, 0)
	for _, attachment := range input.Attachments {
		if !attachment.Current {
			continue
		}
		modality := attachmentMediaModality(attachment)
		if modality == "" {
			continue
		}
		if _, ok := allowed[modality]; !ok {
			continue
		}
		if modelSupportsMedia(input.MainRoute.PlatformModelName, input.MainRoute.ModelCapabilitiesJSON, modality) {
			continue
		}
		selected = append(selected, attachment)
		auditFiles = append(auditFiles, multimodalDelegationAuditFile{
			FileID:   strings.TrimSpace(attachment.FileID),
			FileName: strings.TrimSpace(attachment.FileName),
			MIMEType: firstNonEmptyString(attachment.DetectedMIME, attachment.MimeType),
			Size:     max(attachment.FileSize, 0),
			Modality: modality,
		})
	}
	if len(selected) == 0 {
		return result, nil
	}
	result.Routed = true
	row := domainconversation.ToolCall{
		MessageID:      input.MessageID,
		ConversationID: input.ConversationID,
		UserID:         input.UserID,
		RunID:          input.RunID,
		ToolCallID:     "multimodal_" + normalizePublicID(uuid.NewString()),
		ToolType:       "system_multimodal",
		ToolName:       systemMultimodalAnalyzeToolName,
		Status:         "requested",
		InputJSON:      multimodalDelegationAuditJSON(delegateModel, auditFiles),
	}
	for _, item := range auditFiles {
		result.HandledFileIDs[item.FileID] = struct{}{}
		if item.Modality == "image" {
			result.RoutedImage = true
		}
	}

	route, err := s.routeResolver.ResolveRoute(ctx, channel.ResolveRouteInput{
		PlatformModelName: delegateModel,
		TaskType:          channel.TaskTypeChat,
		Scope:             channel.RouteScopeInternal,
		UserID:            input.UserID,
		ConversationID:    input.ConversationID,
		RequestID:         strings.TrimSpace(input.RequestID),
	})
	if err != nil {
		return s.failMultimodalDelegation(ctx, input, result, row, fmt.Errorf("resolve model %s: %w", delegateModel, err))
	}
	for _, item := range auditFiles {
		if !modelSupportsMedia(route.PlatformModelName, route.ModelCapabilitiesJSON, item.Modality) {
			return s.failMultimodalDelegation(ctx, input, result, row, fmt.Errorf("configured model %s does not declare %s input capability", route.PlatformModelName, item.Modality))
		}
	}

	store, err := s.openMultimodalStore(ctx, cfg)
	if err != nil {
		return s.failMultimodalDelegation(ctx, input, result, row, err)
	}
	parts := []llm.ContentPart{{
		Kind: llm.ContentPartText,
		Text: buildMultimodalDelegationPrompt(input.UserPrompt, auditFiles),
	}}
	for _, attachment := range selected {
		prepared, prepareErr := prepareAttachmentForProcessor(ctx, store, attachment, cfg.ImageMaxDimension, attachmentMediaModality(attachment))
		if prepareErr != nil {
			return s.failMultimodalDelegation(ctx, input, result, row, prepareErr)
		}
		parts = append(parts, llm.ContentPart{
			Kind:     multimodalContentPartKind(attachmentMediaModality(attachment)),
			MimeType: prepared.mimeType,
			Data:     prepared.data,
			FileName: strings.TrimSpace(attachment.FileName),
		})
	}

	timeoutSeconds := cfg.MultimodalDelegationTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	authorization, err := s.authorizeBasicServiceUsage(callCtx, input.UserID, route.PlatformModelName, "multimodal")
	if err != nil {
		return s.failMultimodalDelegation(ctx, input, result, row, fmt.Errorf("authorize usage: %w", err))
	}
	messages := []llm.Message{
		{
			Role:    "system",
			Content: "You are an internal multimodal analysis service. Analyze the supplied media faithfully and return detailed factual observations useful to another model. Preserve visible text, UI state, entities, spatial relationships, timestamps, speech, sounds, actions, and uncertainty. Do not claim to perform the user's external task.",
		},
		{Role: "user", Parts: parts},
	}
	generateInput := buildTextTaskGenerateInput(route, cfg, messages)
	generateInput.RequestID = strings.TrimSpace(input.RequestID)
	generateInput.ConversationID = input.ConversationID
	generateInput.DisableTools = true
	attributionReferer, attributionTitle := s.llmAttribution()
	startedAt := time.Now()
	output, err := s.llmClient.Generate(callCtx, messageRouteConfig(route, attributionReferer, attributionTitle), generateInput)
	if err != nil {
		s.routeResolver.MarkRouteFailure(ctx, route, err)
		releaseErr := s.releaseBasicServiceUsageAuthorization(ctx, authorization)
		if releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		return s.failMultimodalDelegation(ctx, input, result, row, fmt.Errorf("model %s: %w", route.PlatformModelName, err))
	}
	s.routeResolver.MarkRouteSuccess(ctx, route)
	analysis := strings.TrimSpace(output.Text)
	if analysis == "" {
		_ = s.releaseBasicServiceUsageAuthorization(ctx, authorization)
		return s.failMultimodalDelegation(ctx, input, result, row, fmt.Errorf("model %s returned no analysis", route.PlatformModelName))
	}
	if err = s.recordBasicServiceUsage(
		ctx,
		authorization,
		input.UserID,
		input.ConversationID,
		"multimodal",
		"多模态分析",
		route.PlatformModelName,
		route.BindingCode,
		route.Protocol,
		route.UpstreamName,
		route.UpstreamModel,
		"5m",
		output.Usage,
		generateInput.Messages,
		analysis,
		time.Since(startedAt).Milliseconds(),
	); err != nil {
		return s.failMultimodalDelegation(ctx, input, result, row, fmt.Errorf("settle usage: %w", err))
	}

	row.Status = "success"
	row.LatencyMS = max(time.Since(startedAt).Milliseconds(), 0)
	row.OutputJSON = multimodalDelegationStatusJSON(route.PlatformModelName, len(auditFiles), "success")
	s.persistMultimodalDelegationRow(ctx, &row, &result, input.SkipPersistence)
	result.Analyses = append(result.Analyses, imageAttachmentAnalysis{
		Kind:     "media",
		FileID:   strings.Join(multimodalAuditFileIDs(auditFiles), ","),
		FileName: strings.Join(multimodalAuditFileNames(auditFiles), ", "),
		ToolName: systemMultimodalAnalyzeToolName + ":" + route.PlatformModelName,
		Content:  contextArtifactExcerpt(analysis, maxImageAttachmentAnalysisTotalChars),
	})
	if input.TraceRecorder != nil {
		input.TraceRecorder.appendProcessSection(
			fmt.Sprintf("已通过 %s 分析 %d 个媒体附件", route.PlatformModelName, len(auditFiles)),
			formatTraceStep("多模态分析", "不支持对应媒体输入的主模型已接收系统多模态模型的分析结果，并将继续执行原始任务。"),
			map[string]interface{}{
				"model":      route.PlatformModelName,
				"file_count": len(auditFiles),
				"file_ids":   multimodalAuditFileIDs(auditFiles),
				processTracePayloadStage: map[string]interface{}{
					"kind":   "system_multimodal",
					"status": messageTraceStatusCompleted,
				},
			},
			messageTraceStatusCompleted,
		)
	}
	return result, nil
}

func (s *Service) failMultimodalDelegation(
	ctx context.Context,
	input multimodalDelegationInput,
	result multimodalDelegationResult,
	row domainconversation.ToolCall,
	cause error,
) (multimodalDelegationResult, error) {
	row.Status = "error"
	row.ErrorJSON = sanitizeOpaqueToolOutput(cause.Error())
	row.OutputJSON = multimodalDelegationStatusJSON("", len(result.HandledFileIDs), "error")
	s.persistMultimodalDelegationRow(ctx, &row, &result, input.SkipPersistence)
	return result, fmt.Errorf("%w: %v", ErrMultimodalDelegationFailed, cause)
}

func (s *Service) persistMultimodalDelegationRow(
	ctx context.Context,
	row *domainconversation.ToolCall,
	result *multimodalDelegationResult,
	skipPersistence bool,
) {
	if row == nil || result == nil {
		return
	}
	persisted := false
	if !skipPersistence {
		persisted = s.persistToolCallResult(ctx, row)
	}
	result.Rows = append(result.Rows, *row)
	if persisted {
		result.PersistedToolCallKeys[toolCallPersistenceKey(*row)] = struct{}{}
	}
}

func (s *Service) openMultimodalStore(ctx context.Context, cfg config.Config) (objectstore.Store, error) {
	provider := s.storeProvider
	if provider == nil {
		return nil, fmt.Errorf("object storage is unavailable")
	}
	store, err := provider.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("open object storage: %w", err)
	}
	return store, nil
}

func parseMultimodalDelegationModalities(value string) map[string]struct{} {
	result := make(map[string]struct{}, 3)
	for _, raw := range strings.Split(value, ",") {
		switch modality := strings.ToLower(strings.TrimSpace(raw)); modality {
		case "image", "audio", "video":
			result[modality] = struct{}{}
		}
	}
	return result
}

func attachmentMediaModality(attachment AttachmentInput) string {
	mimeType := strings.ToLower(firstNonEmptyString(attachment.DetectedMIME, attachment.MimeType))
	kind := strings.ToLower(normalizeAttachmentKind(attachment.Kind, mimeType))
	switch {
	case kind == "image" || strings.HasPrefix(mimeType, "image/"):
		return "image"
	case kind == "audio" || strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case kind == "video" || strings.HasPrefix(mimeType, "video/"):
		return "video"
	default:
		return ""
	}
}

func multimodalContentPartKind(modality string) string {
	switch modality {
	case "audio":
		return llm.ContentPartAudio
	case "video":
		return llm.ContentPartVideo
	default:
		return llm.ContentPartImage
	}
}

func buildMultimodalDelegationPrompt(userPrompt string, files []multimodalDelegationAuditFile) string {
	names := multimodalAuditFileNames(files)
	return "Analyze the attached media in relation to the user's original request. Return a complete handoff for the primary model.\n\nOriginal request:\n" +
		strings.TrimSpace(userPrompt) + "\n\nFiles:\n- " + strings.Join(names, "\n- ")
}

func multimodalDelegationAuditJSON(model string, files []multimodalDelegationAuditFile) string {
	payload, _ := json.Marshal(map[string]interface{}{
		"model": strings.TrimSpace(model),
		"files": files,
	})
	return string(payload)
}

func multimodalDelegationStatusJSON(model string, fileCount int, status string) string {
	payload, _ := json.Marshal(map[string]interface{}{
		"model":      strings.TrimSpace(model),
		"file_count": fileCount,
		"status":     strings.TrimSpace(status),
	})
	return string(payload)
}

func multimodalAuditFileIDs(files []multimodalDelegationAuditFile) []string {
	result := make([]string, 0, len(files))
	for _, item := range files {
		if value := strings.TrimSpace(item.FileID); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func multimodalAuditFileNames(files []multimodalDelegationAuditFile) []string {
	result := make([]string, 0, len(files))
	for _, item := range files {
		result = append(result, firstNonEmptyString(item.FileName, item.FileID, item.Modality))
	}
	return result
}

func withoutHandledMediaAttachments(plan conversationFileContextPlan, handled map[string]struct{}) conversationFileContextPlan {
	if len(handled) == 0 {
		return plan
	}
	filter := func(items []AttachmentInput) []AttachmentInput {
		result := make([]AttachmentInput, 0, len(items))
		for _, item := range items {
			if _, ok := handled[strings.TrimSpace(item.FileID)]; ok {
				continue
			}
			result = append(result, item)
		}
		return result
	}
	plan.Attachments = filter(plan.Attachments)
	plan.FullAttachments = filter(plan.FullAttachments)
	plan.RAGAttachments = filter(plan.RAGAttachments)
	return plan
}

func withoutHandledAttachments(items []AttachmentInput, handled map[string]struct{}) []AttachmentInput {
	if len(handled) == 0 {
		return items
	}
	result := make([]AttachmentInput, 0, len(items))
	for _, item := range items {
		if _, ok := handled[strings.TrimSpace(item.FileID)]; ok {
			continue
		}
		result = append(result, item)
	}
	return result
}
