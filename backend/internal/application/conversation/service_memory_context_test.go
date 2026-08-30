package conversation

import (
	"context"
	"strings"
	"testing"

	appembedding "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/embedding"
	domainmemory "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/memory"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
)

func TestMemoryCategoryRecallPolicy(t *testing.T) {
	memories := []domainmemory.UserMemory{
		{MemoryKey: "pref", Scope: "preference", Value: strings.Repeat("concise ", 500)},
		{MemoryKey: "identity", Scope: "profile", Value: "private identity value"},
		{MemoryKey: "activity", Scope: "activity", Value: "private activity value"},
		{MemoryKey: "context", Scope: "custom", Value: "private project value"},
		{MemoryKey: "capability", Scope: "capability", Value: "experienced with music production"},
		{MemoryKey: "experience", Scope: "experience", Value: "released an album"},
	}

	preference := buildPreferencePrompt(filterMemoriesByScope(memories, domainmemory.CategoryPreference), 400)
	if preference == "" || estimateTokens(preference) > 400 {
		t.Fatalf("preference must be injected within 400 tokens, got %d", estimateTokens(preference))
	}

	prompt := buildMemorySystemPrompt(memories, 400)
	for _, secret := range []string{"private identity value", "private activity value", "private project value"} {
		if strings.Contains(prompt, secret) {
			t.Fatalf("on-demand memory content leaked into prompt: %q", secret)
		}
	}
	for _, count := range []string{"identity=1", "activity=1", "context=1"} {
		if !strings.Contains(prompt, count) {
			t.Fatalf("missing memory catalog count %q in %q", count, prompt)
		}
	}

	recallable := filterMemoriesByScope(memories, domainmemory.CategoryCapability, domainmemory.CategoryExperience)
	if got := selectRelevantMemories(recallable, "tax invoices", 5); len(got) != 0 {
		t.Fatalf("unrelated memories must not be recalled: %+v", got)
	}
	if got := selectRelevantMemories(recallable, "music production", 5); len(got) != 1 || got[0].MemoryKey != "capability" {
		t.Fatalf("expected relevant capability memory, got %+v", got)
	}
}

func TestRelevantUserMemoriesFallBackWhenEmbeddingFails(t *testing.T) {
	memories := []domainmemory.UserMemory{
		{MemoryKey: "music", Scope: domainmemory.CategoryCapability, Value: "experienced with music production"},
		{MemoryKey: "tax", Scope: domainmemory.CategoryExperience, Value: "experienced with tax invoices"},
	}
	runtimeConfig := config.NewRuntime(config.Config{
		EmbeddingEnabled: true,
		EmbeddingHost:    "http://127.0.0.1:1",
		RAGModel:         "embedding-test",
	})
	service := &Service{
		cfg:            runtimeConfig,
		embeddingSvc:   appembedding.NewServiceWithRuntime(runtimeConfig, nil, nil, nil, nil),
		memoryRecorder: &fakeMemoryRecorder{},
	}

	got := service.selectRelevantUserMemories(context.Background(), 1, "music production", memories, 5)
	if len(got) != 1 || got[0].MemoryKey != "music" {
		t.Fatalf("expected keyword fallback after embedding failure, got %+v", got)
	}
}
