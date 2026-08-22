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

const (
	multimodalDelegationStatusOK          = "ok"
	multimodalDelegationStatusUnsupported = "unsupported"
)

var multimodalDelegationJSONSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"status": map[string]interface{}{
			"type": "string",
			"enum": []string{multimodalDelegationStatusOK, multimodalDelegationStatusUnsupported},
		},
		"analysis": map[string]interface{}{
			"type": "string",
		},
	},
	"required":             []string{"status", "analysis"},
	"additionalProperties": false,
}

var systemMultimodalAnalyzeInputSchema = json.RawMessage(`{
	"type":"object",
	"properties":{
		"file_ids":{
			"type":"array",
			"items":{"type":"string","minLength":1},
			"minItems":1,
			"uniqueItems":true,
			"description":"One or more file IDs from the authorized current or historical attachment list. Historical images may be re-inspected when a new angle or verification is needed."
		},
		"prompt":{
			"type":"string",
			"minLength":1,
			"description":"Your precise analysis request. State what facts, text, layout, objects, speech, sounds, motion, or relationships you need from the selected attachments."
		}
	},
	"required":["file_ids","prompt"],
	"additionalProperties":false
}`)

type selectedMultimodalAnalyzer struct {
	mainRoute      *channel.ResolvedRoute
	attachments    map[string]AttachmentInput
	orderedFileIDs []string
	auditFiles     []multimodalDelegationAuditFile
}

type systemMultimodalAnalyzeArguments struct {
	FileIDs []string `json:"file_ids"`
	Prompt  string   `json:"prompt"`
}

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
	AllowHistorical bool
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
	FileID     string `json:"file_id"`
	FileName   string `json:"file_name"`
	MIMEType   string `json:"mime_type"`
	Size       int64  `json:"size"`
	Modality   string `json:"modality"`
	Historical bool   `json:"historical,omitempty"`
}

type multimodalDelegationResponse struct {
	Status   string `json:"status"`
	Analysis string `json:"analysis"`
}

type multimodalDelegationGroup struct {
	Model       string
	Attachments []AttachmentInput
	AuditFiles  []multimodalDelegationAuditFile
	Route       *channel.ResolvedRoute
}

func (r *selectedToolRuntime) bindMultimodalAnalyzer(
	cfg config.Config,
	mainRoute *channel.ResolvedRoute,
	attachments []AttachmentInput,
) bool {
	return r.bindMultimodalAnalyzerWithHistory(cfg, mainRoute, attachments, false)
}

func (r *selectedToolRuntime) bindMultimodalAnalyzerWithHistory(
	cfg config.Config,
	mainRoute *channel.ResolvedRoute,
	attachments []AttachmentInput,
	allowHistorical bool,
) bool {
	if r == nil || !cfg.MultimodalDelegationEnabled || mainRoute == nil {
		return false
	}
	groups := selectMultimodalDelegationGroupsWithHistory(cfg, mainRoute, attachments, allowHistorical)
	analyzer := &selectedMultimodalAnalyzer{
		mainRoute:   mainRoute,
		attachments: make(map[string]AttachmentInput),
	}
	for _, group := range groups {
		for index, attachment := range group.Attachments {
			fileID := strings.TrimSpace(attachment.FileID)
			if fileID == "" {
				continue
			}
			if _, exists := analyzer.attachments[fileID]; exists {
				continue
			}
			analyzer.attachments[fileID] = attachment
			analyzer.orderedFileIDs = append(analyzer.orderedFileIDs, fileID)
			if index < len(group.AuditFiles) {
				analyzer.auditFiles = append(analyzer.auditFiles, group.AuditFiles[index])
			}
		}
	}
	if len(analyzer.attachments) == 0 {
		return false
	}
	r.multimodalAnalyzer = analyzer
	return true
}

func (a *selectedMultimodalAnalyzer) hasCurrentAttachments() bool {
	if a == nil {
		return false
	}
	for _, attachment := range a.attachments {
		if attachment.Current {
			return true
		}
	}
	return false
}

func (r selectedToolRuntime) multimodalAnalyzerHandlesCurrentAttachments() bool {
	return r.multimodalAnalyzer != nil && r.multimodalAnalyzer.hasCurrentAttachments()
}

