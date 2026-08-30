package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	appartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/artifact"
	appcredentials "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/credentials"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
	domainartifact "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/artifact"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
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

type recordingCredentialResolver struct {
	mu          sync.Mutex
	createCalls int
	updateCalls int
	deleteCalls int
	lastUserID  uint
	lastInput   appcredentials.UpsertInput
	lastName    string
	writeErr    error
}

func (r *recordingCredentialResolver) ListCredentials(context.Context, uint) ([]appcredentials.View, error) {
	return nil, nil
}

func (r *recordingCredentialResolver) CreateCredential(_ context.Context, userID uint, input appcredentials.UpsertInput) (*appcredentials.View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	r.lastUserID = userID
	r.lastInput = input
	if r.writeErr != nil {
		return nil, r.writeErr
	}
	return &appcredentials.View{Name: input.Name, Type: input.Type, Description: input.Description}, nil
}

func (r *recordingCredentialResolver) UpdateCredentialByName(_ context.Context, userID uint, name string, input appcredentials.UpsertInput) (*appcredentials.View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateCalls++
	r.lastUserID = userID
	r.lastName = name
	r.lastInput = input
	if r.writeErr != nil {
		return nil, r.writeErr
	}
	return &appcredentials.View{Name: name, Type: input.Type, Description: input.Description}, nil
}

func (r *recordingCredentialResolver) DeleteCredentialByName(_ context.Context, userID uint, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteCalls++
	r.lastUserID = userID
	r.lastName = name
	return r.writeErr
}

func (*recordingCredentialResolver) ResolveValue(context.Context, uint, string) (string, error) {
	return "", nil
}

func (r *recordingCredentialResolver) totalWrites() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createCalls + r.updateCalls + r.deleteCalls
}

type recordingPlatformAuditWriter struct {
	payloads []string
}

func (w *recordingPlatformAuditWriter) Write(_ context.Context, _ string, _ uint, action string, resource string, resourceID string, _ string, _ string, detail interface{}) {
	payload, _ := json.Marshal(map[string]interface{}{
		"action":      action,
		"resource":    resource,
		"resource_id": resourceID,
		"detail":      detail,
	})
	w.payloads = append(w.payloads, string(payload))
}

func newAskCredentialService(t *testing.T, resolver *recordingCredentialResolver) *Service {
	t.Helper()
	store := newPlatformWriteApprovalStore()
	t.Cleanup(store.Stop)
	return &Service{
		cfg: config.NewRuntime(config.Config{}),
		repo: &mutableUserSettingsRepository{values: map[uint]map[string]string{
			7: {platformToolsWriteApprovalKey: platformToolsWriteApprovalAsk},
		}},
		credentials: resolver,
		platformToolsSettings: &fakePlatformSettingsReader{values: map[string]string{
			platformToolsKeyEnabled:      "true",
			platformToolsKeyWriteEnabled: "true",
		}},
		platformApprovals: store,
	}
}

type approvalPersistenceRepository struct {
	*mutableUserSettingsRepository
	mu      sync.Mutex
	rows    []model.ToolCall
	updates int
}

type approvalArtifactRepository struct {
	repository.ArtifactRepository
	created *domainartifact.Artifact
}

func (r *approvalArtifactRepository) CreateArtifact(_ context.Context, item *domainartifact.Artifact) error {
	r.created = item
	return nil
}

func (r *approvalPersistenceRepository) ListConversationToolCallsByRunID(
	_ context.Context,
	userID uint,
	conversationID uint,
	runID string,
) ([]model.ToolCall, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]model.ToolCall, 0, len(r.rows))
	for _, row := range r.rows {
		if row.UserID == userID && row.ConversationID == conversationID && row.RunID == runID {
			result = append(result, row)
		}
	}
	return result, nil
}

func (r *approvalPersistenceRepository) UpdateConversationToolCallPayload(
	_ context.Context,
	userID uint,
	conversationID uint,
	runID string,
	item model.ToolCall,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.rows {
		if r.rows[i].ID == item.ID && r.rows[i].UserID == userID && r.rows[i].ConversationID == conversationID && r.rows[i].RunID == runID {
			r.rows[i].OutputJSON = item.OutputJSON
			r.rows[i].ErrorJSON = item.ErrorJSON
			r.updates++
			return nil
		}
	}
	return errors.New("tool call not found")
}

func (r *approvalPersistenceRepository) snapshot() (model.ToolCall, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rows[0], r.updates
}

