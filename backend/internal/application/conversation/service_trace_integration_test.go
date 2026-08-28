package conversation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	persistencemodels "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	persistenceconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/postgres/conversation"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCanceledTraceSettlementPersistsCompleteReasoningForReload(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:trace_cancel_settlement?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&persistencemodels.ChatRunEvent{}); err != nil {
		t.Fatalf("migrate trace table: %v", err)
	}

	repo := persistenceconversation.NewRepo(db)
	cfg := config.Config{
		ProcessTraceEnabled:            true,
		ProcessTraceVisibleToUser:      true,
		ProcessTraceStoreUpstreamThink: true,
	}
	service := &Service{cfg: config.NewRuntime(cfg), repo: repo}
	assistant := &model.Message{
		ID:             41,
		ConversationID: 17,
		UserID:         9,
		RunID:          "run_cancel_settlement",
		Role:           "assistant",
	}

	generationCtx, cancelGeneration := context.WithCancel(context.Background())
	recorder := newMessageTraceRecorder(service, generationCtx, assistant, nil)
	recorder.appendUpstreamReasoning(messageTraceThinkKindContent, "嗯", nil)
	recorder.appendUpstreamReasoning(messageTraceThinkKindContent, "，继续分析并保留终止前的完整思考", nil)
	cancelGeneration()
	if generationCtx.Err() == nil {
		t.Fatal("expected generation context to be canceled")
	}

	recorder.failWithContext(context.Background(), ErrMessageGenerationCanceled)

	reloaded := []model.Message{{ID: assistant.ID, Role: "assistant"}}
	reloadService := &Service{cfg: config.NewRuntime(cfg), repo: repo}
	if err := reloadService.hydrateMessageProcessTraces(context.Background(), reloaded); err != nil {
		t.Fatalf("hydrate persisted trace: %v", err)
	}
	trace := reloaded[0].ProcessTrace
	if trace == nil || trace.UpstreamThink == nil {
		t.Fatalf("expected persisted upstream reasoning after reload, got %#v", trace)
	}
	if got, want := trace.UpstreamThink.ContentMarkdown, "嗯，继续分析并保留终止前的完整思考"; got != want {
		t.Fatalf("reloaded reasoning = %q, want %q", got, want)
	}
	if trace.UpstreamThink.Status != messageTraceStatusError {
		t.Fatalf("reloaded reasoning status = %q, want %q", trace.UpstreamThink.Status, messageTraceStatusError)
	}
}

func TestPlatformApprovalTerminalStateSurvivesServiceReload(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:trace_approval_reload?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&persistencemodels.ChatRunEvent{}); err != nil {
		t.Fatalf("migrate trace table: %v", err)
	}

	repo := persistenceconversation.NewRepo(db)
	const pendingOutput = `{"status":"pending_approval","approval_id":"approval-reload","tool":"save_memory"}`
	_, _, tracePayload := buildToolTrace([]model.ToolCall{{
		ToolCallID: "call-reload",
		ToolName:   "save_memory",
		Status:     "success",
		OutputJSON: pendingOutput,
	}})
	payloadJSON, err := json.Marshal(tracePayload)
	if err != nil {
		t.Fatalf("marshal trace payload: %v", err)
	}
	if err := repo.UpsertConversationMessageTrace(context.Background(), &model.MessageTrace{
		MessageID:       71,
		ConversationID:  37,
		UserID:          19,
		RunID:           "run-approval-reload",
		TraceType:       messageTraceTypeTools,
		Status:          messageTraceStatusCompleted,
		ContentMarkdown: "pending",
		PayloadJSON:     string(payloadJSON),
	}); err != nil {
		t.Fatalf("persist trace: %v", err)
	}
	toolRow := model.ToolCall{
		MessageID:      71,
		ConversationID: 37,
		UserID:         19,
		RunID:          "run-approval-reload",
		ToolCallID:     "call-reload",
		ToolName:       "save_memory",
		Status:         "success",
		OutputJSON:     `{"approval_id":"approval-reload","status":"approved","tool":"save_memory"}`,
	}
	if err := repo.CreateConversationToolCall(context.Background(), &toolRow); err != nil {
		t.Fatalf("persist tool call: %v", err)
	}

	cfg := config.Config{ProcessTraceEnabled: true, ProcessTraceVisibleToUser: true}
	reloaded := []model.Message{{ID: 71, Role: "assistant"}}
	if err := (&Service{cfg: config.NewRuntime(cfg), repo: repo}).hydrateMessageProcessTraces(context.Background(), reloaded); err != nil {
		t.Fatalf("hydrate persisted trace: %v", err)
	}
	if reloaded[0].ProcessTrace == nil || reloaded[0].ProcessTrace.Tools == nil {
		t.Fatalf("missing reloaded tools trace: %#v", reloaded[0].ProcessTrace)
	}
	var hydrated struct {
		ToolCalls []struct {
			OutputDetail string `json:"output_detail"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(reloaded[0].ProcessTrace.Tools.PayloadJSON), &hydrated); err != nil {
		t.Fatalf("decode hydrated tools trace: %v", err)
	}
	if len(hydrated.ToolCalls) != 1 || !strings.Contains(hydrated.ToolCalls[0].OutputDetail, `"status":"approved"`) ||
		strings.Contains(hydrated.ToolCalls[0].OutputDetail, "pending_approval") {
		t.Fatalf("terminal approval was not reconciled after reload: %+v", hydrated.ToolCalls)
	}
}

func TestScrubCredentialAttemptsRewritesPersistedTraceEvents(t *testing.T) {
	const (
		secret = "trace-secret-value"
		ref    = "{{secret_ref:11111111-1111-1111-1111-111111111111}}"
	)
	db, err := gorm.Open(sqlite.Open("file:trace_credential_scrub?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&persistencemodels.ChatRunEvent{}); err != nil {
		t.Fatalf("migrate trace table: %v", err)
	}

	repo := persistenceconversation.NewRepo(db)
	cfg := config.Config{
		ProcessTraceEnabled:         true,
		ProcessTraceVisibleToUser:   true,
		ProcessTracePersistInflight: true,
	}
	service := &Service{cfg: config.NewRuntime(cfg), repo: repo}
	assistant := &model.Message{
		ID:             51,
		ConversationID: 27,
		UserID:         13,
		RunID:          "run_credential_scrub",
		Role:           "assistant",
	}
	recorder := newMessageTraceRecorder(service, context.Background(), assistant, nil)
	recorder.appendToolSection(
		"credential attempt "+secret,
		"credential attempt "+ref,
		map[string]interface{}{"secret": secret, "ref": ref},
		messageTraceStatusCompleted,
	)
	recorder.scrubCredentialAttempts(context.Background(), []credentialWrite{{
		Name:  "deploy-key",
		Value: secret,
		Ref:   ref,
	}}, nil)

	rows, err := repo.ListConversationMessageTraceEventsByMessageIDs(context.Background(), []uint{assistant.ID})
	if err != nil {
		t.Fatalf("reload trace events: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected persisted trace events")
	}
	for _, row := range rows {
		serialized := row.Title + row.Summary + row.ContentMarkdown + row.PayloadJSON
		if strings.Contains(serialized, secret) || strings.Contains(serialized, ref) {
			t.Fatalf("persisted trace event retained credential material: %s", serialized)
		}
	}
}
