package conversation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	appdynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/dynamicprompt"
	apppromptpreset "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/promptpreset"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/objectstore"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/pkg/traceid"
)

// platformWriteFile 覆盖用户文本文件内容（写操作，受批准模式管控）。
func (s *Service) platformWriteFile(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		FileID  string `json:"file_id"`
		Content string `json:"content"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	fileID := strings.TrimSpace(args.FileID)
	if fileID == "" {
		return "", fmt.Errorf("file_id is required")
	}
	content := args.Content
	if len(content) > platformFileWriteLimitBytes {
		return "", fmt.Errorf("content exceeds %d bytes limit", platformFileWriteLimitBytes)
	}
	if err := s.overwriteFileContent(ctx, call.UserID, fileID, content); err != nil {
		return "", err
	}
	return marshalPlatformResult(map[string]interface{}{
		"file_id": fileID,
		"bytes":   len(content),
		"status":  "written",
		"note":    "file content updated; text extraction and RAG index will be rebuilt shortly",
	})
}

// overwriteFileContent 覆盖用户文件内容：
// 校验归属与文本类型 → 写新存储对象 → 更新元数据并重置处理状态 → 删除旧对象 →
// 延迟重建调度器登记（debounce，不立即触发提取/RAG 重建）。
func (s *Service) overwriteFileContent(ctx context.Context, userID uint, fileID string, content string) error {
	if userID == 0 || fileID == "" {
		return ErrInvalidFileReference
	}
	item, err := s.repo.GetActiveFileObjectByID(ctx, userID, fileID)
	if err != nil {
		return err
	}
	if !isPlatformWritableFile(item.FileCategory) {
		return fmt.Errorf("file %s is not a writable text file (category %s)", fileID, item.FileCategory)
	}
	if s.storeProvider == nil || s.processingSvc == nil {
		return fmt.Errorf("storage or processing service is unavailable")
	}
	store, err := s.storeProvider.Open(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	relativePath := filepath.ToSlash(filepath.Join(
		fmt.Sprintf("%d", userID),
		now.Format("2006"),
		now.Format("01"),
		"file_"+sanitizePlatformFileName(item.FileName),
	))
	body := bytes.NewBufferString(content)
	if _, err := store.Put(ctx, relativePath, body, objectstore.PutOptions{
		SizeBytes:   int64(len(content)),
		ContentType: firstNonEmptyString(item.MimeType, "text/plain"),
	}); err != nil {
		return fmt.Errorf("write file object: %w", err)
	}
	sum := sha256.Sum256([]byte(content))
	sha256Hex := hex.EncodeToString(sum[:])
	if err := s.repo.ReplaceFileObjectContent(ctx, userID, fileID, relativePath, sha256Hex, int64(len(content))); err != nil {
		_ = store.Delete(ctx, relativePath)
		return err
	}
	if relativePath != item.StoragePath {
		_ = store.Delete(ctx, item.StoragePath)
	}
	// 延迟重建：缓冲窗口内合并多次修改，到期才触发一次提取/RAG 重建。
	s.reindexScheduler.MarkDirty(userID, fileID)

	s.recordPlatformAudit(ctx, callCtx{userID: userID, requestID: traceid.FromContext(ctx)}, "platform_tools.write_file", fileID, map[string]interface{}{
		"file_name": item.FileName,
		"bytes":     len(content),
		"sha256":    sha256Hex,
	})
	return nil
}

// platformUpdateSkill 更新用户自己的技能元数据（写操作，受批准模式管控）。
func (s *Service) platformUpdateSkill(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		SkillID     uint   `json:"skill_id"`
		Title       string `json:"title"`
		Trigger     string `json:"trigger"`
		Description string `json:"description"`
		Markdown    string `json:"markdown"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.SkillID == 0 {
		return "", fmt.Errorf("skill_id is required")
	}
	if s.skillResolver == nil {
		return "", fmt.Errorf("skill service is unavailable")
	}
	patch := skill.PatchInput{}
	if args.Title != "" {
		value := args.Title
		patch.Title = &value
	}
	if args.Trigger != "" {
		value := args.Trigger
		patch.Trigger = &value
	}
	if args.Description != "" {
		value := args.Description
		patch.Description = &value
	}
	if args.Markdown != "" {
		value := args.Markdown
		patch.Markdown = &value
	}
	if args.Enabled != nil {
		patch.Enabled = args.Enabled
	}
	if !patchHasAnyField(patch) {
		return "", fmt.Errorf("at least one field to update is required")
	}
	updated, err := s.skillResolver.UpdateUser(ctx, call.UserID, args.SkillID, patch)
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.update_skill", fmt.Sprintf("%d", args.SkillID), map[string]interface{}{
		"title":   updated.Title,
		"trigger": updated.Trigger,
	})
	return marshalPlatformResult(map[string]interface{}{
		"skill_id": args.SkillID,
		"title":    updated.Title,
		"status":   "updated",
	})
}

