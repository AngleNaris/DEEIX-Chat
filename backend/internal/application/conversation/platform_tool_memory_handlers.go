package conversation

import (
	"context"
	"fmt"
	"strings"
)

// 平台工具记忆域：save_memory / delete_memory / list_memories。
// 让模型像 Hermes 一样自主管理用户长期记忆：保存（含更新）、删除、查看。
// 写工具与文件/技能等写工具同权限体系：write_enabled 开关 + 用户批准模式（auto/ask）。
// 记忆写入后经现有注入通道生效：preference 无条件注入（400 token 槽位）、
// profile/custom 按语义相关性召回（topK=5），异步向量化失败静默走关键词兜底。

const (
	// platformMemoryKeyMaxLen 记忆 key 上限，与 HTTP DTO binding max=128 对齐。
	platformMemoryKeyMaxLen = 128
	// platformMemoryValueMaxLen 记忆 value 上限，与 HTTP DTO binding max=10000 对齐。
	platformMemoryValueMaxLen = 10000
	// platformMemoryListValuePreview list_memories 返回 value 摘要的最大字符数（按 rune 截断）。
	platformMemoryListValuePreview = 200
)

// platformSaveMemory 保存或更新用户长期记忆（写操作，受批准模式管控）。
func (s *Service) platformSaveMemory(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Scope string `json:"scope"`
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
	scope := strings.TrimSpace(args.Scope)
	if scope == "" {
		// 默认归入语义召回域（profile/custom），避免 preference 无条件注入被滥用。
		scope = "custom"
	}
	switch scope {
	case "preference", "profile", "custom":
	default:
		return "", fmt.Errorf("invalid scope %q (allowed: preference, profile, custom)", scope)
	}
	if s.memoryRecorder == nil {
		return "", fmt.Errorf("memory service is unavailable")
	}
	if err := s.memoryRecorder.UpsertUserMemory(ctx, call.UserID, key, value, scope, "ai"); err != nil {
		return "", err
	}
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.save_memory", key, map[string]interface{}{
		"key":   key,
		"scope": scope,
	})
	return marshalPlatformResult(map[string]interface{}{
		"saved": true,
		"key":   key,
		"scope": scope,
		"note":  "memory upserted; it will be recalled in future conversations",
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

// platformListMemories 列出用户长期记忆（只读）。可选 scope 过滤；
// value 返回摘要（按 rune 截断到 200 字符），避免撑爆上下文。
func (s *Service) platformListMemories(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Scope string `json:"scope"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	scope := strings.TrimSpace(args.Scope)
	if scope != "" && scope != "preference" && scope != "profile" && scope != "custom" {
		return "", fmt.Errorf("invalid scope %q (allowed: preference, profile, custom)", scope)
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
		Scope     string `json:"scope"`
		Value     string `json:"value"`
		UpdatedAt string `json:"updated_at"`
	}
	summaries := make([]memorySummary, 0, len(items))
	for _, item := range items {
		if scope != "" && item.Scope != scope {
			continue
		}
		value := item.Value
		if runes := []rune(value); len(runes) > platformMemoryListValuePreview {
			value = string(runes[:platformMemoryListValuePreview]) + "…"
		}
		summaries = append(summaries, memorySummary{
			Key:       item.MemoryKey,
			Scope:     item.Scope,
			Value:     value,
			UpdatedAt: item.UpdatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return marshalPlatformResult(map[string]interface{}{
		"total":    len(summaries),
		"memories": summaries,
	})
}