func (a *selectedMultimodalAnalyzer) toolDefinition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name: systemMultimodalAnalyzeToolName,
		Description: "Analyze selected current or historical image, audio, or video attachments through the system multimodal route. " +
			"The attachments may already have an analysis; only re-inspect when a new angle, correction, or verification is needed. " +
			"Choose only the file IDs needed for the current task and write a precise prompt describing what information you need. " +
			"Use the returned factual analysis to continue the user's original task; do not guess attachment contents from filenames or metadata.",
		InputSchema: systemMultimodalAnalyzeInputSchema,
	}
}

func (r selectedToolRuntime) multimodalAnalyzerGuidance() string {
	if r.multimodalAnalyzer == nil || len(r.multimodalAnalyzer.auditFiles) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("# media_attachments\n")
	builder.WriteString("- The current model cannot directly inspect the attachments listed below. Existing analysis is reference only; call system_multimodal_analyze when a new angle, correction, or verification is needed.\n")
	builder.WriteString("- When attachment contents are needed, call system_multimodal_analyze with only the relevant file_ids and write the prompt yourself for the exact facts needed in the current context.\n")
	builder.WriteString("- A prompt may request OCR/text, visual layout, objects, spatial relationships, speech, sounds, actions, motion, or another focused analysis. Do not infer contents from filenames or metadata.\n")
	builder.WriteString("- Available authorized attachments:\n")
	for _, item := range r.multimodalAnalyzer.auditFiles {
		fmt.Fprintf(
			&builder,
			"  - file_id=%s; scope=%s; modality=%s; name=%s; mime_type=%s\n",
			item.FileID,
			map[bool]string{true: "historical", false: "current"}[item.Historical],
			item.Modality,
			firstNonEmptyString(item.FileName, "unnamed"),
			firstNonEmptyString(item.MIMEType, "unknown"),
		)
	}
	return strings.TrimSpace(builder.String())
}

func (a *selectedMultimodalAnalyzer) resolveAttachments(fileIDs []string) ([]AttachmentInput, error) {
	if a == nil || len(a.attachments) == 0 {
		return nil, errors.New("multimodal analysis is not available for this run")
	}
	seen := make(map[string]struct{}, len(fileIDs))
	result := make([]AttachmentInput, 0, len(fileIDs))
	for _, raw := range fileIDs {
		fileID := strings.TrimSpace(raw)
		if fileID == "" {
			return nil, errors.New("file_ids must not contain empty values")
		}
		if _, duplicate := seen[fileID]; duplicate {
			return nil, fmt.Errorf("duplicate file_id %q", fileID)
		}
		attachment, ok := a.attachments[fileID]
		if !ok {
			return nil, fmt.Errorf("file_id %q is not an authorized conversation attachment for this run", fileID)
		}
		seen[fileID] = struct{}{}
		result = append(result, attachment)
	}
	if len(result) == 0 {
		return nil, errors.New("file_ids must contain at least one authorized conversation attachment")
	}
	return result, nil
}

func (s *Service) executeSystemMultimodalAnalyze(
	ctx context.Context,
	input executeAssistantToolCallsInput,
	argumentsJSON string,
) (string, error) {
	if input.ToolRuntime == nil || input.ToolRuntime.multimodalAnalyzer == nil {
		return "", errors.New("multimodal analysis is not available for this run")
	}
	var arguments systemMultimodalAnalyzeArguments
	if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
		return "", fmt.Errorf("decode multimodal analysis arguments: %w", err)
	}
	arguments.Prompt = strings.TrimSpace(arguments.Prompt)
	if arguments.Prompt == "" {
		return "", errors.New("prompt must not be empty")
	}
	attachments, err := input.ToolRuntime.multimodalAnalyzer.resolveAttachments(arguments.FileIDs)
	if err != nil {
		return "", err
	}
	delegation, err := s.delegateUnsupportedMedia(ctx, multimodalDelegationInput{
		UserID:          input.UserID,
		ConversationID:  input.ConversationID,
		MessageID:       input.MessageID,
		RequestID:       input.RequestID,
		RunID:           input.RunID,
		UserPrompt:      arguments.Prompt,
		Attachments:     attachments,
		MainRoute:       input.ToolRuntime.multimodalAnalyzer.mainRoute,
		TraceRecorder:   input.TraceRecorder,
		SkipPersistence: true,
		AllowHistorical: true,
	})
	if err != nil {
		return "", err
	}
	if !delegation.Routed || len(delegation.Analyses) == 0 {
		return "", errors.New("no selected attachment could be analyzed")
	}
	analyses := make([]map[string]interface{}, 0, len(delegation.Analyses))
	for _, item := range delegation.Analyses {
		analyses = append(analyses, map[string]interface{}{
			"file_ids":   splitNonEmptyCSV(item.FileID),
			"file_names": splitNonEmptyCSV(item.FileName),
			"analysis":   strings.TrimSpace(item.Content),
		})
	}
	output, err := json.Marshal(map[string]interface{}{
		"status":   "success",
		"analyses": analyses,
	})
	if err != nil {
		return "", fmt.Errorf("encode multimodal analysis result: %w", err)
	}
	return string(output), nil
}