func approvalRecordFromOutput(t *testing.T, service *Service, output string) *platformWriteApproval {
	t.Helper()
	var pending struct {
		Status     string `json:"status"`
		ApprovalID string `json:"approval_id"`
	}
	if err := json.Unmarshal([]byte(output), &pending); err != nil {
		t.Fatalf("decode pending approval: %v\n%s", err, output)
	}
	if pending.Status != "pending_approval" || pending.ApprovalID == "" {
		t.Fatalf("unexpected pending approval output: %s", output)
	}
	record := service.platformApprovals.get(pending.ApprovalID)
	if record == nil {
		t.Fatalf("approval %q was not stored", pending.ApprovalID)
	}
	return record
}

func TestAppendPlatformToolRuntimeDisabled(t *testing.T) {
	svc := newTestServiceWithPlatformSettings(map[string]string{
		platformToolsKeyEnabled: "false",
	})
	var result selectedToolRuntime
	if err := svc.appendPlatformToolRuntime(context.Background(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 平台工具整体关闭时，仅凭据管理工具（系统级能力）仍注入。
	if len(result.platformEntries) == 0 {
		t.Fatalf("expected credential tools when platform tools disabled")
	}
	for name := range result.platformEntries {
		if !isCredentialPlatformTool(name) {
			t.Fatalf("unexpected non-credential tool injected when disabled: %s", name)
		}
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
	credentialWriteCount := 0
	for name, entry := range result.platformEntries {
		if name == "" {
			t.Fatalf("empty model name in platform entries")
		}
		if entry.kind == platformToolWrite {
			writeCount++
			if isCredentialPlatformTool(name) {
				credentialWriteCount++
			}
		} else {
			readCount++
		}
	}
	// 凭据写工具（系统级）不受 write_enabled 控制；其余写工具必须被过滤。
	if writeCount != credentialWriteCount {
		t.Fatalf("non-credential write tools must not be injected when write_enabled=false, got %d", writeCount)
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

func TestAppendPlatformToolRuntimeImageGen(t *testing.T) {
	// 未开启 image_gen：不注入。
	svc := newTestServiceWithPlatformSettings(map[string]string{
		platformToolsKeyEnabled:      "true",
		platformToolsKeyWriteEnabled: "true",
		"image_gen_enabled":          "false",
		"image_gen_channels":         `[{"model":"dashscope-image","note":"4K"}]`,
	})
	var result selectedToolRuntime
	if err := svc.appendPlatformToolRuntime(context.Background(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.platformEntries["image_gen"]; ok {
		t.Fatalf("image_gen must not be injected when image_gen_enabled=false")
	}

	// 开启但渠道为空：不注入。
	svc = newTestServiceWithPlatformSettings(map[string]string{
		platformToolsKeyEnabled:      "true",
		platformToolsKeyWriteEnabled: "true",
		"image_gen_enabled":          "true",
		"image_gen_channels":         "[]",
	})
	result = selectedToolRuntime{}
	if err := svc.appendPlatformToolRuntime(context.Background(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result.platformEntries["image_gen"]; ok {
		t.Fatalf("image_gen must not be injected with empty channels")
	}

	// 开启且有渠道：注入，且描述渐进披露渠道名与备注。
	svc = newTestServiceWithPlatformSettings(map[string]string{
		platformToolsKeyEnabled:      "true",
		platformToolsKeyWriteEnabled: "true",
		"image_gen_enabled":          "true",
		"image_gen_channels":         `[{"model":"dashscope-image","note":"4K"},{"model":"qwen-omni"}]`,
	})
	result = selectedToolRuntime{}
	if err := svc.appendPlatformToolRuntime(context.Background(), &result); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry, ok := result.platformEntries["image_gen"]
	if !ok {
		t.Fatalf("image_gen must be injected when enabled with channels")
	}
	if entry.kind != platformToolWrite {
		t.Fatalf("image_gen must be a write tool")
	}
	if !strings.Contains(entry.definition.Description, "dashscope-image") ||
		!strings.Contains(entry.definition.Description, "4K") ||
		!strings.Contains(entry.definition.Description, "qwen-omni") {
		t.Fatalf("image_gen description must disclose channels with notes, got: %s", entry.definition.Description)
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

func TestCredentialPlatformWritesRespectAskWithoutLeakingPlaintext(t *testing.T) {
	const secret = "ask-mode-credential-secret"
	tests := []struct {
		name      string
		tool      string
		arguments string
		hasSecret bool
	}{
		{name: "create", tool: "credential_create", arguments: `{"name":"deploy-key","type":"api_key","value":"` + secret + `"}`, hasSecret: true},
		{name: "update", tool: "credential_update", arguments: `{"name":"deploy-key","description":"rotated","value":"` + secret + `"}`, hasSecret: true},
		{name: "delete", tool: "credential_delete", arguments: `{"name":"deploy-key"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &recordingCredentialResolver{}
			service := newAskCredentialService(t, resolver)
			runtime := selectedToolRuntime{}
			runtime.bindCredentialSecretRefs(7, 11, "run-ask-credential")

			output, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()[tt.tool], ExecuteToolInput{
				UserID:         7,
				ConversationID: 11,
				RequestID:      "req-ask-credential",
				RunID:          "run-ask-credential",
				ToolName:       tt.tool,
				ArgumentsJSON:  tt.arguments,
				ToolRuntime:    &runtime,
			})
			if err != nil {
				t.Fatalf("submit credential approval: %v", err)
			}
			if resolver.totalWrites() != 0 {
				t.Fatalf("ask mode executed %s before approval", tt.tool)
			}

			record := approvalRecordFromOutput(t, service, output)
			summary, err := marshalApprovalSummary(record)
			if err != nil {
				t.Fatalf("marshal approval summary: %v", err)
			}
			serialized := output + record.ArgumentsJSON + summary
			if strings.Contains(serialized, secret) {
				t.Fatalf("approval surfaces leaked plaintext: %s", serialized)
			}
			if tt.hasSecret {
				if !strings.Contains(record.ArgumentsJSON, "{{secret_ref:") {
					t.Fatalf("approval record did not replace plaintext with a secret ref: %s", record.ArgumentsJSON)
				}
				if !strings.Contains(summary, "[REDACTED]") {
					t.Fatalf("approval summary did not redact credential value: %s", summary)
				}
			}
		})
	}
}

func TestCredentialApprovalOwnerExecutesExactlyOnceAndDestroysSecret(t *testing.T) {
	const secret = "approval-owner-secret"
	resolver := &recordingCredentialResolver{}
	audit := &recordingPlatformAuditWriter{}
	service := newAskCredentialService(t, resolver)
	service.auditWriter = audit
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-owner-approval")
	originalRef := runtime.credentialSecrets.protect(secret)

	output, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()["credential_create"], ExecuteToolInput{
		UserID:         7,
		ConversationID: 11,
		RequestID:      "req-owner-approval",
		RunID:          "run-owner-approval",
		ToolName:       "credential_create",
		ArgumentsJSON:  `{"name":"deploy-key","type":"api_key","value":"` + originalRef + `"}`,
		ToolRuntime:    &runtime,
	})
	if err != nil {
		t.Fatalf("submit credential approval: %v", err)
	}
	record := approvalRecordFromOutput(t, service, output)
	if record.credentialSecrets == nil || len(record.credentialSecretRefs) != 1 {
		t.Fatalf("approval did not retain an execution-only secret binding: %+v", record)
	}
	approvalRef := record.credentialSecretRefs[0]
	if value, ok := record.credentialSecrets.resolve(approvalRef); !ok || value != secret {
		t.Fatalf("approval secret was unavailable before owner approval: ok=%v value=%q", ok, value)
	}

	if _, err := service.ApprovePlatformWrite(t.Context(), record.ID, 8, true); !errors.Is(err, ErrPlatformApprovalNotFound) {
		t.Fatalf("cross-user approval error = %v, want ErrPlatformApprovalNotFound", err)
	}
	if resolver.totalWrites() != 0 {
		t.Fatal("cross-user approval executed the credential handler")
	}
	if _, ok := record.credentialSecrets.resolve(approvalRef); !ok {
		t.Fatal("cross-user approval destroyed the owner's pending secret")
	}

	summary, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, true)
	if err != nil {
		t.Fatalf("owner approve credential write: %v", err)
	}
	if resolver.createCalls != 1 || resolver.lastUserID != 7 || resolver.lastInput.Value != secret {
		t.Fatalf("owner approval did not execute the original secret once: calls=%d user=%d input=%+v", resolver.createCalls, resolver.lastUserID, resolver.lastInput)
	}
	if strings.Contains(summary, secret) || strings.Contains(summary, approvalRef) || !strings.Contains(summary, "[REDACTED]") {
		t.Fatalf("approved summary leaked secret material: %s", summary)
	}
	if _, ok := record.credentialSecrets.resolve(approvalRef); ok {
		t.Fatal("approved credential secret was not destroyed")
	}
	if _, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, true); !errors.Is(err, ErrPlatformApprovalNotFound) {
		t.Fatalf("duplicate approval error = %v, want ErrPlatformApprovalNotFound", err)
	}
	if resolver.createCalls != 1 {
		t.Fatalf("duplicate approval executed handler again: %d", resolver.createCalls)
	}
	for _, payload := range audit.payloads {
		if strings.Contains(payload, secret) || strings.Contains(payload, approvalRef) {
			t.Fatalf("audit payload leaked secret material: %s", payload)
		}
	}
}

func TestCredentialApprovalConcurrentClaimsExecuteExactlyOnce(t *testing.T) {
	resolver := &recordingCredentialResolver{}
	service := newAskCredentialService(t, resolver)
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-concurrent-approval")
	output, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()["credential_create"], ExecuteToolInput{
		UserID: 7, ConversationID: 11, RequestID: "req-concurrent-approval", RunID: "run-concurrent-approval",
		ToolName: "credential_create", ArgumentsJSON: `{"name":"deploy-key","value":"concurrent-secret"}`, ToolRuntime: &runtime,
	})
	if err != nil {
		t.Fatalf("submit concurrent approval: %v", err)
	}
	record := approvalRecordFromOutput(t, service, output)

	const claims = 24
	var wg sync.WaitGroup
	wg.Add(claims)
	errs := make(chan error, claims)
	for range claims {
		go func() {
			defer wg.Done()
			_, approveErr := service.ApprovePlatformWrite(t.Context(), record.ID, 7, true)
			errs <- approveErr
		}()
	}
	wg.Wait()
	close(errs)

	successes := 0
	for approveErr := range errs {
		if approveErr == nil {
			successes++
			continue
		}
		if !errors.Is(approveErr, ErrPlatformApprovalNotFound) {
			t.Fatalf("unexpected concurrent claim error: %v", approveErr)
		}
	}
	if successes != 1 || resolver.totalWrites() != 1 {
		t.Fatalf("concurrent claims executed more than once: successes=%d writes=%d", successes, resolver.totalWrites())
	}
}

func TestPlatformWriteApprovalConcurrentApproveRejectHasSingleWinner(t *testing.T) {
	resolver := &recordingCredentialResolver{}
	service := newAskCredentialService(t, resolver)
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-concurrent-terminal")
	output, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()["credential_create"], ExecuteToolInput{
		UserID: 7, ConversationID: 11, RequestID: "req-concurrent-terminal", RunID: "run-concurrent-terminal",
		ToolName: "credential_create", ArgumentsJSON: `{"name":"deploy-key","value":"concurrent-terminal-secret"}`, ToolRuntime: &runtime,
	})
	if err != nil {
		t.Fatalf("submit concurrent terminal approval: %v", err)
	}
	record := approvalRecordFromOutput(t, service, output)

	const claims = 24
	var wg sync.WaitGroup
	wg.Add(claims)
	errs := make(chan error, claims)
	for i := range claims {
		approve := i%2 == 0
		go func() {
			defer wg.Done()
			_, claimErr := service.ApprovePlatformWrite(t.Context(), record.ID, 7, approve)
			errs <- claimErr
		}()
	}
	wg.Wait()
	close(errs)

	successes := 0
	for claimErr := range errs {
		if claimErr == nil {
			successes++
			continue
		}
		if !errors.Is(claimErr, ErrPlatformApprovalNotFound) {
			t.Fatalf("unexpected concurrent terminal error: %v", claimErr)
		}
	}
	if successes != 1 {
		t.Fatalf("terminal winners = %d, want 1", successes)
	}
	switch record.Status {
	case platformApprovalStatusApproved:
		if resolver.totalWrites() != 1 {
			t.Fatalf("approved winner writes = %d, want 1", resolver.totalWrites())
		}
	case platformApprovalStatusRejected:
		if resolver.totalWrites() != 0 {
			t.Fatalf("rejected winner writes = %d, want 0", resolver.totalWrites())
		}
	default:
		t.Fatalf("unexpected terminal status %q", record.Status)
	}
}

func TestPlatformWriteApprovalHonorsCurrentAdminWriteSwitch(t *testing.T) {
	resolver := &recordingCredentialResolver{}
	service := newAskCredentialService(t, resolver)
	runtime := selectedToolRuntime{}
	runtime.bindCredentialSecretRefs(7, 11, "run-admin-switch")
	output, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()["credential_create"], ExecuteToolInput{
		UserID: 7, ConversationID: 11, RequestID: "req-admin-switch", RunID: "run-admin-switch",
		ToolName: "credential_create", ArgumentsJSON: `{"name":"deploy-key","value":"switch-secret"}`, ToolRuntime: &runtime,
	})
	if err != nil {
		t.Fatalf("submit approval: %v", err)
	}
	record := approvalRecordFromOutput(t, service, output)
	settings := service.platformToolsSettings.(*fakePlatformSettingsReader)
	settings.values[platformToolsKeyWriteEnabled] = "false"

	if _, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, true); !errors.Is(err, ErrPlatformWriteDisabled) {
		t.Fatalf("approve while disabled error = %v, want ErrPlatformWriteDisabled", err)
	}
	if resolver.totalWrites() != 0 || record.Status != platformApprovalStatusPending {
		t.Fatalf("disabled approval was claimed or executed: writes=%d status=%s", resolver.totalWrites(), record.Status)
	}

	settings.values[platformToolsKeyWriteEnabled] = "true"
	if _, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, true); err != nil {
		t.Fatalf("approve after re-enable: %v", err)
	}
	if resolver.totalWrites() != 1 || record.Status != platformApprovalStatusApproved {
		t.Fatalf("re-enabled approval result: writes=%d status=%s", resolver.totalWrites(), record.Status)
	}
}

func TestCredentialApprovalRejectExpireAndFailureDestroySecrets(t *testing.T) {
	t.Run("reject", func(t *testing.T) {
		resolver := &recordingCredentialResolver{}
		service := newAskCredentialService(t, resolver)
		runtime := selectedToolRuntime{}
		runtime.bindCredentialSecretRefs(7, 11, "run-reject")
		output, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()["credential_update"], ExecuteToolInput{
			UserID: 7, ConversationID: 11, RequestID: "req-reject", RunID: "run-reject",
			ToolName: "credential_update", ArgumentsJSON: `{"name":"deploy-key","value":"reject-secret"}`, ToolRuntime: &runtime,
		})
		if err != nil {
			t.Fatalf("submit rejection case: %v", err)
		}
		record := approvalRecordFromOutput(t, service, output)
		ref := record.credentialSecretRefs[0]
		summary, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, false)
		if err != nil {
			t.Fatalf("reject credential approval: %v", err)
		}
		if resolver.totalWrites() != 0 || record.Status != platformApprovalStatusRejected {
			t.Fatalf("rejection executed handler or lost status: writes=%d status=%s", resolver.totalWrites(), record.Status)
		}
		if strings.Contains(summary, "reject-secret") || !strings.Contains(summary, "[REDACTED]") {
			t.Fatalf("rejection summary leaked secret: %s", summary)
		}
		if _, ok := record.credentialSecrets.resolve(ref); ok {
			t.Fatal("rejected credential secret was not destroyed")
		}
	})

	t.Run("expire", func(t *testing.T) {
		resolver := &recordingCredentialResolver{}
		service := newAskCredentialService(t, resolver)
		base := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
		service.platformApprovals.nowFn = func() time.Time { return base }
		service.platformApprovals.ttl = time.Minute
		runtime := selectedToolRuntime{}
		runtime.bindCredentialSecretRefs(7, 11, "run-expire")
		output, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()["credential_create"], ExecuteToolInput{
			UserID: 7, ConversationID: 11, RequestID: "req-expire", RunID: "run-expire",
			ToolName: "credential_create", ArgumentsJSON: `{"name":"deploy-key","value":"expire-secret"}`, ToolRuntime: &runtime,
		})
		if err != nil {
			t.Fatalf("submit expiry case: %v", err)
		}
		record := approvalRecordFromOutput(t, service, output)
		ref := record.credentialSecretRefs[0]
		service.platformApprovals.cleanup(base.Add(2 * time.Minute))
		if record.Status != platformApprovalStatusExpired || service.platformApprovals.get(record.ID) != nil {
			t.Fatalf("expired approval was not removed: %+v", record)
		}
		if _, ok := record.credentialSecrets.resolve(ref); ok {
			t.Fatal("expired credential secret was not destroyed")
		}
	})

	t.Run("execution failure", func(t *testing.T) {
		resolver := &recordingCredentialResolver{writeErr: errors.New("credential backend unavailable")}
		service := newAskCredentialService(t, resolver)
		settingsRepo := service.repo.(*mutableUserSettingsRepository)
		capture := &approvalPersistenceRepository{mutableUserSettingsRepository: settingsRepo}
		service.repo = capture
		runtime := selectedToolRuntime{}
		runtime.bindCredentialSecretRefs(7, 11, "run-failure")
		output, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()["credential_create"], ExecuteToolInput{
			UserID:         7,
			ConversationID: 11,
			MessageID:      32,
			RequestID:      "req-failure",
			RunID:          "run-failure",
			ToolCallID:     "call-failure",
			ToolName:       "credential_create",
			ArgumentsJSON:  `{"name":"deploy-key","value":"failure-secret"}`,
			ToolRuntime:    &runtime,
		})
		if err != nil {
			t.Fatalf("submit failure case: %v", err)
		}
		record := approvalRecordFromOutput(t, service, output)
		ref := record.credentialSecretRefs[0]
		capture.rows = []model.ToolCall{{
			ID:             92,
			MessageID:      32,
			UserID:         7,
			ConversationID: 11,
			RunID:          "run-failure",
			ToolCallID:     "call-failure",
			ToolName:       "credential_create",
			Status:         "success",
			OutputJSON:     output,
		}}
		if _, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, true); err == nil {
			t.Fatal("expected credential backend failure")
		}
		if resolver.createCalls != 1 {
			t.Fatalf("approval failure did not execute exactly once: %d", resolver.createCalls)
		}
		if record.Status != platformApprovalStatusFailed {
			t.Fatalf("approval failure status = %s, want %s", record.Status, platformApprovalStatusFailed)
		}
		row, updates := capture.snapshot()
		if updates != 1 || !strings.Contains(row.OutputJSON, `"status":"failed"`) {
			t.Fatalf("failed terminal persistence: updates=%d output=%s", updates, row.OutputJSON)
		}
		summary, err := service.GetPlatformWriteApproval(record.ID, 7)
		if err != nil || !strings.Contains(summary, `"status":"failed"`) {
			t.Fatalf("failed approval summary = %q, %v", summary, err)
		}
		if _, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, true); !errors.Is(err, ErrPlatformApprovalNotFound) {
			t.Fatalf("failed approval retry error = %v, want ErrPlatformApprovalNotFound", err)
		}
		if resolver.createCalls != 1 {
			t.Fatalf("failed approval executed more than once: %d", resolver.createCalls)
		}
		if _, ok := record.credentialSecrets.resolve(ref); ok {
			t.Fatal("failed approved credential secret was not destroyed")
		}
	})
}

func TestPlatformWriteApprovalStoreLifecycle(t *testing.T) {
	store := newPlatformWriteApprovalStore()
	defer store.Stop()
	entry := platformToolRegistry()["write_file"]
	record := store.create(1, 2, "req-1", entry, "write_file__platform", `{"file_id":"f1","content":"{{credential: deploy-key}}"}`)
	if record == nil || record.ID == "" || record.Status != platformApprovalStatusPending {
		t.Fatalf("unexpected record: %+v", record)
	}
	if record.ToolName != "write_file" || record.ExecutionToolName != "write_file__platform" {
		t.Fatalf("approval must retain model-facing and canonical names: %+v", record)
	}
	if record.ArgumentsJSON != `{"file_id":"f1","content":"{{credential: deploy-key}}"}` {
		t.Fatalf("approval arguments must remain unexpanded: %s", record.ArgumentsJSON)
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

func TestPlatformWriteApprovalStatusIsOwnerScopedAndRestartInvalidatesPending(t *testing.T) {
	store := newPlatformWriteApprovalStore()
	entry := platformToolRegistry()["write_file"]
	record := store.create(7, 11, "req-restart", entry, "write_file", `{"file_id":"f1","content":"hello"}`)
	service := &Service{platformApprovals: store}

	summary, err := service.GetPlatformWriteApproval(record.ID, 7)
	if err != nil || !strings.Contains(summary, `"status":"pending"`) {
		t.Fatalf("owner status = %q, %v", summary, err)
	}
	if _, err := service.GetPlatformWriteApproval(record.ID, 8); !errors.Is(err, ErrPlatformApprovalNotFound) {
		t.Fatalf("cross-user status error = %v, want ErrPlatformApprovalNotFound", err)
	}

	store.Stop()
	restarted := &Service{platformApprovals: newPlatformWriteApprovalStore()}
	defer restarted.platformApprovals.Stop()
	if _, err := restarted.GetPlatformWriteApproval(record.ID, 7); !errors.Is(err, ErrPlatformApprovalNotFound) {
		t.Fatalf("restart status error = %v, want ErrPlatformApprovalNotFound", err)
	}
}

func TestPlatformWriteApprovalStatusExpiresAndDestroysCredentialSecret(t *testing.T) {
	store := newPlatformWriteApprovalStore()
	defer store.Stop()
	base := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	store.nowFn = func() time.Time { return base }
	store.ttl = time.Minute
	secrets := newCredentialSecretRefStore(7, 11, "run-expiry-status")
	ref := secrets.protect("approval-expiry-secret")
	record := store.createProtected(
		7,
		11,
		0,
		"req-expiry-status",
		"run-expiry-status",
		"",
		platformToolRegistry()["credential_create"],
		"credential_create",
		`{"name":"deploy-key","value":"`+ref+`"}`,
		secrets,
		[]string{ref},
	)
	store.nowFn = func() time.Time { return base.Add(2 * time.Minute) }

	summary, err := (&Service{platformApprovals: store}).GetPlatformWriteApproval(record.ID, 7)
	if err != nil || !strings.Contains(summary, `"status":"expired"`) {
		t.Fatalf("expired status = %q, %v", summary, err)
	}
	if _, ok := secrets.resolve(ref); ok {
		t.Fatal("status lookup did not destroy the expired credential secret")
	}
	if store.get(record.ID) != nil {
		t.Fatal("expired approval remained in the store")
	}
}

func TestPlatformWriteApprovalPersistsTerminalToolOutput(t *testing.T) {
	for _, tt := range []struct {
		name    string
		approve bool
		status  string
	}{
		{name: "approved", approve: true, status: platformApprovalStatusApproved},
		{name: "rejected", approve: false, status: platformApprovalStatusRejected},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &recordingCredentialResolver{}
			service := newAskCredentialService(t, resolver)
			settingsRepo := service.repo.(*mutableUserSettingsRepository)
			capture := &approvalPersistenceRepository{mutableUserSettingsRepository: settingsRepo}
			service.repo = capture
			runtime := selectedToolRuntime{}
			runtime.bindCredentialSecretRefs(7, 11, "run-terminal-persist")

			pendingOutput, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()["credential_create"], ExecuteToolInput{
				UserID:         7,
				ConversationID: 11,
				MessageID:      31,
				RequestID:      "req-terminal-persist",
				RunID:          "run-terminal-persist",
				ToolCallID:     "call-terminal-persist",
				ToolName:       "credential_create",
				ArgumentsJSON:  `{"name":"deploy-key","value":"terminal-secret"}`,
				ToolRuntime:    &runtime,
			})
			if err != nil {
				t.Fatalf("submit approval: %v", err)
			}
			record := approvalRecordFromOutput(t, service, pendingOutput)
			capture.rows = []model.ToolCall{{
				ID:             91,
				MessageID:      31,
				UserID:         7,
				ConversationID: 11,
				RunID:          "run-terminal-persist",
				ToolCallID:     "call-terminal-persist",
				ToolName:       "credential_create",
				Status:         "success",
				OutputJSON:     pendingOutput,
			}}

			if _, err := service.ApprovePlatformWrite(t.Context(), record.ID, 8, tt.approve); !errors.Is(err, ErrPlatformApprovalNotFound) {
				t.Fatalf("cross-user terminal action error = %v", err)
			}
			if _, updates := capture.snapshot(); updates != 0 {
				t.Fatalf("cross-user action persisted %d updates", updates)
			}
			if _, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, tt.approve); err != nil {
				t.Fatalf("owner terminal action: %v", err)
			}
			row, updates := capture.snapshot()
			if updates != 1 {
				t.Fatalf("terminal updates = %d, want 1", updates)
			}
			var terminal struct {
				ApprovalID string `json:"approval_id"`
				Status     string `json:"status"`
			}
			if err := json.Unmarshal([]byte(row.OutputJSON), &terminal); err != nil {
				t.Fatalf("decode terminal output: %v", err)
			}
			if terminal.ApprovalID != record.ID || terminal.Status != tt.status {
				t.Fatalf("terminal output = %+v, want id=%s status=%s", terminal, record.ID, tt.status)
			}
		})
	}
}

func TestApprovedSaveArtifactPersistsSafeResult(t *testing.T) {
	const secret = "artifact-title-secret"
	service := newAskCredentialService(t, &recordingCredentialResolver{})
	service.credentials = &fakeCredentialResolver{values: map[string]string{"approval-key": secret}}
	settingsRepo := service.repo.(*mutableUserSettingsRepository)
	capture := &approvalPersistenceRepository{mutableUserSettingsRepository: settingsRepo}
	service.repo = capture
	artifactRepo := &approvalArtifactRepository{}
	service.artifactSvc = appartifact.NewService(artifactRepo)

	const runID = "run-artifact-terminal"
	const toolCallID = "call-artifact-terminal"
	pendingOutput, err := service.executePlatformToolCall(t.Context(), platformToolRegistry()["save_artifact"], ExecuteToolInput{
		UserID:         7,
		ConversationID: 11,
		MessageID:      32,
		RequestID:      "req-artifact-terminal",
		RunID:          runID,
		ToolCallID:     toolCallID,
		ToolName:       "save_artifact",
		ArgumentsJSON:  `{"title":"{{credential: approval-key}}","kind":"html","code":"<h1>safe</h1>"}`,
		ToolRuntime:    &selectedToolRuntime{},
	})
	if err != nil {
		t.Fatalf("submit artifact approval: %v", err)
	}
	record := approvalRecordFromOutput(t, service, pendingOutput)
	capture.rows = []model.ToolCall{{
		ID:             92,
		MessageID:      32,
		UserID:         7,
		ConversationID: 11,
		RunID:          runID,
		ToolCallID:     toolCallID,
		ToolName:       "save_artifact",
		Status:         "success",
		OutputJSON:     pendingOutput,
	}}

	if _, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, true); err != nil {
		t.Fatalf("approve artifact: %v", err)
	}
	row, updates := capture.snapshot()
	if updates != 1 {
		t.Fatalf("terminal updates = %d, want 1", updates)
	}
	var terminal map[string]interface{}
	if err := json.Unmarshal([]byte(row.OutputJSON), &terminal); err != nil {
		t.Fatalf("decode artifact terminal output: %v", err)
	}
	if terminal["artifact_id"] == "" || terminal["status"] != platformApprovalStatusApproved || terminal["title"] != "{{credential: approval-key}}" || terminal["kind"] != "html" {
		t.Fatalf("unexpected artifact terminal output: %s", row.OutputJSON)
	}
	if strings.Contains(row.OutputJSON, secret) || strings.Contains(row.OutputJSON, "<h1>safe</h1>") {
		t.Fatalf("artifact terminal output exposed sensitive input: %s", row.OutputJSON)
	}
	if artifactRepo.created == nil || artifactRepo.created.Title != secret {
		t.Fatalf("artifact handler did not execute with expanded credential: %+v", artifactRepo.created)
	}
}

func TestApprovePlatformWriteUsesCanonicalNameAndExpandsCredentialsAtExecution(t *testing.T) {
	const secret = "approval-secret-value"
	store := newPlatformWriteApprovalStore()
	defer store.Stop()
	service := &Service{
		credentials: &fakeCredentialResolver{values: map[string]string{"approval-key": secret}},
		platformToolsSettings: &fakePlatformSettingsReader{values: map[string]string{
			platformToolsKeyWriteEnabled: "true",
		}},
		platformApprovals: store,
	}
	entry := platformToolRegistry()["execute_js"]
	arguments := `{"code":"const value = '{{credential: approval-key}}'; if (value.startsWith('{{')) { throw new Error('credential not expanded'); } 'ok'"}`
	record := store.create(7, 9, "req-approval", entry, "execute_js", arguments)
	record.ToolName = "execute_js__platform"

	pendingSummary, err := marshalApprovalSummary(record)
	if err != nil {
		t.Fatalf("marshal pending approval: %v", err)
	}
	if strings.Contains(pendingSummary, secret) || strings.Contains(record.ArgumentsJSON, secret) {
		t.Fatalf("pending approval exposed credential plaintext: summary=%s record=%s", pendingSummary, record.ArgumentsJSON)
	}
	if !strings.Contains(pendingSummary, "{{credential: approval-key}}") {
		t.Fatalf("pending approval lost credential placeholder: %s", pendingSummary)
	}

	approvedSummary, err := service.ApprovePlatformWrite(t.Context(), record.ID, 7, true)
	if err != nil {
		t.Fatalf("approve platform write: %v", err)
	}
	if record.Status != platformApprovalStatusApproved {
		t.Fatalf("approval record was not marked approved: %+v", record)
	}
	if strings.Contains(approvedSummary, secret) || strings.Contains(record.ArgumentsJSON, secret) {
		t.Fatalf("approved summary or record exposed credential plaintext: summary=%s record=%s", approvedSummary, record.ArgumentsJSON)
	}
	if !strings.Contains(approvedSummary, "execute_js__platform") {
		t.Fatalf("approval summary lost model-facing tool name: %s", approvedSummary)
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