// patchHasAnyField 判断 PatchInput 是否至少有一个字段被设置。
func patchHasAnyField(patch skill.PatchInput) bool {
	return patch.Title != nil || patch.Trigger != nil || patch.Description != nil || patch.Markdown != nil || patch.Enabled != nil || patch.SortOrder != nil
}

// platformDeleteFile 永久删除用户文件（写操作，受批准模式管控）。
func (s *Service) platformDeleteFile(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		FileID string `json:"file_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	fileID := strings.TrimSpace(args.FileID)
	if fileID == "" {
		return "", fmt.Errorf("file_id is required")
	}
	if s.uploadSvc == nil {
		return "", fmt.Errorf("file service is unavailable")
	}
	result, err := s.uploadSvc.DeleteFile(ctx, call.UserID, fileID)
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_file", fileID, map[string]interface{}{
		"deleted": result.Deleted,
	})
	return marshalPlatformResult(map[string]interface{}{
		"file_id": fileID,
		"deleted": result.Deleted,
		"note":    "file permanently deleted and storage quota released",
	})
}

// platformListUserSettings 列出用户个人设置（只读）。
func (s *Service) platformListUserSettings(ctx context.Context, call platformToolCallContext) (string, error) {
	if s.userSettingsSvc == nil {
		return "", fmt.Errorf("user settings service is unavailable")
	}
	settings, err := s.userSettingsSvc.ListSettings(ctx, call.UserID)
	if err != nil {
		return "", err
	}
	return marshalPlatformResult(map[string]interface{}{
		"settings": settings,
	})
}

// platformUpdateUserSetting 更新用户个人设置（白名单 key，写操作受批准模式管控）。
func (s *Service) platformUpdateUserSetting(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	key := strings.TrimSpace(args.Key)
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	if s.userSettingsSvc == nil {
		return "", fmt.Errorf("user settings service is unavailable")
	}
	updated, err := s.userSettingsSvc.PatchSettings(ctx, call.UserID, map[string]string{key: args.Value})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.update_user_setting", key, map[string]interface{}{
		"value": args.Value,
	})
	return marshalPlatformResult(map[string]interface{}{
		"key":     key,
		"value":   updated[key],
		"updated": true,
	})
}

// platformUpdateConversation 更新用户会话（标题/星标/归档/标签；写操作受批准模式管控）。
// conversation_id 为数字 id，内部统一转 publicID 走既有应用层方法。
func (s *Service) platformUpdateConversation(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		ConversationID uint     `json:"conversation_id"`
		Title          string   `json:"title"`
		Starred        *bool    `json:"starred"`
		Archived       *bool    `json:"archived"`
		Labels         []string `json:"labels"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.ConversationID == 0 {
		return "", fmt.Errorf("conversation_id is required")
	}
	conversation, err := s.GetConversation(ctx, call.UserID, args.ConversationID)
	if err != nil {
		return "", err
	}
	publicID := conversation.PublicID
	updated := conversation
	if strings.TrimSpace(args.Title) != "" {
		updated, err = s.RenameConversation(ctx, call.UserID, publicID, args.Title)
		if err != nil {
			return "", err
		}
	}
	if args.Starred != nil {
		updated, err = s.SetConversationStar(ctx, call.UserID, publicID, *args.Starred)
		if err != nil {
			return "", err
		}
	}
	if args.Archived != nil {
		updated, err = s.SetConversationArchived(ctx, call.UserID, publicID, *args.Archived)
		if err != nil {
			return "", err
		}
	}
	if args.Labels != nil {
		updated, err = s.UpdateConversationLabels(ctx, call.UserID, publicID, args.Labels)
		if err != nil {
			return "", err
		}
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.update_conversation", fmt.Sprintf("%d", args.ConversationID), map[string]interface{}{
		"title":    updated.Title,
		"starred":  updated.IsStarred,
		"archived": updated.Status == "archived",
	})
	return marshalPlatformResult(map[string]interface{}{
		"conversation_id": args.ConversationID,
		"title":           updated.Title,
		"starred":         updated.IsStarred,
		"archived":        updated.Status == "archived",
		"updated":         true,
	})
}

// isPlatformWritableFile 判断文件是否可被平台工具写入（仅文本类）。
func isPlatformWritableFile(category string) bool {
	return strings.EqualFold(strings.TrimSpace(category), "text")
}

