package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// imageGenToolChannelLimit 单次生成图片数量上限。
const imageGenToolChannelLimit = 4

// imageGenChannelSetting 平台 image_gen 渠道配置项（与 settings 包 JSON 形状一致）。
type imageGenChannelSetting struct {
	Model string `json:"model"`
	Note  string `json:"note,omitempty"`
}

// platformImageGenChannels 解析平台 image_gen 渠道配置（JSON 数组 [{model,note}]）。
func parseImageGenChannels(raw string) []imageGenChannelSetting {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var items []imageGenChannelSetting
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil
	}
	result := make([]imageGenChannelSetting, 0, len(items))
	for _, item := range items {
		item.Model = strings.TrimSpace(item.Model)
		if item.Model == "" {
			continue
		}
		result = append(result, item)
	}
	return result
}

// buildImageGenToolDescription 渐进披露渠道清单：模型名 + 管理员备注（如"可生成4K图"），
// 由 agent 在调用时自行选择渠道。
func buildImageGenToolDescription(channels []imageGenChannelSetting) string {
	var builder strings.Builder
	builder.WriteString("Generate images for the user using one of the configured image generation channels. " +
		"Call this when the user asks to create an image based on the current discussion. " +
		"Choose the most suitable channel from the list below (channel notes describe each channel's capability). " +
		"Available channels:")
	for _, item := range channels {
		builder.WriteString("\n- channel model: ")
		builder.WriteString(item.Model)
		if note := strings.TrimSpace(item.Note); note != "" {
			builder.WriteString(" (")
			builder.WriteString(note)
			builder.WriteString(")")
		}
	}
	builder.WriteString("\nIf you are not sure which channel fits best, pick the first one. " +
		"The generated image is saved to the user's files and returned as markdown; report it to the user. " +
		"This is a WRITE operation: it may require user approval depending on the user's approval mode.")
	return builder.String()
}

// platformGenerateImage 平台 image_gen 工具执行体：按渠道模型生成图片并上传为文件对象，
// 返回 markdown 引用（供模型直接回显给用户）。
func (s *Service) platformGenerateImage(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Prompt  string `json:"prompt"`
		Channel string `json:"channel"`
		Count   int    `json:"count"`
	}
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return "", fmt.Errorf("invalid image_gen arguments: %v", err)
		}
	}
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" {
		return "", fmt.Errorf("image_gen requires a prompt describing the image to generate")
	}
	channel := strings.TrimSpace(args.Channel)
	if channel == "" {
		return "", fmt.Errorf("image_gen requires a channel (pick one from the available channels in the tool description)")
	}
	count := args.Count
	if count <= 0 {
		count = 1
	}
	if count > imageGenToolChannelLimit {
		count = imageGenToolChannelLimit
	}

	values, err := s.platformToolsSettings.RuntimeValuesByNamespace(ctx, platformToolsNamespace)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("platform_tools_settings_read_failed",
				zap.String("trace_id", traceIDFromContext(ctx)),
				zap.Error(err),
			)
		}
		return "", fmt.Errorf("image_gen channel settings unavailable")
	}
	channels := parseImageGenChannels(values["image_gen_channels"])
	selected := ""
	for _, item := range channels {
		if strings.EqualFold(strings.TrimSpace(item.Model), channel) {
			selected = item.Model
			break
		}
	}
	if selected == "" {
		return "", fmt.Errorf("image_gen channel %q is not configured; available channels: %s",
			channel, imageGenChannelNames(channels))
	}

	results := make([]string, 0, count)
	exportItems := make([]imageGenExportItem, 0, count)
	for i := 0; i < count; i++ {
		promptPart := prompt
		if count > 1 {
			promptPart = fmt.Sprintf("%s (variant %d of %d, same subject and style)", prompt, i+1, count)
		}
		result, genErr := s.generateImagesForTool(ctx, MediaImageInput{
			UserID:            call.UserID,
			ConversationID:    call.ConversationID,
			RequestID:         call.RequestID,
			TaskType:          MediaImageTaskGeneration,
			Prompt:            promptPart,
			PlatformModelName: selected,
		})
		if genErr != nil {
			if i == 0 {
				return "", genErr
			}
			results = append(results, fmt.Sprintf("(variant %d failed: %s)", i+1, messageErrorSummary(genErr)))
			break
		}
		if len(result.Files) > 0 {
			for _, file := range result.Files {
				exportItems = append(exportItems, imageGenExportItem{
					Path: "file://" + file.FileID,
					Name: file.FileName,
				})
			}
		}
		results = append(results, result.Markdown)
	}
	output := strings.Join(results, "\n\n")
	if len(exportItems) > 0 {
		// __export__ 标记放在最前（工具结果整体是"JSON 标记 + markdown"拼接，
		// 标记段必须是合法 JSON，供后端解析后把生成图挂为消息附件卡片）。
		exportJSON, _ := json.Marshal(map[string]interface{}{
			"__export__": exportItems,
		})
		output = string(exportJSON) + "\n\n" + output
	}
	return output, nil
}

// imageGenExportItem 与 toolExportItem 同构，但 path 存放已上传文件的 fileID
//（image_gen 的文件已由平台上传，无需再读共享卷）。
type imageGenExportItem struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

func imageGenChannelNames(channels []imageGenChannelSetting) string {
	names := make([]string, 0, len(channels))
	for _, item := range channels {
		names = append(names, item.Model)
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
