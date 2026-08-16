package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	appupload "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/upload"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
)

// toolExportItem 工具结果中的导出标记（__export__ 数组元素）。
// 任何 MCP 工具（如沙箱 sandbox_export_file、mm save_view）结果携带该字段时，
// DEEIX 后端会从共享卷读取文件并落库为当前用户的文件。
// Path 以 "file://" 前缀开头时表示平台已上传的文件 fileID（如 image_gen 生成的图片），
// 直接按已有文件挂为消息附件，不再读共享卷。
type toolExportItem struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type toolExportAttachmentRepository interface {
	CreateAttachments(ctx context.Context, items []domainconversation.Attachment) error
}

// maxToolExportBytes 单文件导出上限（与上传默认上限一致）。
const maxToolExportBytes = 20 << 20

// parseToolExportItems 从工具结果 JSON 中解析 __export__ 标记。
// 支持三种形态：纯 JSON、content 块包装、以及"JSON 标记 + 文本"拼接
// （如 image_gen 返回 {"__export__":[...]} 后接 markdown 图片引用）。
func parseToolExportItems(outputJSON string) []toolExportItem {
	raw := strings.TrimSpace(outputJSON)
	if raw == "" {
		return nil
	}
	// content 块形式：{"content":[{"type":"text","text":"..."}]} 或纯 JSON。
	var probe struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Export []toolExportItem `json:"__export__"`
	}
	if err := json.Unmarshal([]byte(raw), &probe); err == nil {
		if len(probe.Export) > 0 {
			return probe.Export
		}
		for _, block := range probe.Content {
			var inner struct {
				Export []toolExportItem `json:"__export__"`
			}
			if err := json.Unmarshal([]byte(block.Text), &inner); err != nil {
				continue
			}
			if len(inner.Export) > 0 {
				return inner.Export
			}
		}
	}
	// 拼接形态：逐段尝试解析"以 { 开头"的 JSON 对象（按括号配平截断）。
	for start := 0; start < len(raw); {
		braceIndex := strings.IndexByte(raw[start:], '{')
		if braceIndex < 0 {
			break
		}
		braceIndex += start
		end := matchJSONObjectEnd(raw, braceIndex)
		if end < 0 {
			break
		}
		var candidate struct {
			Export []toolExportItem `json:"__export__"`
		}
		if err := json.Unmarshal([]byte(raw[braceIndex:end+1]), &candidate); err == nil && len(candidate.Export) > 0 {
			return candidate.Export
		}
		start = end + 1
	}
	return nil
}

// matchJSONObjectEnd 返回从 openIndex（必须指向 '{'）开始配平的 JSON 对象结束下标；
// 处理字符串与转义，配平失败返回 -1。
func matchJSONObjectEnd(raw string, openIndex int) int {
	depth := 0
	inString := false
	escaped := false
	for i := openIndex; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// exportFilePath 校验共享目录路径并返回相对共享根的路径。
// 只允许读取共享目录（SandboxSharedDir）内的文件，且路径前缀必须属于当前用户会话
// （/shared/deeix-<uid>-<cid>/），防跨用户导出。实际文件由 openExportRegularFile
// 相对共享根原子打开，Linux 使用 openat2 拒绝 traversal、symlink 和 magic link。
func (s *Service) exportFilePath(input executeAssistantToolCallsInput, path string) (string, error) {
	sharedDir := strings.TrimSpace(s.cfg.Snapshot().SandboxSharedDir)
	if sharedDir == "" {
		sharedDir = "/shared"
	}
	sharedDir = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(sharedDir, "\\", "/")))
	p := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(path, "\\", "/")))
	if !strings.HasPrefix(p, sharedDir+"/") {
		return "", fmt.Errorf("export path outside shared dir: %s", path)
	}
	expected := fmt.Sprintf("%s/deeix-%d-%d", sharedDir, input.UserID, input.ConversationID)
	if input.ConversationID == 0 {
		expected = fmt.Sprintf("%s/deeix-%d", sharedDir, input.UserID)
	}
	if !strings.HasPrefix(p, expected+"/") {
		return "", fmt.Errorf("export path not in current session scope: %s", path)
	}
	rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(p, sharedDir+"/")))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("export path outside shared dir: %s", path)
	}
	return rel, nil
}

