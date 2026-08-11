package conversation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	appupload "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/upload"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
)

const (
	// maxToolArtifactBytes 单个工具产物附件上限（与上传默认上限一致）。
	maxToolArtifactBytes = 20 << 20
	// maxToolArtifactsPerCall 单次工具调用最多附件化块数（防滥用）。
	maxToolArtifactsPerCall = 8
)

// toolArtifactBlock 工具结果中提取出的多模态附件块。
type toolArtifactBlock struct {
	Kind     string // image / audio / video
	MimeType string
	Data     []byte
}

// extractToolArtifactBlocks 从 MCP 工具结果 JSON 中提取多模态 content 块与通用 base64 图片键。
// 支持的格式（与前端工具轨迹渲染保持一致）：
//   - content[].type == "image" / "audio" / "video"，携带 data(base64) + mimeType（mm-plugins / 沙箱截图等）
//   - 顶层或 content 块内的 base64 图片键：b64_json / base64 / image_url / uri / url / partial_image_b64
func extractToolArtifactBlocks(output string) []toolArtifactBlock {
	raw := strings.TrimSpace(output)
	if raw == "" {
		return nil
	}
	var payload struct {
		Content []map[string]any `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil || len(payload.Content) == 0 {
		// 非 content 数组结构：尝试顶层 base64 图片键。
		return extractBase64ImageBlocks(raw)
	}
	blocks := make([]toolArtifactBlock, 0, len(payload.Content))
	for _, item := range payload.Content {
		block, ok := contentBlockToArtifact(item)
		if ok {
			blocks = append(blocks, block)
			if len(blocks) >= maxToolArtifactsPerCall {
				break
			}
		}
	}
	return blocks
}

func contentBlockToArtifact(item map[string]any) (toolArtifactBlock, bool) {
	blockType := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", item["type"])))
	switch blockType {
	case "image", "audio", "video":
		data, mimeType, ok := decodeContentBlockData(item)
		if !ok {
			return toolArtifactBlock{}, false
		}
		return toolArtifactBlock{Kind: blockType, MimeType: mimeType, Data: data}, true
	default:
		// 兼容 text 块内嵌图片 JSON（部分工具把图放 text 里）。
		if text := strings.TrimSpace(fmt.Sprintf("%v", item["text"])); text != "" {
			if blocks := extractBase64ImageBlocks(text); len(blocks) > 0 {
				return blocks[0], true
			}
		}
	}
	return toolArtifactBlock{}, false
}

func decodeContentBlockData(item map[string]any) ([]byte, string, bool) {
	mimeType := strings.TrimSpace(fmt.Sprintf("%v", item["mimeType"]))
	if mimeType == "" {
		mimeType = strings.TrimSpace(fmt.Sprintf("%v", item["mime_type"]))
	}
	dataRaw := item["data"]
	if dataRaw == nil {
		return nil, "", false
	}
	encoded := strings.TrimSpace(fmt.Sprintf("%v", dataRaw))
	if strings.HasPrefix(encoded, "data:") {
		// data URL 形式：data:<mime>;base64,<payload>
		comma := strings.Index(encoded, ",")
		if comma < 0 {
			return nil, "", false
		}
		if mimeType == "" {
			header := encoded[:comma]
			if semi := strings.Index(header, ";"); semi > 0 {
				mimeType = strings.TrimPrefix(header[:semi], "data:")
			}
		}
		encoded = encoded[comma+1:]
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", false
	}
	if len(data) == 0 || len(data) > maxToolArtifactBytes {
		return nil, "", false
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return data, mimeType, true
}

// extractBase64ImageBlocks 从任意 JSON/文本中提取 base64 图片（b64_json/base64/image_url 等键）。
func extractBase64ImageBlocks(raw string) []toolArtifactBlock {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var probe map[string]any
	if err := json.Unmarshal([]byte(trimmed), &probe); err != nil {
		return nil
	}
	for _, key := range []string{"b64_json", "base64", "partial_image_b64"} {
		value, ok := probe[key]
		if !ok {
			continue
		}
		encoded := strings.TrimSpace(fmt.Sprintf("%v", value))
		if encoded == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(data) == 0 || len(data) > maxToolArtifactBytes {
			continue
		}
		mimeType := probeMIMEFromBase64(data)
		if mimeType == "" {
			mimeType = "image/png"
		}
		return []toolArtifactBlock{{Kind: "image", MimeType: mimeType, Data: data}}
	}
	// image_url / url / uri：仅接受 data URL（外链不落库，避免服务端拉取任意地址）。
	if value, ok := probe["image_url"].(string); ok {
		if data, mimeType, decoded := decodeDataURL(value); decoded {
			return []toolArtifactBlock{{Kind: "image", MimeType: mimeType, Data: data}}
		}
	}
	return nil
}

func probeMIMEFromBase64(data []byte) string {
	if len(data) > 7 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "image/png"
	}
	if len(data) > 2 && data[0] == 0xFF && data[1] == 0xD8 {
		return "image/jpeg"
	}
	if len(data) > 3 && string(data[:4]) == "GIF8" {
		return "image/gif"
	}
	if len(data) > 3 && string(data[:3]) == "ID3" {
		return "audio/mpeg"
	}
	if len(data) > 7 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		return "audio/wav"
	}
	return ""
}

func decodeDataURL(value string) ([]byte, string, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "data:") {
		return nil, "", false
	}
	comma := strings.Index(value, ",")
	if comma < 0 {
		return nil, "", false
	}
	header := value[:comma]
	mimeType := "image/png"
	if semi := strings.Index(header, ";"); semi > 0 {
		mimeType = strings.TrimPrefix(header[:semi], "data:")
	}
	data, err := base64.StdEncoding.DecodeString(value[comma+1:])
	if err != nil || len(data) == 0 || len(data) > maxToolArtifactBytes {
		return nil, "", false
	}
	return data, mimeType, true
}

func artifactFileExtension(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/mp4", "audio/x-m4a":
		return ".m4a"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	default:
		return ""
	}
}

// attachToolArtifacts 将工具结果中的多模态块落库为消息附件。
// 失败不阻断工具循环（仅记日志）：附件化是增强能力，工具结果仍按原路径回喂模型。
func (s *Service) attachToolArtifacts(ctx context.Context, input executeAssistantToolCallsInput, outputJSON string) {
	blocks := extractToolArtifactBlocks(outputJSON)
	if len(blocks) == 0 {
		return
	}
	now := time.Now()
	attachments := make([]domainconversation.Attachment, 0, len(blocks))
	for i, block := range blocks {
		if len(block.Data) > maxToolArtifactBytes || len(block.Data) == 0 {
			continue
		}
		ext := artifactFileExtension(block.MimeType)
		if ext == "" {
			ext = ".bin"
		}
		fileName := fmt.Sprintf("tool-artifact-%d-%d%s", now.UnixMilli(), i+1, ext)
		uploadResult, uploadErr := s.UploadFile(ctx, appupload.UploadFileInput{
			UserID:       input.UserID,
			Purpose:      "tool_artifact",
			FileName:     fileName,
			MimeType:     block.MimeType,
			DeclaredSize: int64(len(block.Data)),
			Reader:       bytes.NewReader(block.Data),
		})
		if uploadErr != nil {
			slog.Warn("attach tool artifact upload failed", "tool", input.RunID, "err", uploadErr)
			continue
		}
		file := uploadResult.File
		attachments = append(attachments, domainconversation.Attachment{
			ConversationID: input.ConversationID,
			MessageID:      input.MessageID,
			UserID:         input.UserID,
			FileID:         file.FileID,
			Kind:           block.Kind,
			FileName:       file.FileName,
			MimeType:       file.DetectedMIME,
			FileSize:       file.SizeBytes,
			SHA256:         file.SHA256,
			StoragePath:    file.StoragePath,
			Status:         "active",
			UploadedAt:     now,
		})
	}
	if len(attachments) == 0 {
		return
	}
	if err := s.repo.CreateAttachments(ctx, attachments); err != nil {
		slog.Warn("attach tool artifacts failed", "count", len(attachments), "err", err)
	}
}
