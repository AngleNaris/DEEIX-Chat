package conversation

import (
	"errors"
	"strings"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

func TestFlushCredentialBufferedStreamEventsRedactsSpanningDeltas(t *testing.T) {
	const secret = "stream-secret-value"
	events := []llm.GenerateStreamEvent{
		{Delta: "before stream-", Reasoning: &llm.ReasoningDelta{Text: "reason stream-"}},
		{Delta: "secret-value after", Reasoning: &llm.ReasoningDelta{Text: "secret-value done"}},
	}
	var handled []llm.GenerateStreamEvent
	err := flushCredentialBufferedStreamEvents(events, []credentialWrite{{Name: "key", Value: secret}}, func(event llm.GenerateStreamEvent) error {
		handled = append(handled, event)
		return nil
	})
	if err != nil {
		t.Fatalf("flush buffered events: %v", err)
	}
	if len(handled) != 1 {
		t.Fatalf("handled event count = %d, want 1", len(handled))
	}
	if handled[0].Delta != "before [REDACTED] after" {
		t.Fatalf("unexpected delta: %q", handled[0].Delta)
	}
	if handled[0].Reasoning == nil || handled[0].Reasoning.Text != "reason [REDACTED] done" {
		t.Fatalf("unexpected reasoning: %#v", handled[0].Reasoning)
	}
}

func TestCredentialStreamBufferSeparatesImmediateMetadata(t *testing.T) {
	buffer := credentialStreamBuffer{}
	original := llm.GenerateStreamEvent{
		Delta:      "protected text",
		Usage:      llm.Usage{InputTokens: 11, OutputTokens: 3},
		ResponseID: "response-1",
		GeneratedImage: &llm.GeneratedImage{
			B64JSON:       strings.Repeat("a", 2*1024*1024),
			MIMEType:      "image/png",
			RevisedPrompt: "protected prompt",
		},
	}
	immediate, hadSideEffect, err := buffer.add(original)
	if err != nil {
		t.Fatalf("buffer event: %v", err)
	}
	if !hadSideEffect || immediate.Usage != original.Usage || immediate.ResponseID != original.ResponseID {
		t.Fatalf("immediate metadata not preserved: %#v", immediate)
	}
	if immediate.GeneratedImage != nil {
		t.Fatalf("image partial must not bypass credential guard: %#v", immediate.GeneratedImage)
	}
	if len(buffer.events) != 1 || buffer.events[0].Delta != original.Delta {
		t.Fatalf("protected delta not buffered: %#v", buffer.events)
	}
	if buffer.events[0].Usage != (llm.Usage{}) || buffer.events[0].ResponseID != "" || buffer.events[0].GeneratedImage != nil {
		t.Fatalf("immediate metadata leaked into buffer: %#v", buffer.events[0])
	}
	if buffer.bytes != len(original.Delta) {
		t.Fatalf("buffer bytes = %d, want %d", buffer.bytes, len(original.Delta))
	}
}

func TestCredentialStreamBufferRejectsOversizedProtectedContent(t *testing.T) {
	buffer := credentialStreamBuffer{}
	_, hadSideEffect, err := buffer.add(llm.GenerateStreamEvent{Delta: strings.Repeat("x", maxCredentialStreamBufferBytes+1)})
	if !errors.Is(err, errCredentialStreamBufferLimitExceeded) {
		t.Fatalf("expected buffer limit error, got %v", err)
	}
	if !hadSideEffect {
		t.Fatal("oversized upstream output must disable automatic retry")
	}
	if len(buffer.events) != 0 || buffer.bytes != 0 {
		t.Fatalf("oversized event was retained: events=%d bytes=%d", len(buffer.events), buffer.bytes)
	}
}

func TestCloneGenerateStreamEventDeepCopiesPointerFields(t *testing.T) {
	original := llm.GenerateStreamEvent{
		Reasoning:      &llm.ReasoningDelta{Text: "reason"},
		ServerToolCall: &llm.ToolCall{ArgumentsJSON: `{"value":"secret"}`},
		GeneratedImage: &llm.GeneratedImage{RevisedPrompt: "prompt"},
	}
	cloned := cloneGenerateStreamEvent(original)
	cloned.Reasoning.Text = "changed"
	cloned.ServerToolCall.ArgumentsJSON = "{}"
	cloned.GeneratedImage.RevisedPrompt = "changed"
	if original.Reasoning.Text != "reason" || original.ServerToolCall.ArgumentsJSON != `{"value":"secret"}` || original.GeneratedImage.RevisedPrompt != "prompt" {
		t.Fatalf("clone mutated original: %#v", original)
	}
}
