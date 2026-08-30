package conversation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	domainmemory "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/memory"
)

// fakeMemoryRecorder 模拟 memoryRecorder 窄接口，记录调用供断言。
type fakeMemoryRecorder struct {
	upserted map[string]string // memoryKey → "scope|value|updatedBy"
	deleted  []string
	items    []domainmemory.UserMemory
}

func (f *fakeMemoryRecorder) UpsertUserMemory(ctx context.Context, userID uint, memoryKey string, value string, scope string, updatedBy string) error {
	if f.upserted == nil {
		f.upserted = map[string]string{}
	}
	f.upserted[memoryKey] = fmt.Sprintf("%s|%s|%s", scope, value, updatedBy)
	return nil
}

func (f *fakeMemoryRecorder) DeleteUserMemory(ctx context.Context, userID uint, memoryKey string) error {
	f.deleted = append(f.deleted, memoryKey)
	return nil
}

func (f *fakeMemoryRecorder) ListUserMemories(ctx context.Context, userID uint) ([]domainmemory.UserMemory, error) {
	return f.items, nil
}

func (f *fakeMemoryRecorder) SearchUserMemoriesByEmbedding(ctx context.Context, userID uint, queryEmbedding []float32, embeddingSignature string, topK int, minSimilarity float64) ([]domainmemory.UserMemory, error) {
	return nil, nil
}

func (f *fakeMemoryRecorder) UpsertUserMemoryEmbedding(ctx context.Context, userID uint, memoryKey string, expectedValue string, embedding []float32, embeddingSignature string) error {
	return nil
}

func TestPlatformSaveMemory(t *testing.T) {
	rec := &fakeMemoryRecorder{}
	svc := &Service{memoryRecorder: rec}

	// 默认 category 为 context，写入方标注 ai。
	out, err := svc.platformSaveMemory(context.Background(), platformToolCallContext{
		UserID:    1,
		RequestID: "req-1",
		Arguments: json.RawMessage(`{"key":"language_preference","value":"prefers Chinese"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("bad output %q: %v", out, err)
	}
	if result["saved"] != true || result["category"] != "context" || result["key"] != "language_preference" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := rec.upserted["language_preference"]; got != "context|prefers Chinese|ai" {
		t.Fatalf("unexpected upsert record %q", got)
	}

	// 指定 category 与同 key 更新。
	if _, err := svc.platformSaveMemory(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"key":"language_preference","value":"prefers Japanese","category":"preference"}`),
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := rec.upserted["language_preference"]; got != "preference|prefers Japanese|ai" {
		t.Fatalf("unexpected updated record %q", got)
	}
}

func TestPlatformSaveMemoryValidation(t *testing.T) {
	svc := &Service{memoryRecorder: &fakeMemoryRecorder{}}
	cases := []struct {
		name    string
		args    string
		message string
	}{
		{"missing key", `{"value":"v"}`, "key is required"},
		{"missing value", `{"key":"k"}`, "value is required"},
		{"key too long", fmt.Sprintf(`{"key":%q,"value":"v"}`, strings.Repeat("k", platformMemoryKeyMaxLen+1)), "key exceeds"},
		{"value too long", fmt.Sprintf(`{"key":"k","value":%q}`, strings.Repeat("v", platformMemoryValueMaxLen+1)), "value exceeds"},
		{"invalid category", `{"key":"k","value":"v","category":"system"}`, "invalid category"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.platformSaveMemory(context.Background(), platformToolCallContext{
				UserID:    1,
				Arguments: json.RawMessage(tc.args),
			})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected error containing %q, got %v", tc.message, err)
			}
		})
	}

	// memoryRecorder 缺失时报错而不是 panic。
	noSvc := &Service{}
	if _, err := noSvc.platformSaveMemory(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"key":"k","value":"v"}`),
	}); err == nil {
		t.Fatalf("expected error when memory service unavailable")
	}
}

func TestPlatformDeleteMemory(t *testing.T) {
	rec := &fakeMemoryRecorder{}
	svc := &Service{memoryRecorder: rec}
	out, err := svc.platformDeleteMemory(context.Background(), platformToolCallContext{
		UserID:    1,
		RequestID: "req-2",
		Arguments: json.RawMessage(`{"key":"language_preference"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"deleted":true`) {
		t.Fatalf("unexpected output %q", out)
	}
	if len(rec.deleted) != 1 || rec.deleted[0] != "language_preference" {
		t.Fatalf("unexpected deleted records: %+v", rec.deleted)
	}

	if _, err := svc.platformDeleteMemory(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{}`),
	}); err == nil {
		t.Fatalf("expected error when key missing")
	}
}

func TestPlatformListMemories(t *testing.T) {
	longValue := strings.Repeat("汉", 300)
	rec := &fakeMemoryRecorder{items: []domainmemory.UserMemory{
		{MemoryKey: "topic", Scope: "preference", Value: "likes databases", UpdatedAt: time.Now()},
		{MemoryKey: "fact", Scope: "custom", Value: longValue, UpdatedAt: time.Now()},
	}}
	svc := &Service{memoryRecorder: rec}

	// 全量返回，超长 value 截断为摘要。
	out, err := svc.platformListMemories(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result struct {
		Total    int `json:"total"`
		Memories []struct {
			Key      string `json:"key"`
			Category string `json:"category"`
			Value    string `json:"value"`
		} `json:"memories"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("bad output: %v", err)
	}
	if result.Total != 2 || len(result.Memories) != 2 {
		t.Fatalf("unexpected list: %+v", result)
	}
	valueRunes := []rune(result.Memories[1].Value)
	if len(valueRunes) != platformMemoryListValuePreview+1 {
		t.Fatalf("value must be truncated to %d runes + ellipsis, got %d", platformMemoryListValuePreview, len(valueRunes))
	}

	// category/query/limit 过滤。
	out, err = svc.platformListMemories(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"category":"preference","query":"database","limit":1}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("bad output: %v", err)
	}
	if result.Total != 1 || result.Memories[0].Key != "topic" {
		t.Fatalf("unexpected filtered list: %+v", result)
	}

	// 无效 category 与越界 limit 拒绝。
	if _, err := svc.platformListMemories(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"category":"system"}`),
	}); err == nil {
		t.Fatalf("expected error for invalid category")
	}
	if _, err := svc.platformListMemories(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"limit":51}`),
	}); err == nil {
		t.Fatalf("expected error for invalid limit")
	}
}
