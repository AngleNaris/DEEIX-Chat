package conversation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	appstorage "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/objectstorage"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	domainmcp "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/mcp"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
)

const (
	maxImageAttachmentAnalysisChars      = 6000
	maxImageAttachmentAnalysisTotalChars = 16000
	// maxProcessorBinaryBytes 附件处理器（audio/file 模式）允许注入的原始文件大小上限。
	// 二进制附件按 base64 注入 MCP 参数，过大会撑爆请求体；图片走缩放流程不受此限。
	maxProcessorBinaryBytes = 8 << 20
)

type imageAttachmentAnalysis struct {
	FileID   string
	FileName string
	ToolName string
	Content  string
}

type imageAttachmentProcessingInput struct {
	UserID         uint
	ConversationID uint
	MessageID      uint
	RequestID      string
	RunID          string
	UserPrompt     string
	Attachments    []AttachmentInput
	Runtime        selectedToolRuntime
	TraceRecorder  *messageTraceRecorder
}

type imageAttachmentProcessingResult struct {
	Routed                bool
	Analyses              []imageAttachmentAnalysis
	Rows                  []domainconversation.ToolCall
	PersistedToolCallKeys map[string]struct{}
}

// processImageAttachments 将当前消息附件按附件处理器（image/audio/file 模式）注入
// 到指定的 MCP 工具参数中执行，并把分析结果接回主模型上下文。
// 名称保留 image 前缀以兼容既有调用点；实际支持三种附件模式。
func (s *Service) processImageAttachments(
	ctx context.Context,
	input imageAttachmentProcessingInput,
) (imageAttachmentProcessingResult, error) {
	processor := input.Runtime.attachmentProcessor
	if processor == nil {
		return imageAttachmentProcessingResult{}, nil
	}
	// mode 为空时回退 image 行为（兼容存量工具与旧测试构造）。
	mode := strings.ToLower(strings.TrimSpace(processor.mode))
	if mode == "" {
		mode = domainmcp.AttachmentInputModeImage
	}
	attachments := currentProcessorAttachments(input.Attachments, mode)
	if len(attachments) == 0 {
		return imageAttachmentProcessingResult{}, nil
	}
	result := imageAttachmentProcessingResult{
		Routed:                true,
		Analyses:              make([]imageAttachmentAnalysis, 0, len(attachments)),
		Rows:                  make([]domainconversation.ToolCall, 0, len(attachments)),
		PersistedToolCallKeys: make(map[string]struct{}, len(attachments)),
	}
	if len(attachments) > s.resolveMaxToolCallsPerRun() {
		return result, fmt.Errorf("%w: attachment count exceeds the tool call limit", ErrImageAttachmentProcessingFailed)
	}
	if processor.argument == "" ||
		(processor.encoding != domainmcp.AttachmentEncodingBase64 && processor.encoding != domainmcp.AttachmentEncodingDataURL) {
		return result, fmt.Errorf("%w: processor configuration is invalid", ErrImageAttachmentProcessingFailed)
	}

	cfg := s.cfg.Snapshot()
	storeProvider := s.storeProvider
	if storeProvider == nil {
		storeProvider = appstorage.NewRuntimeProvider(config.NewRuntime(cfg), nil)
	}
	store, err := storeProvider.Open(ctx)
	if err != nil {
		return result, fmt.Errorf("%w: open object storage: %v", ErrImageAttachmentProcessingFailed, err)
	}

	totalImageBytes := 0
	analysisCharLimit := min(maxImageAttachmentAnalysisChars, maxImageAttachmentAnalysisTotalChars/len(attachments))
	for index, attachment := range attachments {
		prepared, prepareErr := prepareAttachmentForProcessor(ctx, store, attachment, cfg.ImageMaxDimension, processor.mode)
		if prepareErr != nil {
			return result, prepareErr
		}
		totalImageBytes += len(prepared.data)
		if processor.mode == domainmcp.AttachmentInputModeImage && totalImageBytes > maxConversationImageContextBytes {
			return result, fmt.Errorf("%w: image attachment context exceeds %d bytes", ErrFileTooLarge, maxConversationImageContextBytes)
		}
		if processor.mode != domainmcp.AttachmentInputModeImage && len(prepared.data) > maxProcessorBinaryBytes {
			return result, fmt.Errorf("%w: attachment exceeds %d bytes and cannot be injected into the tool", ErrFileTooLarge, maxProcessorBinaryBytes)
		}

		encodedImage := base64.StdEncoding.EncodeToString(prepared.data)
		if processor.encoding == domainmcp.AttachmentEncodingDataURL {
			encodedImage = "data:" + prepared.mimeType + ";base64," + encodedImage
		}
		arguments := map[string]interface{}{processor.argument: encodedImage}
		if processor.promptArgument != "" {
			arguments[processor.promptArgument] = strings.TrimSpace(input.UserPrompt)
		}
		argumentsJSON, marshalErr := json.Marshal(arguments)
		if marshalErr != nil {
			return result, fmt.Errorf("%w: encode processor arguments: %v", ErrImageAttachmentProcessingFailed, marshalErr)
		}
		normalizedArguments, validationErr := normalizeToolArguments(string(argumentsJSON), input.Runtime.schemas[processor.modelName])
		row := domainconversation.ToolCall{
			MessageID:      input.MessageID,
			ConversationID: input.ConversationID,
			UserID:         input.UserID,
			RunID:          input.RunID,
			ToolCallID:     fmt.Sprintf("attachment_%s_%d", input.RunID, index+1),
			ToolType:       "mcp_attachment",
			ToolName:       processor.toolName,
			Status:         "requested",
			InputJSON:      imageAttachmentAuditInput(attachment, prepared.mimeType, processor.encoding, len(prepared.data)),
		}
		if validationErr != nil {
			row.Status = "error"
			row.ErrorJSON = validationErr.Error()
			s.persistImageAttachmentToolRow(ctx, &row, &result)
			return result, fmt.Errorf("%w: %v", ErrImageAttachmentProcessingFailed, validationErr)
		}

		mcpConfig, ok := input.Runtime.mcpConfigs[processor.modelName]
		if !ok {
			row.Status = "error"
			row.ErrorJSON = "processor is not enabled for this run"
			s.persistImageAttachmentToolRow(ctx, &row, &result)
			return result, fmt.Errorf("%w: processor is not enabled", ErrImageAttachmentProcessingFailed)
		}
		startedAt := time.Now()
		output, executeErr := s.executeToolCall(ctx, ExecuteToolInput{
			UserID:         input.UserID,
			ConversationID: input.ConversationID,
			RequestID:      input.RequestID,
			ToolName:       processor.toolName,
			ArgumentsJSON:  normalizedArguments,
			MCPConfig:      &mcpConfig,
		})
		row.LatencyMS = max(time.Since(startedAt).Milliseconds(), 0)
		if executeErr != nil {
			row.Status = "error"
			row.ErrorJSON = sanitizeOpaqueToolOutput(executeErr.Error())
			s.persistImageAttachmentToolRow(ctx, &row, &result)
			return result, fmt.Errorf("%w: %v", ErrImageAttachmentProcessingFailed, executeErr)
		}
		row.OutputJSON = sanitizeOpaqueToolOutput(output)
		if row.OutputJSON == "" {
			row.OutputJSON = "{}"
		}
		analysis := imageAttachmentAnalysisText(output)
		if analysis == "" {
			row.Status = "error"
			row.ErrorJSON = "processor returned no textual analysis"
			s.persistImageAttachmentToolRow(ctx, &row, &result)
			return result, fmt.Errorf("%w: processor returned no textual analysis", ErrImageAttachmentProcessingFailed)
		}
		row.Status = "success"
		s.persistImageAttachmentToolRow(ctx, &row, &result)
		analysis = contextArtifactExcerpt(analysis, analysisCharLimit)
		result.Analyses = append(result.Analyses, imageAttachmentAnalysis{
			FileID:   strings.TrimSpace(attachment.FileID),
			FileName: firstNonEmptyString(attachment.FileName, attachment.FileID),
			ToolName: processor.displayName,
			Content:  analysis,
		})
	}

	if input.TraceRecorder != nil {
		fileNames := make([]string, 0, len(result.Analyses))
		for _, analysis := range result.Analyses {
			fileNames = append(fileNames, analysis.FileName)
		}
		attachmentLabel := "附件"
		if processor.mode == domainmcp.AttachmentInputModeImage {
			attachmentLabel = "图片"
		}
		input.TraceRecorder.appendProcessSection(
			fmt.Sprintf("已通过 %s 处理 %d 个%s", processor.displayName, len(result.Analyses), attachmentLabel),
			formatTraceStep(attachmentLabel, fmt.Sprintf("%s已交由 %s 处理，主模型仅接收处理结果。", attachmentLabel, processor.displayName)),
			map[string]interface{}{
				"tool_id":    processor.toolID,
				"tool_name":  processor.toolName,
				"file_names": fileNames,
				processTracePayloadStage: map[string]interface{}{
					"kind":       "mcp_attachment_processor",
					"status":     messageTraceStatusCompleted,
					"file_count": len(result.Analyses),
				},
			},
			messageTraceStatusCompleted,
		)
	}
	return result, nil
}

