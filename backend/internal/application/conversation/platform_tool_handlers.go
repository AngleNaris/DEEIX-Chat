package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	apppromptpreset "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/promptpreset"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
)

// platformToolJSON 输出统一走 JSON 字符串，模型侧按工具结果解析。

// platformListFiles 列出用户文件（仅本人数据）。
func (s *Service) platformListFiles(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Query string `json:"query"`
		Kind  string `json:"kind"`
		Page  int    `json:"page"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.Page < 1 {
		args.Page = 1
	}
	if s.uploadSvc == nil {
		return "", fmt.Errorf("file service is unavailable")
	}
	result, err := s.uploadSvc.ListFiles(ctx, call.UserID, args.Page, platformListPageSize, args.Query, args.Kind, "")
	if err != nil {
		return "", err
	}
	type fileSummary struct {
		FileID           string `json:"file_id"`
		Name             string `json:"name"`
		Kind             string `json:"kind"`
		SizeBytes        int64  `json:"size_bytes"`
		ProcessingStatus string `json:"processing_status"`
		ExtractStatus    string `json:"extract_status"`
		RAGReady         bool   `json:"rag_ready"`
		ChunkCount       int    `json:"chunk_count"`
	}
	items := make([]fileSummary, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, fileSummary{
			FileID:           item.FileID,
			Name:             item.FileName,
			Kind:             item.FileCategory,
			SizeBytes:        item.SizeBytes,
			ProcessingStatus: item.ProcessingStatus,
			ExtractStatus:    item.ExtractStatus,
			RAGReady:         item.RAGReady,
			ChunkCount:       item.ChunkCount,
		})
	}
	return marshalPlatformResult(map[string]interface{}{
		"total": result.Total,
		"page":  args.Page,
		"files": items,
	})
}

// platformReadFile 读取用户文件内容：文本类读原文，其他类型读提取文本。
// 支持 offset 分页继续读取；单次输出上限 platformFileReadLimitBytes。
func (s *Service) platformReadFile(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		FileID string `json:"file_id"`
		Offset int    `json:"offset"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	fileID := strings.TrimSpace(args.FileID)
	if fileID == "" {
		return "", fmt.Errorf("file_id is required")
	}
	if args.Offset < 0 {
		args.Offset = 0
	}
	if s.uploadSvc == nil {
		return "", fmt.Errorf("file service is unavailable")
	}

	content, isText, truncated, err := s.readUserFileContent(ctx, call.UserID, fileID, args.Offset)
	if err != nil {
		return "", err
	}
	return marshalPlatformResult(map[string]interface{}{
		"file_id":   fileID,
		"is_text":   isText,
		"content":   content,
		"offset":    args.Offset,
		"truncated": truncated,
	})
}

// readUserFileContent 读取用户文件内容（文本类读原文，非文本读提取文本），
// 返回内容、是否原文、是否截断。单次最大 platformFileReadLimitBytes。
func (s *Service) readUserFileContent(ctx context.Context, userID uint, fileID string, offset int) (string, bool, bool, error) {
	result, err := s.uploadSvc.OpenFileContent(ctx, userID, fileID)
	if err != nil {
		return "", false, false, err
	}
	defer func() { _ = result.Reader.Close() }()
	if result.File.FileCategory == "text" {
		buf := make([]byte, platformFileReadLimitBytes+1)
		if offset > 0 {
			if _, skipErr := io.CopyN(io.Discard, result.Reader, int64(offset)); skipErr != nil && skipErr != io.EOF {
				return "", true, false, skipErr
			}
		}
		n, readErr := io.ReadFull(result.Reader, buf)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return "", true, false, readErr
		}
		if n > platformFileReadLimitBytes {
			return string(buf[:platformFileReadLimitBytes]), true, true, nil
		}
		return string(buf[:n]), true, false, nil
	}

	extract, extractErr := s.GetFileExtract(ctx, userID, fileID)
	if extractErr != nil {
		return "", false, false, fmt.Errorf("file has no readable text: %w", extractErr)
	}
	text := extract.ExtractText
	if offset > len(text) {
		offset = len(text)
	}
	end := offset + platformFileReadLimitBytes
	truncated := false
	if end > len(text) {
		end = len(text)
	} else {
		truncated = true
	}
	return text[offset:end], false, truncated, nil
}