// sanitizePlatformFileName 清理写入文件对象名称中的路径分隔符。
func sanitizePlatformFileName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "/", "_")
	base = strings.ReplaceAll(base, "\\", "_")
	base = strings.TrimSpace(base)
	if base == "." || base == "" {
		base = "file"
	}
	return base
}

type callCtx struct {
	userID    uint
	requestID string
}

// recordPlatformAudit 记录平台工具写操作审计。
func (s *Service) recordPlatformAudit(ctx context.Context, call callCtx, action string, resourceID string, detail interface{}) {
	if s.auditWriter == nil {
		return
	}
	s.auditWriter.Write(
		ctx,
		strings.TrimSpace(call.requestID),
		call.userID,
		action,
		"platform_tools",
		strings.TrimSpace(resourceID),
		"",
		"",
		detail,
	)
}

// platformCreateSkill 创建用户自己的技能（写操作，受批准模式管控）。
func (s *Service) platformCreateSkill(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Title       string `json:"title"`
		Trigger     string `json:"trigger"`
		Description string `json:"description"`
		Markdown    string `json:"markdown"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Title) == "" {
		return "", fmt.Errorf("title is required")
	}
	if s.skillResolver == nil {
		return "", fmt.Errorf("skill service is unavailable")
	}
	enabled := true
	if args.Enabled != nil {
		enabled = *args.Enabled
	}
	created, err := s.skillResolver.CreateUser(ctx, call.UserID, skill.WriteInput{
		Title:       strings.TrimSpace(args.Title),
		Trigger:     strings.TrimSpace(args.Trigger),
		Description: strings.TrimSpace(args.Description),
		Markdown:    args.Markdown,
		Enabled:     enabled,
	})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.create_skill", fmt.Sprintf("%d", created.ID), map[string]interface{}{
		"title":   created.Title,
		"trigger": created.Trigger,
	})
	return marshalPlatformResult(map[string]interface{}{
		"skill_id": created.ID,
		"title":    created.Title,
		"created":  true,
	})
}

// platformCreatePromptPreset 创建用户自定义提示词（写操作，受批准模式管控）。
func (s *Service) platformCreatePromptPreset(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Title       string `json:"title"`
		Trigger     string `json:"trigger"`
		Description string `json:"description"`
		Content     string `json:"content"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Title) == "" {
		return "", fmt.Errorf("title is required")
	}
	if s.promptPresets == nil {
		return "", fmt.Errorf("prompt preset service is unavailable")
	}
	enabled := true
	if args.Enabled != nil {
		enabled = *args.Enabled
	}
	created, err := s.promptPresets.CreateUser(ctx, call.UserID, apppromptpreset.WriteInput{
		Title:       strings.TrimSpace(args.Title),
		Trigger:     strings.TrimSpace(args.Trigger),
		Description: strings.TrimSpace(args.Description),
		Content:     args.Content,
		Enabled:     enabled,
	})
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.create_prompt_preset", fmt.Sprintf("%d", created.ID), map[string]interface{}{
		"title":   created.Title,
		"trigger": created.Trigger,
	})
	return marshalPlatformResult(map[string]interface{}{
		"prompt_preset_id": created.ID,
		"title":            created.Title,
		"trigger":          created.Trigger,
		"created":          true,
	})
}

// platformUpdatePromptPreset 更新用户自定义提示词（写操作，受批准模式管控；内置提示词不可改）。
func (s *Service) platformUpdatePromptPreset(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		PromptPresetID uint    `json:"prompt_preset_id"`
		Title          *string `json:"title"`
		Trigger        *string `json:"trigger"`
		Description    *string `json:"description"`
		Content        *string `json:"content"`
		Enabled        *bool   `json:"enabled"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.PromptPresetID == 0 {
		return "", fmt.Errorf("prompt_preset_id is required")
	}
	if s.promptPresets == nil {
		return "", fmt.Errorf("prompt preset service is unavailable")
	}
	patch := apppromptpreset.PatchInput{
		Title:       args.Title,
		Trigger:     args.Trigger,
		Description: args.Description,
		Content:     args.Content,
		Enabled:     args.Enabled,
	}
	updated, err := s.promptPresets.UpdateUser(ctx, call.UserID, args.PromptPresetID, patch)
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.update_prompt_preset", fmt.Sprintf("%d", args.PromptPresetID), map[string]interface{}{
		"title":   updated.Title,
		"trigger": updated.Trigger,
	})
	return marshalPlatformResult(map[string]interface{}{
		"prompt_preset_id": args.PromptPresetID,
		"title":            updated.Title,
		"trigger":          updated.Trigger,
		"updated":          true,
	})
}

// platformDeletePromptPreset 删除用户自定义提示词（写操作，受批准模式管控；内置提示词不可删）。
func (s *Service) platformDeletePromptPreset(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		PromptPresetID uint `json:"prompt_preset_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.PromptPresetID == 0 {
		return "", fmt.Errorf("prompt_preset_id is required")
	}
	if s.promptPresets == nil {
		return "", fmt.Errorf("prompt preset service is unavailable")
	}
	if err := s.promptPresets.DeleteUser(ctx, call.UserID, args.PromptPresetID); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_prompt_preset", fmt.Sprintf("%d", args.PromptPresetID), nil)
	return marshalPlatformResult(map[string]interface{}{
		"prompt_preset_id": args.PromptPresetID,
		"deleted":          true,
	})
}

