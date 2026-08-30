package conversation

import (
	"context"
	"fmt"
	"strings"

	domainmemory "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/memory"
)

// 平台工具记忆域：save_memory / delete_memory / list_memories。
// 让模型像 Hermes 一样自主管理用户长期记忆：保存（含更新）、删除、查看。
// 写工具与文件/技能等写工具同权限体系：write_enabled 开关 + 用户批准模式（auto/ask）。
// 记忆写入后经现有注入通道生效：preference 无条件注入（400 token 槽位）、
// capability/experience 按语义相关性召回（topK=5），其他类别由模型按需读取。

const (
	// platformMemoryKeyMaxLen 记忆 key 上限，与 HTTP DTO binding max=128 对齐。
	platformMemoryKeyMaxLen = 128
	// platformMemoryValueMaxLen 记忆 value 上限，与 HTTP DTO binding max=10000 对齐。
	platformMemoryValueMaxLen = 10000
	// platformMemoryListValuePreview list_memories 返回 value 摘要的最大字符数（按 rune 截断）。
	platformMemoryListValuePreview = 200
	platformMemoryListDefaultLimit = 20
	platformMemoryListMaxLimit     = 50
)

// platformSaveMemory 保存或更新用户长期记忆（写操作，受批准模式管控）。
func (s *Service) platformSaveMemory(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		Category string `json:"category"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	key := strings.TrimSpace(args.Key)
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	if len(key) > platformMemoryKeyMaxLen {
		return "", fmt.Errorf("key exceeds %d characters", platformMemoryKeyMaxLen)
	}
	value := strings.TrimSpace(args.Value)
	if value == "" {
		return "", fmt.Errorf("value is required")
	}
	if len(value) > platformMemoryValueMaxLen {
		return "", fmt.Errorf("value exceeds %d characters", platformMemoryValueMaxLen)
	}
	category := strings.TrimSpace(args.Category)
	if category == "" {
		category = domainmemory.CategoryContext
	}
	canonicalCategory, ok := domainmemory.CanonicalCategory(category)
	if !ok {
		return "", fmt.Errorf("invalid category %q", category)
	}
	if s.memoryRecorder == nil {
		return "", fmt.Errorf("memory service is unavailable")
	}
	if err := s.memoryRecorder.UpsertUserMemory(ctx, call.UserID, key, value, canonicalCategory, "ai"); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.save_memory", key, map[string]interface{}{
		"key":      key,
		"category": canonicalCategory,
	})
	return marshalPlatformResult(map[string]interface{}{
		"saved":    true,
		"key":      key,
		"category": canonicalCategory,
		"note":     "memory upserted",
	})
}

// platformDeleteMemory 删除用户长期记忆（写操作，受批准模式管控）。
func (s *Service) platformDeleteMemory(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Key string `json:"key"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	key := strings.TrimSpace(args.Key)
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	if s.memoryRecorder == nil {
		return "", fmt.Errorf("memory service is unavailable")
	}
	if err := s.memoryRecorder.DeleteUserMemory(ctx, call.UserID, key); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.delete_memory", key, map[string]interface{}{
		"key": key,
	})
	return marshalPlatformResult(map[string]interface{}{
		"deleted": true,
		"key":     key,
	})
}

// platformListMemories 列出用户长期记忆（只读）。可选 category/query 过滤；
// value 返回摘要（按 rune 截断到 200 字符），避免撑爆上下文。
func (s *Service) platformListMemories(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Category string `json:"category"`
		Query    string `json:"query"`
		Limit    int    `json:"limit"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	category := strings.TrimSpace(args.Category)
	if category != "" {
		canonicalCategory, ok := domainmemory.CanonicalCategory(category)
		if !ok {
			return "", fmt.Errorf("invalid category %q", category)
		}
		category = canonicalCategory
	}
	limit := args.Limit
	if limit == 0 {
		limit = platformMemoryListDefaultLimit
	}
	if limit < 1 || limit > platformMemoryListMaxLimit {
		return "", fmt.Errorf("limit must be between 1 and %d", platformMemoryListMaxLimit)
	}
	if s.memoryRecorder == nil {
		return "", fmt.Errorf("memory service is unavailable")
	}
	items, err := s.memoryRecorder.ListUserMemories(ctx, call.UserID)
	if err != nil {
		return "", err
	}
	type memorySummary struct {
		Key       string `json:"key"`
		Category  string `json:"category"`
		Value     string `json:"value"`
		UpdatedAt string `json:"updated_at"`
	}
	summaries := make([]memorySummary, 0, len(items))
	query := strings.ToLower(strings.TrimSpace(args.Query))
	for _, item := range items {
		itemCategory := domainmemory.NormalizeCategory(item.Scope)
		if category != "" && itemCategory != category {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.MemoryKey+" "+item.Value), query) {
			continue
		}
		value := item.Value
		if runes := []rune(value); len(runes) > platformMemoryListValuePreview {
			value = string(runes[:platformMemoryListValuePreview]) + "…"
		}
		summaries = append(summaries, memorySummary{
			Key:       item.MemoryKey,
			Category:  itemCategory,
			Value:     value,
			UpdatedAt: item.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
		if len(summaries) >= limit {
			break
		}
	}
	return marshalPlatformResult(map[string]interface{}{
		"total":    len(summaries),
		"memories": summaries,
	})
}
