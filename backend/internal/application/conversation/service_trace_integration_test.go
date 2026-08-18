package conversation

import (
	"context"
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
