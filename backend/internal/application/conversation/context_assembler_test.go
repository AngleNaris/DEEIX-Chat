package conversation

import (
	"strings"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

func TestContextAssemblerKeepsRequiredPreferenceWhenOverBudget(t *testing.T) {
	assembler := NewContextAssembler(1)
	assembler.Add(ContextSlot{
		Kind:     SlotPreference,
		Content:  "# prefs\n- language: Chinese",
		Required: true,
	})

	messages, trace := assembler.Assemble([]llm.Message{{Role: "user", Content: strings.Repeat("history ", 20)}})
	if len(messages) != 2 || messages[0].Role != "system" || messages[0].Content != "# prefs\n- language: Chinese" {
		t.Fatalf("required preference was not injected: %+v", messages)
	}
	for _, trimmed := range trace.TrimmedSlots {
		if trimmed.Kind == SlotPreference {
			t.Fatalf("required preference was trimmed: %+v", trace)
		}
	}
}