func splitNonEmptyCSV(value string) []string {
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
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
	groups := selectMultimodalDelegationGroupsWithHistory(cfg, input.MainRoute, input.Attachments, input.AllowHistorical)
	if len(groups) == 0 {
		return result, nil
	}
	result.Routed = true
	for _, group := range groups {
		for _, item := range group.AuditFiles {
			if item.FileID != "" {
				result.HandledFileIDs[item.FileID] = struct{}{}
			}
			if item.Modality == "image" {
				result.RoutedImage = true
			}
		}
	}

	for index := range groups {
		group := &groups[index]
		row := newMultimodalDelegationRow(input, group.Model, group.AuditFiles)
		if group.Model == "" {
			modality := firstMultimodalAuditModality(group.AuditFiles)
			err := s.failMultimodalDelegation(
				ctx,
				input,
				&result,
				row,
				"",
				len(group.AuditFiles),
				fmt.Errorf("delegation model is not configured for %s input", modality),
			)
			return result, err
		}

		route, err := s.routeResolver.ResolveRoute(ctx, channel.ResolveRouteInput{
			PlatformModelName: group.Model,
			TaskType:          channel.TaskTypeChat,
			Scope:             channel.RouteScopeInternal,
			UserID:            input.UserID,
			ConversationID:    input.ConversationID,
			RequestID:         strings.TrimSpace(input.RequestID),
		})
		if err != nil || route == nil {
			if err == nil {
				err = errors.New("resolved route is empty")
			}
			err = s.failMultimodalDelegation(
				ctx,
				input,
				&result,
				row,
				group.Model,
				len(group.AuditFiles),
				fmt.Errorf("resolve model %s: %w", group.Model, err),
			)
			return result, err
		}
		for _, item := range group.AuditFiles {
			if !modelSupportsMedia(route.PlatformModelName, route.ModelCapabilitiesJSON, item.Modality) {
				err = s.failMultimodalDelegation(
					ctx,
					input,
					&result,
					row,
					route.PlatformModelName,
					len(group.AuditFiles),
					fmt.Errorf("configured model %s does not declare %s input capability", route.PlatformModelName, item.Modality),
				)
				return result, err
			}
			if !llm.SupportsMediaInputAdapter(route.Protocol, item.Modality) {
				err = s.failMultimodalDelegation(
					ctx,
					input,
					&result,
					row,
					route.PlatformModelName,
					len(group.AuditFiles),
					fmt.Errorf("configured model %s uses protocol %s, which cannot carry %s input", route.PlatformModelName, route.Protocol, item.Modality),
				)
				return result, err
			}
		}
		group.Route = route
	}

	store, err := s.openMultimodalStore(ctx, cfg)
	if err != nil {
		var firstFailure error
		for _, group := range groups {
			row := newMultimodalDelegationRow(input, group.Model, group.AuditFiles)
			failure := s.failMultimodalDelegation(ctx, input, &result, row, group.Model, len(group.AuditFiles), err)
			if firstFailure == nil {
				firstFailure = failure
			}
		}
		return result, firstFailure
	}

	for _, group := range groups {
		if err = s.delegateUnsupportedMediaGroup(ctx, input, cfg, store, group, &result); err != nil {
			return result, err
		}
	}

	return result, nil
}