// platformReadSkillFile 读取技能包内文件（清单白名单文本文件）。
func (s *Service) platformReadSkillFile(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		SkillID uint   `json:"skill_id"`
		Path    string `json:"path"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.SkillID == 0 || strings.TrimSpace(args.Path) == "" {
		return "", fmt.Errorf("skill_id and path are required")
	}
	if s.skillResolver == nil {
		return "", fmt.Errorf("skill service is unavailable")
	}
	content, err := s.skillResolver.GetPackageFile(ctx, call.UserID, args.SkillID, strings.TrimSpace(args.Path))
	if err != nil {
		return "", err
	}
	truncated := false
	if len(content) > platformSkillReadLimitBytes {
		content = content[:platformSkillReadLimitBytes]
		truncated = true
	}
	return marshalPlatformResult(map[string]interface{}{
		"skill_id":  args.SkillID,
		"path":      strings.TrimSpace(args.Path),
		"content":   string(content),
		"truncated": truncated,
	})
}

// platformListSkills 列出用户可见技能（含包文件清单）。
func (s *Service) platformListSkills(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Query string `json:"query"`
		Page  int    `json:"page"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.Page < 1 {
		args.Page = 1
	}
	if s.skillResolver == nil {
		return "", fmt.Errorf("skill service is unavailable")
	}
	items, total, err := s.skillResolver.ListVisible(ctx, call.UserID, skill.ListInput{
		Query:    args.Query,
		Page:     args.Page,
		PageSize: platformListPageSize,
	})
	if err != nil {
		return "", err
	}
	summary := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		entry := map[string]interface{}{
			"skill_id":    item.ID,
			"title":       item.Title,
			"trigger":     item.Trigger,
			"description": item.Description,
			"is_package":  item.IsPackage(),
		}
		if item.IsPackage() && len(item.PackageFiles) > 0 {
			files := make([]map[string]interface{}, 0, len(item.PackageFiles))
			for _, f := range item.PackageFiles {
				files = append(files, map[string]interface{}{
					"path": f.Path,
					"size": f.Size,
					"kind": f.Kind,
				})
			}
			entry["package_files"] = files
		}
		summary = append(summary, entry)
	}
	return marshalPlatformResult(map[string]interface{}{
		"total": total,
		"page":  args.Page,
		"skills": summary,
	})
}

// platformListConversations 列出用户会话（与用户搜索同一查询路径 SearchConversations，
// 覆盖归档会话；关键词匹配标题/标签/模型/项目/消息正文）。
func (s *Service) platformListConversations(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Query string `json:"query"`
		Page  int    `json:"page"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.Page < 1 {
		args.Page = 1
	}
	items, hasMore, err := s.SearchConversations(ctx, call.UserID, args.Page, platformListPageSize, args.Query)
	if err != nil {
		return "", err
	}
	type conversationSummary struct {
		ID        uint   `json:"conversation_id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		UpdatedAt string `json:"updated_at"`
	}
	summary := make([]conversationSummary, 0, len(items))
	for _, item := range items {
		summary = append(summary, conversationSummary{
			ID:        item.Conversation.ID,
			Title:     item.Conversation.Title,
			Status:    item.Conversation.Status,
			UpdatedAt: item.Conversation.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return marshalPlatformResult(map[string]interface{}{
		"has_more": hasMore,
		"page":     args.Page,
		"conversations": summary,
	})
}

// platformReadConversation 读取用户会话的消息历史（最近 N 条，按时间升序）。
func (s *Service) platformReadConversation(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		ConversationID uint `json:"conversation_id"`
		Limit          int  `json:"limit"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.ConversationID == 0 {
		return "", fmt.Errorf("conversation_id is required")
	}
	limit := args.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	conversation, err := s.GetConversation(ctx, call.UserID, args.ConversationID)
	if err != nil {
		return "", err
	}
	messages, _, err := s.ListMessages(ctx, call.UserID, args.ConversationID, 1, limit)
	if err != nil {
		return "", err
	}
	type messageSummary struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	summary := make([]messageSummary, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if len(content) > 4000 {
			content = content[:4000] + "…[truncated]"
		}
		summary = append(summary, messageSummary{Role: msg.Role, Content: content})
	}
	return marshalPlatformResult(map[string]interface{}{
		"conversation_id": args.ConversationID,
		"title":           conversation.Title,
		"messages":        summary,
	})
}

// decodePlatformArgs 解码平台工具参数（宽松：未知字段忽略）。
func decodePlatformArgs(raw json.RawMessage, target interface{}) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return nil
}

// marshalPlatformResult 序列化平台工具结果。
func marshalPlatformResult(payload interface{}) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode tool result: %w", err)
	}
	return string(data), nil
}

// platformListPromptPresets 列出用户可见提示词（含内置）。
func (s *Service) platformListPromptPresets(ctx context.Context, call platformToolCallContext) (string, error) {
	if s.promptPresets == nil {
		return "", fmt.Errorf("prompt preset service is unavailable")
	}
	items, total, err := s.promptPresets.ListVisible(ctx, call.UserID, apppromptpreset.ListInput{
		Page:     1,
		PageSize: platformListPageSize,
	})
	if err != nil {
		return "", err
	}
	summary := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		summary = append(summary, map[string]interface{}{
			"prompt_preset_id": item.ID,
			"title":            item.Title,
			"trigger":          item.Trigger,
			"description":      item.Description,
			"content":          item.Content,
			"enabled":          item.Enabled,
			"scope":            item.Scope,
		})
	}
	return marshalPlatformResult(map[string]interface{}{
		"prompt_presets": summary,
		"total":          total,
	})
}

// platformListDynamicPrompts 列出用户动态提示词脚本。
func (s *Service) platformListDynamicPrompts(ctx context.Context, call platformToolCallContext) (string, error) {
	if s.dynamicPrompts == nil {
		return "", fmt.Errorf("dynamic prompt service is unavailable")
	}
	items, err := s.dynamicPrompts.ListDynamicPrompts(ctx, call.UserID)
	if err != nil {
		return "", err
	}
	summary := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		summary = append(summary, map[string]interface{}{
			"prompt_id": item.PublicID,
			"name":      item.Name,
			"kind":      item.Kind,
			"enabled":   item.Enabled,
			"content":   item.Content,
		})
	}
	return marshalPlatformResult(map[string]interface{}{
		"dynamic_prompts": summary,
	})
}