// platformCreateDynamicPrompt 创建用户动态提示词脚本（写操作，受批准模式管控）。
func (s *Service) platformCreateDynamicPrompt(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Content string `json:"content"`
		Enabled *bool  `json:"enabled"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Name) == "" {
		return "", fmt.Errorf("name is required")
	}
	if s.dynamicPrompts == nil {
		return "", fmt.Errorf("dynamic prompt service is unavailable")
	}
	created, err := s.dynamicPrompts.UpsertDynamicPrompt(ctx, call.UserID, "", appdynamicprompt.UpsertInput{
		Name:    strings.TrimSpace(args.Name),
		Kind:    strings.TrimSpace(args.Kind),
		Content: args.Content,
		Enabled: args.Enabled,
	}, "ai")
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.create_dynamic_prompt", created.PublicID, map[string]interface{}{
		"name": created.Name,
	})
	return marshalPlatformResult(map[string]interface{}{
		"prompt_id": created.PublicID,
		"name":      created.Name,
		"created":   true,
	})
}

// platformUpdateDynamicPrompt 更新用户动态提示词脚本（写操作，受批准模式管控）。
func (s *Service) platformUpdateDynamicPrompt(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		PromptID string  `json:"prompt_id"`
		Name     *string `json:"name"`
		Kind     *string `json:"kind"`
		Content  *string `json:"content"`
		Enabled  *bool   `json:"enabled"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	promptID := strings.TrimSpace(args.PromptID)
	if promptID == "" {
		return "", fmt.Errorf("prompt_id is required")
	}
	if s.dynamicPrompts == nil {
		return "", fmt.Errorf("dynamic prompt service is unavailable")
	}
	input := appdynamicprompt.UpsertInput{}
	if args.Name != nil {
		input.Name = strings.TrimSpace(*args.Name)
	}
	if args.Kind != nil {
		input.Kind = strings.TrimSpace(*args.Kind)
	}
	if args.Content != nil {
		input.Content = *args.Content
	}
	input.Enabled = args.Enabled
	updated, err := s.dynamicPrompts.UpsertDynamicPrompt(ctx, call.UserID, promptID, input, "ai")
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.update_dynamic_prompt", updated.PublicID, map[string]interface{}{
		"name": updated.Name,
	})
	return marshalPlatformResult(map[string]interface{}{
		"prompt_id": updated.PublicID,
		"name":      updated.Name,
		"updated":   true,
	})
}

// platformDeleteDynamicPrompt 删除用户动态提示词脚本（写操作，受批准模式管控）。
func (s *Service) platformDeleteDynamicPrompt(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		PromptID string `json:"prompt_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	promptID := strings.TrimSpace(args.PromptID)
	if promptID == "" {
		return "", fmt.Errorf("prompt_id is required")
	}
	if s.dynamicPrompts == nil {
		return "", fmt.Errorf("dynamic prompt service is unavailable")
	}
	if err := s.dynamicPrompts.DeleteDynamicPrompt(ctx, call.UserID, promptID); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_dynamic_prompt", promptID, nil)
	return marshalPlatformResult(map[string]interface{}{
		"prompt_id": promptID,
		"deleted":   true,
	})
}

// platformRunDynamicPrompt 执行用户动态提示词脚本（js 沙箱 1s/4KB 截断；text 直返）。
// 供 AI 验证自己创建的脚本；写类操作，受批准模式管控。
func (s *Service) platformRunDynamicPrompt(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		PromptID string `json:"prompt_id"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	promptID := strings.TrimSpace(args.PromptID)
	if promptID == "" {
		return "", fmt.Errorf("prompt_id is required")
	}
	if s.dynamicPrompts == nil {
		return "", fmt.Errorf("dynamic prompt service is unavailable")
	}
	result, err := s.dynamicPrompts.RunDynamicPrompt(ctx, call.UserID, promptID)
	if err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.run_dynamic_prompt", promptID, nil)
	return marshalPlatformResult(map[string]interface{}{
		"prompt_id": promptID,
		"result":    result,
	})
}