type preparedImageAttachment struct {
	data     []byte
	mimeType string
}

// prepareAttachmentForProcessor 按模式准备附件字节：
//   - image：读取 + 缩放（复用现有图片管线）；
//   - audio/file：原样读取（受 maxProcessorBinaryBytes 限制）。
func prepareAttachmentForProcessor(
	ctx context.Context,
	store objectstore.Store,
	attachment AttachmentInput,
	maxDimension int,
	mode string,
) (preparedImageAttachment, error) {
	storagePath := strings.TrimSpace(attachment.StoragePath)
	if storagePath == "" {
		return preparedImageAttachment{}, fmt.Errorf("%w: attachment storage path is empty", ErrInvalidFileReference)
	}
	reader, _, err := store.Open(ctx, storagePath)
	if err != nil {
		return preparedImageAttachment{}, fmt.Errorf("%w: open attachment %s: %v", ErrFileNotFound, attachment.FileID, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, maxConversationImageSourceBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return preparedImageAttachment{}, fmt.Errorf("%w: read attachment %s: %v", ErrFileNotFound, attachment.FileID, readErr)
	}
	if closeErr != nil {
		return preparedImageAttachment{}, fmt.Errorf("%w: close attachment %s: %v", ErrFileNotFound, attachment.FileID, closeErr)
	}
	if len(data) == 0 {
		return preparedImageAttachment{}, fmt.Errorf("%w: attachment %s is empty", ErrInvalidFileReference, attachment.FileID)
	}
	if len(data) > maxConversationImageSourceBytes {
		return preparedImageAttachment{}, fmt.Errorf("%w: attachment %s exceeds source limit", ErrFileTooLarge, attachment.FileID)
	}
	mimeType := firstNonEmptyString(attachment.DetectedMIME, attachment.MimeType)
	if mode == domainmcp.AttachmentInputModeImage {
		if maxDimension <= 0 {
			maxDimension = 1024
		}
		mimeType = resolveImageMimeType(mimeType)
		resized, actualMIME := resizeImageIfNeeded(data, mimeType, maxDimension)
		return preparedImageAttachment{data: resized, mimeType: actualMIME}, nil
	}
	return preparedImageAttachment{data: data, mimeType: mimeType}, nil
}

// currentProcessorAttachments 按处理器模式筛选当前消息附件。
func currentProcessorAttachments(attachments []AttachmentInput, mode string) []AttachmentInput {
	result := make([]AttachmentInput, 0)
	for _, attachment := range attachments {
		if !attachment.Current {
			continue
		}
		mimeType := strings.ToLower(firstNonEmptyString(attachment.DetectedMIME, attachment.MimeType))
		kind := normalizeAttachmentKind(attachment.Kind, mimeType)
		switch mode {
		case domainmcp.AttachmentInputModeImage:
			if kind == "image" {
				result = append(result, attachment)
			}
		case domainmcp.AttachmentInputModeAudio:
			if strings.HasPrefix(mimeType, "audio/") {
				result = append(result, attachment)
			}
		case domainmcp.AttachmentInputModeFile:
			if kind != "image" {
				result = append(result, attachment)
			}
		}
	}
	return result
}

func imageAttachmentAuditInput(attachment AttachmentInput, mimeType string, encoding string, byteSize int) string {
	payload, _ := json.Marshal(map[string]interface{}{
		"file_id":   strings.TrimSpace(attachment.FileID),
		"file_name": strings.TrimSpace(attachment.FileName),
		"mime_type": strings.TrimSpace(mimeType),
		"encoding":  strings.TrimSpace(encoding),
		"byte_size": byteSize,
	})
	return string(payload)
}

func (s *Service) persistImageAttachmentToolRow(
	ctx context.Context,
	row *domainconversation.ToolCall,
	result *imageAttachmentProcessingResult,
) {
	if row == nil || result == nil {
		return
	}
	persisted := s.persistToolCallResult(ctx, row)
	result.Rows = append(result.Rows, *row)
	if persisted {
		result.PersistedToolCallKeys[toolCallPersistenceKey(*row)] = struct{}{}
	}
}

func imageAttachmentAnalysisText(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StructuredContent interface{} `json:"structuredContent"`
	}
	if err := json.Unmarshal([]byte(value), &payload); err == nil {
		parts := make([]string, 0, len(payload.Content))
		for _, item := range payload.Content {
			if text := strings.TrimSpace(item.Text); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
		if payload.StructuredContent != nil {
			if encoded, err := json.Marshal(payload.StructuredContent); err == nil {
				return strings.TrimSpace(modelToolOutputForModel(string(encoded)))
			}
		}
		return ""
	}
	return strings.TrimSpace(modelToolOutputForModel(value))
}

func withoutCurrentImageAttachments(plan conversationFileContextPlan) conversationFileContextPlan {
	filter := func(items []AttachmentInput) []AttachmentInput {
		result := make([]AttachmentInput, 0, len(items))
		for _, item := range items {
			mimeType := firstNonEmptyString(item.DetectedMIME, item.MimeType)
			if item.Current && normalizeAttachmentKind(item.Kind, mimeType) == "image" {
				continue
			}
			result = append(result, item)
		}
		return result
	}
	plan.Attachments = filter(plan.Attachments)
	plan.FullAttachments = filter(plan.FullAttachments)
	return plan
}
