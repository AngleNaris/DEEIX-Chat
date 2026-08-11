package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	appupload "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/upload"
	domainconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
)

// toolExportItem 工具结果中的导出标记（__export__ 数组元素）。
// 任何 MCP 工具（如沙箱 sandbox_export_file、mm save_view）结果携带该字段时，
// DEEIX 后端会从共享卷读取文件并落库为当前用户的文件。
type toolExportItem struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// maxToolExportBytes 单文件导出上限（与上传默认上限一致）。
const maxToolExportBytes = 20 << 20

// parseToolExportItems 从工具结果 JSON 中解析 __export__ 标记。
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
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return nil
	}
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
	return nil
}

// exportFilePath 校验共享目录路径并返回容器内绝对路径。
// 只允许读取共享卷（SandboxSharedDir）内的文件，且路径前缀必须属于当前用户会话
// （/shared/deeix-<uid>-<cid>/），防跨用户导出。
func (s *Service) exportFilePath(input executeAssistantToolCallsInput, path string) (string, error) {
	sharedDir := strings.TrimSpace(s.cfg.Snapshot().SandboxSharedDir)
	if sharedDir == "" {
		sharedDir = "/shared"
	}
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
	return p, nil
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
	for _, item := range items {
		absPath, err := s.exportFilePath(input, item.Path)
		if err != nil {
			slog.Warn("tool export rejected", "tool", input.RunID, "err", err)
			continue
		}
		stat, err := os.Stat(absPath)
		if err != nil || !stat.Mode().IsRegular() {
			slog.Warn("tool export file not readable", "path", absPath, "err", err)
			continue
		}
		if stat.Size() <= 0 || stat.Size() > maxToolExportBytes {
			slog.Warn("tool export file size invalid", "path", absPath, "size", stat.Size())
			continue
		}
		reader, err := os.Open(absPath)
		if err != nil {
			slog.Warn("tool export open failed", "path", absPath, "err", err)
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" || name == "." || strings.ContainsAny(name, "/\\") {
			name = filepath.Base(absPath)
		}
		uploadResult, uploadErr := s.UploadFile(ctx, appupload.UploadFileInput{
			UserID:       input.UserID,
			Purpose:      "sandbox_export",
			FileName:     name,
			DeclaredSize: stat.Size(),
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
	if err := s.repo.CreateAttachments(ctx, attachments); err != nil {
		slog.Warn("tool export persist attachments failed", "count", len(attachments), "err", err)
	}
	return strings.Join(links, " ")
}