func (s *Service) delegateUnsupportedMediaGroup(
	ctx context.Context,
	input multimodalDelegationInput,
	cfg config.Config,
	store objectstore.Store,
	group multimodalDelegationGroup,
	result *multimodalDelegationResult,
) error {
	route := group.Route
	row := newMultimodalDelegationRow(input, group.Model, group.AuditFiles)
	parts := []llm.ContentPart{{
		Kind: llm.ContentPartText,
		Text: buildMultimodalDelegationPrompt(input.UserPrompt, group.AuditFiles),
	}}
	for _, attachment := range group.Attachments {
		modality := attachmentMediaModality(attachment)
		prepared, prepareErr := prepareAttachmentForProcessor(ctx, store, attachment, cfg.ImageMaxDimension, modality)
		if prepareErr != nil {
			return s.failMultimodalDelegation(ctx, input, result, row, group.Model, len(group.AuditFiles), prepareErr)
		}
		parts = append(parts, llm.ContentPart{
			Kind:     multimodalContentPartKind(modality),
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
	authorization, err := s.authorizeBasicServiceUsage(callCtx, input.UserID, route.PlatformModelName, "multimodal")
	if err != nil {
		cancel()
		return s.failMultimodalDelegation(ctx, input, result, row, route.PlatformModelName, len(group.AuditFiles), fmt.Errorf("authorize usage: %w", err))
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
	generateInput.Options = multimodalDelegationOptions(generateInput.Options)
	attributionReferer, attributionTitle := s.llmAttribution()
	startedAt := time.Now()
	output, err := s.llmClient.Generate(callCtx, messageRouteConfig(route, attributionReferer, attributionTitle), generateInput)
	cancel()
	if err != nil {
		s.routeResolver.MarkRouteFailure(ctx, route, err)
		releaseErr := s.releaseBasicServiceUsageAuthorization(ctx, authorization)
		if releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		return s.failMultimodalDelegation(ctx, input, result, row, route.PlatformModelName, len(group.AuditFiles), fmt.Errorf("model %s: %w", route.PlatformModelName, err))
	}
	analysis, err := resolveMultimodalDelegationOutput(output)
	if err != nil {
		s.routeResolver.MarkRouteFailure(ctx, route, err)
		releaseErr := s.releaseBasicServiceUsageAuthorization(ctx, authorization)
		if releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
		return s.failMultimodalDelegation(ctx, input, result, row, route.PlatformModelName, len(group.AuditFiles), fmt.Errorf("model %s: %w", route.PlatformModelName, err))
	}
	s.routeResolver.MarkRouteSuccess(ctx, route)
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
		return s.failMultimodalDelegation(ctx, input, result, row, route.PlatformModelName, len(group.AuditFiles), fmt.Errorf("settle usage: %w", err))
	}

	row.Status = "success"
	row.LatencyMS = max(time.Since(startedAt).Milliseconds(), 0)
	row.OutputJSON = multimodalDelegationStatusJSON(route.PlatformModelName, len(group.AuditFiles), "success")
	s.persistMultimodalDelegationRow(ctx, &row, result, input.SkipPersistence)
	result.Analyses = append(result.Analyses, imageAttachmentAnalysis{
		Kind:     "media",
		FileID:   strings.Join(multimodalAuditFileIDs(group.AuditFiles), ","),
		FileName: strings.Join(multimodalAuditFileNames(group.AuditFiles), ", "),
		ToolName: systemMultimodalAnalyzeToolName + ":" + route.PlatformModelName,
		Content:  contextArtifactExcerpt(analysis, maxImageAttachmentAnalysisTotalChars),
	})
	if input.TraceRecorder != nil {
		input.TraceRecorder.appendProcessSection(
			fmt.Sprintf("已通过 %s 分析 %d 个媒体附件", route.PlatformModelName, len(group.AuditFiles)),
			formatTraceStep("多模态分析", "不支持对应媒体输入的主模型已接收系统多模态模型的分析结果，并将继续执行原始任务。"),
			map[string]interface{}{
				"model":      route.PlatformModelName,
				"file_count": len(group.AuditFiles),
				"file_ids":   multimodalAuditFileIDs(group.AuditFiles),
				processTracePayloadStage: map[string]interface{}{
					"kind":   "system_multimodal",
					"status": messageTraceStatusCompleted,
				},
			},
			messageTraceStatusCompleted,
		)
	}
	return nil
}

func newMultimodalDelegationRow(
	input multimodalDelegationInput,
	model string,
	files []multimodalDelegationAuditFile,
) domainconversation.ToolCall {
	return domainconversation.ToolCall{
		MessageID:      input.MessageID,
		ConversationID: input.ConversationID,
		UserID:         input.UserID,
		RunID:          input.RunID,
		ToolCallID:     "multimodal_" + normalizePublicID(uuid.NewString()),
		ToolType:       "system_multimodal",
		ToolName:       systemMultimodalAnalyzeToolName,
		Status:         "requested",
		InputJSON:      multimodalDelegationAuditJSON(model, files),
	}
}

func (s *Service) failMultimodalDelegation(
	ctx context.Context,
	input multimodalDelegationInput,
	result *multimodalDelegationResult,
	row domainconversation.ToolCall,
	model string,
	fileCount int,
	cause error,
) error {
	row.Status = "error"
	row.ErrorJSON = sanitizeOpaqueToolOutput(cause.Error())
	row.OutputJSON = multimodalDelegationStatusJSON(model, fileCount, "error")
	s.persistMultimodalDelegationRow(ctx, &row, result, input.SkipPersistence)
	return fmt.Errorf("%w: %v", ErrMultimodalDelegationFailed, cause)
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

func selectMultimodalDelegationGroups(
	cfg config.Config,
	mainRoute *channel.ResolvedRoute,
	attachments []AttachmentInput,
) []multimodalDelegationGroup {
	return selectMultimodalDelegationGroupsWithHistory(cfg, mainRoute, attachments, false)
}

func selectMultimodalDelegationGroupsWithHistory(
	cfg config.Config,
	mainRoute *channel.ResolvedRoute,
	attachments []AttachmentInput,
	allowHistorical bool,
) []multimodalDelegationGroup {
	if mainRoute == nil {
		return nil
	}
	allowed := parseMultimodalDelegationModalities(cfg.MultimodalDelegationModalities)
	groups := make([]multimodalDelegationGroup, 0, 3)
	groupIndexes := make(map[string]int, 3)
	for _, attachment := range attachments {
		if !attachment.Current && !(allowHistorical && attachmentMediaModality(attachment) == "image") {
			continue
		}
		modality := attachmentMediaModality(attachment)
		if modality == "" {
			continue
		}
		if _, ok := allowed[modality]; !ok {
			continue
		}
		if modelSupportsMedia(mainRoute.PlatformModelName, mainRoute.ModelCapabilitiesJSON, modality) {
			continue
		}

		model := multimodalDelegationModelForModality(cfg, modality)
		groupKey := "model:" + model
		if model == "" {
			groupKey = "missing:" + modality
		}
		index, ok := groupIndexes[groupKey]
		if !ok {
			index = len(groups)
			groupIndexes[groupKey] = index
			groups = append(groups, multimodalDelegationGroup{Model: model})
		}
		groups[index].Attachments = append(groups[index].Attachments, attachment)
		groups[index].AuditFiles = append(groups[index].AuditFiles, multimodalDelegationAuditFile{
			FileID:     strings.TrimSpace(attachment.FileID),
			FileName:   strings.TrimSpace(attachment.FileName),
			MIMEType:   firstNonEmptyString(attachment.DetectedMIME, attachment.MimeType),
			Size:       max(attachment.FileSize, 0),
			Modality:   modality,
			Historical: !attachment.Current,
		})
	}
	return groups
}

func multimodalDelegationModelForModality(cfg config.Config, modality string) string {
	var model string
	switch strings.ToLower(strings.TrimSpace(modality)) {
	case "image":
		model = cfg.MultimodalDelegationImageModel
	case "audio":
		model = cfg.MultimodalDelegationAudioModel
	case "video":
		model = cfg.MultimodalDelegationVideoModel
	}
	if model = strings.TrimSpace(model); model != "" {
		return model
	}
	if strings.TrimSpace(cfg.MultimodalDelegationImageModel) != "" ||
		strings.TrimSpace(cfg.MultimodalDelegationAudioModel) != "" ||
		strings.TrimSpace(cfg.MultimodalDelegationVideoModel) != "" {
		return ""
	}
	return strings.TrimSpace(cfg.MultimodalDelegationModel)
}

func firstMultimodalAuditModality(files []multimodalDelegationAuditFile) string {
	for _, item := range files {
		if modality := strings.TrimSpace(item.Modality); modality != "" {
			return modality
		}
	}
	return "media"
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
	return "Analyze the attached media in relation to the user's original request. Directly inspect every supplied media attachment. Do not infer media contents from only the request, filename, MIME type, or metadata.\n\n" +
		"Return exactly one JSON object with this shape:\n" +
		`{"status":"ok","analysis":"complete factual handoff"}` + "\n" +
		`Use status "ok" only when you directly accessed and analyzed all supplied media. If any attachment cannot be decoded, viewed, heard, or otherwise inspected, return {"status":"unsupported","analysis":"brief reason"} instead. Do not wrap the JSON in commentary.` +
		"\n\nOriginal request:\n" + strings.TrimSpace(userPrompt) + "\n\nFiles:\n- " + strings.Join(names, "\n- ")
}

func multimodalDelegationOptions(base map[string]interface{}) map[string]interface{} {
	options := make(map[string]interface{}, len(base)+1)
	for key, value := range base {
		options[key] = value
	}
	options["response_format"] = map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "multimodal_delegation",
			"strict": false,
			"schema": multimodalDelegationJSONSchema,
		},
	}
	return options
}

func resolveMultimodalDelegationOutput(output *llm.GenerateOutput) (string, error) {
	if output == nil {
		return "", errors.New("returned no response")
	}
	return resolveMultimodalDelegationResponse(output.Text)
}

func resolveMultimodalDelegationResponse(raw string) (string, error) {
	text := stripMarkdownJSONFence(strings.TrimSpace(raw))
	if text == "" {
		return "", errors.New("returned no analysis")
	}
	var response multimodalDelegationResponse
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		if isMultimodalCapabilityRefusal(text) {
			return "", errors.New("reported that the supplied media could not be analyzed")
		}
		return "", errors.New("returned an invalid multimodal analysis envelope")
	}
	response.Status = strings.ToLower(strings.TrimSpace(response.Status))
	response.Analysis = strings.TrimSpace(response.Analysis)
	switch response.Status {
	case multimodalDelegationStatusOK:
		if response.Analysis == "" {
			return "", errors.New("returned an empty multimodal analysis")
		}
		if isMultimodalCapabilityRefusal(response.Analysis) {
			return "", errors.New("reported that the supplied media could not be analyzed")
		}
		return response.Analysis, nil
	case multimodalDelegationStatusUnsupported:
		return "", errors.New("reported unsupported media input")
	default:
		return "", errors.New("returned an invalid multimodal analysis status")
	}
}

func isMultimodalCapabilityRefusal(text string) bool {
	value := strings.ToLower(strings.TrimSpace(text))
	if value == "" {
		return false
	}
	for _, phrase := range []string{
		"i cannot analyze this image",
		"i cannot analyze this audio",
		"i cannot analyze this video",
		"i can't analyze this image",
		"i can't analyze this audio",
		"i can't analyze this video",
		"i am unable to analyze the image",
		"i am unable to analyze the audio",
		"i am unable to analyze the video",
		"cannot inspect the video frames",
		"cannot access the attached image",
		"cannot access the attached audio",
		"cannot access the attached video",
		"do not have vision capabilities",
		"don't have vision capabilities",
		"does not support audio input",
		"does not support video input",
		"unable to listen to the audio",
		"unable to watch the video",
		"i cannot listen to the audio",
		"i can't listen to the audio",
		"i cannot watch videos",
		"i can't watch videos",
		"i cannot view videos",
		"i can't view videos",
		"please upload the audio",
		"please upload an audio",
		"please provide the audio",
		"please provide an audio",
		"please upload the video",
		"please upload a video",
		"please provide the video",
		"please provide a video",
		"无法查看图片",
		"无法分析图片",
		"无法查看视频",
		"无法分析视频",
		"无法读取视频",
		"无法识别视频",
		"无法收听音频",
		"无法分析音频",
		"无法读取音频",
		"不支持图片输入",
		"不支持音频输入",
		"不支持视频输入",
		"没有视觉能力",
		"无法读取附件",
	} {
		if strings.Contains(value, phrase) {
			return true
		}
	}
	return false
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