// exportToolArtifacts 处理工具结果中的 __export__ 标记：
// 从共享卷读取文件 → 上传为用户文件（出现在用户文件列表）→ 挂为消息附件 →
// 返回 markdown 下载链接（注入工具结果文本，模型可见并可在回答中引用）。
// 失败不阻断工具循环（记日志降级）：导出是增强能力，工具结果仍按原路径回喂模型。
func (s *Service) exportToolArtifacts(ctx context.Context, input executeAssistantToolCallsInput, outputJSON string) string {
	items := parseToolExportItems(outputJSON)
	if len(items) == 0 {
		return ""
	}
	sharedDir := strings.TrimSpace(s.cfg.Snapshot().SandboxSharedDir)
	if sharedDir == "" {
		sharedDir = "/shared"
	}
	now := time.Now()
	attachments := make([]domainconversation.Attachment, 0, len(items))
	links := make([]string, 0, len(items))
	seenExportPaths := make(map[string]struct{}, len(items))
	for _, item := range items {
		// 平台已上传文件的导出（image_gen 等）：path 即 fileID，直接挂为附件。
		if strings.HasPrefix(strings.TrimSpace(item.Path), "file://") {
			fileID := strings.TrimPrefix(strings.TrimSpace(item.Path), "file://")
			resolved, resolveErr := s.resolveAttachments(ctx, input.UserID, []string{fileID})
			if resolveErr != nil || len(resolved) == 0 {
				slog.Warn("tool export file_id resolve failed", "tool", input.RunID, "file_id", fileID, "err", resolveErr)
				continue
			}
			fileItem := resolved[0]
			attachments = append(attachments, domainconversation.Attachment{
				ConversationID: input.ConversationID,
				MessageID:      input.MessageID,
				UserID:         input.UserID,
				FileID:         fileItem.FileID,
				Kind:           "image",
				FileName:       fileItem.FileName,
				MimeType:       fileItem.MimeType,
				FileSize:       fileItem.FileSize,
				SHA256:         fileItem.SHA256,
				StoragePath:    fileItem.StoragePath,
				Status:         "active",
				UploadedAt:     now,
			})
			links = append(links, fmt.Sprintf("![%s](/api/v1/files/%s/content)", fileItem.FileName, fileItem.FileID))
			continue
		}
		relPath, err := s.exportFilePath(input, item.Path)
		if err != nil {
			slog.Warn("tool export rejected", "tool", input.RunID, "err", err)
			continue
		}
		if _, exists := seenExportPaths[relPath]; exists {
			continue
		}
		seenExportPaths[relPath] = struct{}{}
		reader, size, err := openExportRegularFile(sharedDir, relPath)
		if err != nil {
			slog.Warn("tool export file not readable", "path", item.Path, "err", err)
			continue
		}
		if size <= 0 || size > maxToolExportBytes {
			_ = reader.Close()
			slog.Warn("tool export file size invalid", "path", item.Path, "size", size)
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" || name == "." || strings.ContainsAny(name, "/\\") {
			name = filepath.Base(relPath)
		}
		uploadResult, uploadErr := s.UploadFile(ctx, appupload.UploadFileInput{
			UserID:       input.UserID,
			Purpose:      "sandbox_export",
			FileName:     name,
			DeclaredSize: size,
			Reader:       reader,
		})
		_ = reader.Close()
		if uploadErr != nil {
			slog.Warn("tool export upload failed", "name", name, "err", uploadErr)
			continue
		}
		file := uploadResult.File
		attachments = append(attachments, domainconversation.Attachment{
			ConversationID: input.ConversationID,
			MessageID:      input.MessageID,
			UserID:         input.UserID,
			FileID:         file.FileID,
			Kind:           "file",
			FileName:       file.FileName,
			MimeType:       file.DetectedMIME,
			FileSize:       file.SizeBytes,
			SHA256:         file.SHA256,
			StoragePath:    file.StoragePath,
			Status:         "active",
			UploadedAt:     now,
		})
		links = append(links, fmt.Sprintf("[%s](/api/v1/files/%s/content)", file.FileName, file.FileID))
	}
	if len(attachments) == 0 {
		return ""
	}
	if err := persistToolExportAttachments(ctx, s.repo, attachments); err != nil {
		slog.Warn("tool export persist attachments failed", "count", len(attachments), "err", err)
		return ""
	}
	return strings.Join(links, " ")
}

func persistToolExportAttachments(
	ctx context.Context,
	repo toolExportAttachmentRepository,
	attachments []domainconversation.Attachment,
) error {
	return repo.CreateAttachments(ctx, attachments)
}
