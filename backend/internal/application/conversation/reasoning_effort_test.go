package conversation

import (
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

func TestReasoningEffortParamForProtocol(t *testing.T) {
	t.Run("scalar protocols pass through levels", func(t *testing.T) {
		path, value, ok := ReasoningEffortParamForProtocol(llm.AdapterOpenAIChatCompletions, ReasoningEffortHigh)
		if !ok || path != "reasoning_effort" || value != "high" {
			t.Fatalf("unexpected scalar mapping: path=%q value=%v ok=%v", path, value, ok)
		}
		path, value, ok = ReasoningEffortParamForProtocol(llm.AdapterOpenAIChatCompletions, ReasoningEffortMax)
		if !ok || path != "reasoning_effort" || value != "max" {
			t.Fatalf("unexpected max mapping: path=%q value=%v ok=%v", path, value, ok)
		}
	})

	t.Run("responses pass through xhigh and truncate max", func(t *testing.T) {
		path, value, ok := ReasoningEffortParamForProtocol(llm.AdapterOpenAIResponses, ReasoningEffortXHigh)
		if !ok || path != "reasoning.effort" || value != "xhigh" {
			t.Fatalf("unexpected xhigh mapping: path=%q value=%v ok=%v", path, value, ok)
		}
		path, value, ok = ReasoningEffortParamForProtocol(llm.AdapterOpenAIResponses, ReasoningEffortMax)
		if !ok || path != "reasoning.effort" || value != "xhigh" {
			t.Fatalf("expected max truncated to xhigh, got path=%q value=%v ok=%v", path, value, ok)
		}
	})

	t.Run("anthropic maps to thinking budget", func(t *testing.T) {
		wantBudgets := map[string]int{
			ReasoningEffortLow:    2048,
			ReasoningEffortMedium: 4096,
			ReasoningEffortHigh:   8192,
			ReasoningEffortXHigh:  16384,
			ReasoningEffortMax:    32000,
		}
		for level, wantBudget := range wantBudgets {
			path, value, ok := ReasoningEffortParamForProtocol(llm.AdapterAnthropicMessages, level)
			if !ok || path != "thinking" {
				t.Fatalf("unexpected anthropic mapping: path=%q ok=%v", path, ok)
			}
			thinking, isMap := value.(map[string]interface{})
			if !isMap || thinking["type"] != "enabled" || thinking["budget_tokens"] != wantBudget {
				t.Fatalf("unexpected anthropic value for %s: %v", level, value)
			}
		}
	})

	t.Run("unsupported protocol or level rejected", func(t *testing.T) {
		if _, _, ok := ReasoningEffortParamForProtocol(llm.AdapterGoogleGenerateContent, ReasoningEffortHigh); ok {
			t.Fatal("expected unsupported protocol to be rejected")
		}
		if _, _, ok := ReasoningEffortParamForProtocol(llm.AdapterAnthropicMessages, "ultra"); ok {
			t.Fatal("expected invalid level to be rejected")
		}
	})
}

func TestApplyReasoningEffortInjection(t *testing.T) {
	t.Run("keeps existing choice when no explicit input level", func(t *testing.T) {
		base := map[string]interface{}{
			"reasoning_effort": "low",
		}
		result := applyReasoningEffortInjection(llm.AdapterOpenAIChatCompletions, "", ReasoningEffortMedium, base)
		if result["reasoning_effort"] != "low" {
			t.Fatalf("expected composer choice preserved, got %v", result["reasoning_effort"])
		}
	})

	t.Run("injects resolved level when option absent", func(t *testing.T) {
		result := applyReasoningEffortInjection(llm.AdapterOpenAIChatCompletions, "", ReasoningEffortMedium, map[string]interface{}{})
		if result["reasoning_effort"] != "medium" {
			t.Fatalf("expected default injected, got %v", result["reasoning_effort"])
		}
	})

	t.Run("explicit input level overrides existing option", func(t *testing.T) {
		base := map[string]interface{}{
			"reasoning_effort": "high",
		}
		result := applyReasoningEffortInjection(llm.AdapterOpenAIChatCompletions, ReasoningEffortLow, ReasoningEffortLow, base)
		if result["reasoning_effort"] != "low" {
			t.Fatalf("expected explicit level to override, got %v", result["reasoning_effort"])
		}
	})

	t.Run("anthropic injects thinking with default budget", func(t *testing.T) {
		result := applyReasoningEffortInjection(llm.AdapterAnthropicMessages, "", ReasoningEffortHigh, map[string]interface{}{})
		thinking, isMap := result["thinking"].(map[string]interface{})
		if !isMap || thinking["type"] != "enabled" || thinking["budget_tokens"] != 8192 {
			t.Fatalf("unexpected anthropic thinking injection: %v", result["thinking"])
		}
	})

	t.Run("anthropic clamps budget to max_tokens", func(t *testing.T) {
		base := map[string]interface{}{"max_tokens": float64(4096)}
		result := applyReasoningEffortInjection(llm.AdapterAnthropicMessages, "", ReasoningEffortHigh, base)
		thinking := result["thinking"].(map[string]interface{})
		if thinking["budget_tokens"] != 3072 {
			t.Fatalf("expected budget clamped to 3072, got %v", thinking["budget_tokens"])
		}
	})

	t.Run("anthropic prefers max_output_tokens for clamping", func(t *testing.T) {
		base := map[string]interface{}{"max_tokens": float64(4096), "max_output_tokens": float64(2048)}
		result := applyReasoningEffortInjection(llm.AdapterAnthropicMessages, "", ReasoningEffortHigh, base)
		thinking := result["thinking"].(map[string]interface{})
		if thinking["budget_tokens"] != 1024 {
			t.Fatalf("expected budget clamped to 1024, got %v", thinking["budget_tokens"])
		}
	})

	t.Run("anthropic skips injection when max_tokens too small", func(t *testing.T) {
		base := map[string]interface{}{"max_tokens": float64(1500)}
		result := applyReasoningEffortInjection(llm.AdapterAnthropicMessages, "", ReasoningEffortHigh, base)
		if _, exists := result["thinking"]; exists {
			t.Fatalf("expected injection skipped, got %v", result["thinking"])
		}
	})

	t.Run("anthropic keeps existing thinking config without input level", func(t *testing.T) {
		base := map[string]interface{}{
			"thinking": map[string]interface{}{"type": "disabled"},
		}
		result := applyReasoningEffortInjection(llm.AdapterAnthropicMessages, "", ReasoningEffortHigh, base)
		thinking := result["thinking"].(map[string]interface{})
		if thinking["type"] != "disabled" {
			t.Fatalf("expected existing thinking config preserved, got %v", thinking)
		}
	})

	t.Run("unsupported protocol returns base unchanged", func(t *testing.T) {
		base := map[string]interface{}{"temperature": float64(0.7)}
		result := applyReasoningEffortInjection(llm.AdapterGoogleGenerateContent, "", ReasoningEffortHigh, base)
		if _, exists := result["thinking"]; exists {
			t.Fatal("expected no injection for unsupported protocol")
		}
	})
}
