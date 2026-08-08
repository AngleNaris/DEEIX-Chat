package conversation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

// fakePlatformSettingsReader 模拟 platform_tools 运行时设置读取器。
type fakePlatformSettingsReader struct {
	values map[string]string
}

func (f *fakePlatformSettingsReader) RuntimeValuesByNamespace(ctx context.Context, namespace string) (map[string]string, error) {
	if f == nil || f.values == nil {
		return map[string]string{}, nil
	}
	return f.values, nil
}

func newTestServiceWithPlatformSettings(values map[string]string) *Service {
	return &Service{
		platformToolsSettings: &fakePlatformSettingsReader{values: values},
		platformApprovals:     newPlatformWriteApprovalStore(),
	}
}

func TestAppendPlatformToolRuntimeDisabled(t *testing.T) {
	svc := newTestServiceWithPlatformSettings(map[string]string{
		platformToolsKeyEnabled: "false",
	})
	var result selectedToolRuntime
	if err := svc.appendPlatformToolRuntime(context.Background(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.definitions) != 0 || len(result.platformEntries) != 0 {
		t.Fatalf("expected no platform tools when disabled, got %d definitions", len(result.definitions))
	}
}

func TestAppendPlatformToolRuntimeReadOnlyOnly(t *testing.T) {
	svc := newTestServiceWithPlatformSettings(map[string]string{
		platformToolsKeyEnabled:      "true",
		platformToolsKeyWriteEnabled: "false",
	})
	var result selectedToolRuntime
	if err := svc.appendPlatformToolRuntime(context.Background(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	readCount := 0
	writeCount := 0
	for name, entry := range result.platformEntries {
		if name == "" {
			t.Fatalf("empty model name in platform entries")
		}
		if entry.kind == platformToolWrite {
			writeCount++
		} else {
			readCount++
		}
	}
	if writeCount != 0 {
		t.Fatalf("write tools must not be injected when write_enabled=false, got %d", writeCount)
	}
	if readCount == 0 {
		t.Fatalf("read tools must be injected when enabled=true")
	}
	// 所有定义在 nameMap/schemas 中成对出现。
	if len(result.definitions) != len(result.platformEntries) {
		t.Fatalf("definitions/entries mismatch: %d vs %d", len(result.definitions), len(result.platformEntries))
	}
	for _, def := range result.definitions {
		if _, ok := result.nameMap[def.Name]; !ok {
			t.Fatalf("definition %q missing from nameMap", def.Name)
		}
	}
}

func TestAppendPlatformToolRuntimeWriteEnabled(t *testing.T) {
	svc := newTestServiceWithPlatformSettings(map[string]string{
		platformToolsKeyEnabled:      "true",
		platformToolsKeyWriteEnabled: "true",
	})
	var result selectedToolRuntime
	if err := svc.appendPlatformToolRuntime(context.Background(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hasWrite := false
	for _, entry := range result.platformEntries {
		if entry.kind == platformToolWrite {
			hasWrite = true
		}
	}
	if !hasWrite {
		t.Fatalf("write tools must be injected when write_enabled=true")
	}
}

func TestAppendPlatformToolRuntimeNameCollisionWithMCP(t *testing.T) {
	svc := newTestServiceWithPlatformSettings(map[string]string{
		platformToolsKeyEnabled: "true",
	})
	var result selectedToolRuntime
	result.definitions = append(result.definitions, llmToolDefinition("read_file", "mcp tool"))
	result.nameMap = map[string]string{"read_file": "mcp_read_file"}
	result.schemas = map[string]json.RawMessage{"read_file": json.RawMessage(`{"type":"object"}`)}
	if err := svc.appendPlatformToolRuntime(context.Background(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 平台工具应被重命名避免冲突。
	found := false
	for name := range result.platformEntries {
		if name == "read_file" {
			found = true
		}
	}
	if found {
		t.Fatalf("platform read_file must be renamed when MCP tool uses the name")
	}
	seen := map[string]bool{}
	for _, def := range result.definitions {
		if seen[def.Name] {
			t.Fatalf("duplicate definition name %q", def.Name)
		}
		seen[def.Name] = true
	}
}

func TestExecutePlatformToolCallAutoApproval(t *testing.T) {
	svc := newTestServiceWithPlatformSettings(nil)
	called := false
	entry := platformToolEntry{
		definition: llmToolDefinition("write_file", "test write"),
		kind:       platformToolWrite,
		handler: func(s *Service, ctx context.Context, call platformToolCallContext) (string, error) {
			called = true
			return `{"ok":true}`, nil
		},
	}
	// userID=0 → 默认 auto 直接执行。
	output, err := svc.executePlatformToolCall(context.Background(), entry, ExecuteToolInput{
		UserID:        0,
		ArgumentsJSON: `{"file_id":"f1","content":"hi"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatalf("auto mode must execute handler directly")
	}
	if output != `{"ok":true}` {
		t.Fatalf("unexpected output %q", output)
	}
}

func TestPlatformWriteApprovalStoreLifecycle(t *testing.T) {
	store := newPlatformWriteApprovalStore()
	defer store.Stop()
	entry := platformToolRegistry()["write_file"]
	record := store.create(1, 2, "req-1", entry, `{"file_id":"f1","content":"x"}`)
	if record == nil || record.ID == "" || record.Status != platformApprovalStatusPending {
		t.Fatalf("unexpected record: %+v", record)
	}
	if got := store.get(record.ID); got != record {
		t.Fatalf("get mismatch")
	}
	// 非属主无法认领。
	if claimed := store.claim(record.ID, 99, platformApprovalStatusApproved); claimed != nil {
		t.Fatalf("non-owner must not claim")
	}
	if record.Status != platformApprovalStatusPending {
		t.Fatalf("status must stay pending after failed claim")
	}
	// 属主批准。
	if claimed := store.claim(record.ID, 1, platformApprovalStatusApproved); claimed != record {
		t.Fatalf("owner claim failed")
	}
	if record.Status != platformApprovalStatusApproved {
		t.Fatalf("expected approved, got %s", record.Status)
	}
	// 已处理记录不可重复认领。
	if claimed := store.claim(record.ID, 1, platformApprovalStatusRejected); claimed != nil {
		t.Fatalf("processed record must not be claimed again")
	}
}

func TestPlatformToolRegistryShape(t *testing.T) {
	registry := platformToolRegistry()
	if len(registry) < 6 {
		t.Fatalf("expected at least 6 platform tools, got %d", len(registry))
	}
	for name, entry := range registry {
		if name == "" || entry.definition.Name == "" || entry.handler == nil {
			t.Fatalf("tool %q has incomplete entry", name)
		}
		if entry.kind != platformToolRead && entry.kind != platformToolWrite {
			t.Fatalf("tool %q has invalid kind %q", name, entry.kind)
		}
		var schema map[string]interface{}
		if err := json.Unmarshal(entry.definition.InputSchema, &schema); err != nil {
			t.Fatalf("tool %q has invalid schema: %v", name, err)
		}
	}
	// 写工具都有审计动作。
	for name, entry := range registry {
		if entry.kind == platformToolWrite && entry.auditAction == "" {
			t.Fatalf("write tool %q missing auditAction", name)
		}
	}
}

func TestPlatformPureHelpers(t *testing.T) {
	if !isPlatformWritableFile("text") || isPlatformWritableFile("pdf") || isPlatformWritableFile("") {
		t.Fatalf("isPlatformWritableFile wrong")
	}
	if got := sanitizePlatformFileName("../a/b.txt"); got != "b.txt" {
		t.Fatalf("sanitize got %q", got)
	}
	if got := sanitizePlatformFileName(""); got != "file" {
		t.Fatalf("sanitize empty got %q", got)
	}
	if patchHasAnyField(skillPatchInput()) {
		t.Fatalf("empty patch must be false")
	}
	var args struct {
		FileID string `json:"file_id"`
	}
	if err := decodePlatformArgs(json.RawMessage(`{"file_id":"f1","extra":1}`), &args); err != nil || args.FileID != "f1" {
		t.Fatalf("decodePlatformArgs failed: %v %+v", err, args)
	}
	if err := decodePlatformArgs(json.RawMessage(`{bad`), &args); err == nil {
		t.Fatalf("invalid json must error")
	}
}

// llmToolDefinition 构造测试用工具定义。
func llmToolDefinition(name, description string) llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        name,
		Description: description,
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	}
}

// skillPatchInput 构造空 PatchInput。
func skillPatchInput() skill.PatchInput {
	return skill.PatchInput{}
}

func TestPlatformUpdateUserSettingRequiresService(t *testing.T) {
	svc := newTestServiceWithPlatformSettings(nil)
	output, err := svc.platformUpdateUserSetting(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"key":"chat.file_mode","value":"rag"}`),
	})
	if err == nil {
		t.Fatalf("expected error when user settings service is unavailable, got %q", output)
	}
}

func TestPlatformCreateRoleRequiresName(t *testing.T) {
	svc := newTestServiceWithPlatformSettings(nil)
	output, err := svc.platformCreateRole(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"description":"no name"}`),
	})
	if err == nil {
		t.Fatalf("expected error when name missing, got %q", output)
	}
}
