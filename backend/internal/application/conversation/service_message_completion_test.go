package conversation

import (
	"strings"
	"testing"
	"time"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

func TestNormalizeAssistantArtifactContent(t *testing.T) {
	reasoning := strings.Join([]string{
		"先核对布局。",
		"```html",
		"<main>HTML</main>",
		"```",
		"继续处理样式。",
		"~~~scss",
		"main { color: red; }",
		"~~~",
		"```javascript",
		"console.log('ready')",
		"```",
		"```svg",
		"<svg viewBox=\"0 0 1 1\"></svg>",
		"```",
		"完成。",
	}, "\n")

	content, remaining := normalizeAssistantArtifactContent("这是正文。", reasoning)
	for _, expected := range []string{"```html", "~~~scss", "```javascript", "```svg"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("content missing %q: %q", expected, content)
		}
	}
	if remaining != "先核对布局。\n继续处理样式。\n完成。" {
		t.Fatalf("unexpected remaining reasoning: %q", remaining)
	}
}

func TestNormalizeAssistantArtifactContentLeavesUnsupportedAndUnclosedFences(t *testing.T) {
	reasoning := "说明\n```go\nfmt.Println(1)\n```\n```html\n<div>unfinished</div>"
	content, remaining := normalizeAssistantArtifactContent("answer", reasoning)
	if content != "answer" || remaining != reasoning {
		t.Fatalf("unsupported or unclosed fences changed: content=%q reasoning=%q", content, remaining)
	}
}

func TestNormalizeAssistantArtifactContentRemovesExactDuplicate(t *testing.T) {
	artifact := "```html\n<main>same</main>\n```"
	content, reasoning := normalizeAssistantArtifactContent("answer\n\n"+artifact, "thinking\n"+artifact+"\ndone")
	if strings.Count(content, artifact) != 1 {
		t.Fatalf("expected one artifact, got %q", content)
	}
	if reasoning != "thinking\ndone" {
		t.Fatalf("unexpected reasoning: %q", reasoning)
	}
}

func TestNormalizeAssistantArtifactContentDetectsUnlabelledDocuments(t *testing.T) {
	reasoning := "```\n<div>preview</div>\n```\n```xml\n<?xml version=\"1.0\"?><svg></svg>\n```"
	content, remaining := normalizeAssistantArtifactContent("", reasoning)
	if remaining != "" || !strings.Contains(content, "<div>preview</div>") || !strings.Contains(content, "<svg></svg>") {
		t.Fatalf("unexpected normalization: content=%q reasoning=%q", content, remaining)
	}
}

func TestCanceledGenerationWithObservedUsageIsRetainedForBilling(t *testing.T) {
	input := persistInterruptedMessageGenerationInput{
		UserMessage:          &model.Message{},
		AssistantMessage:     &model.Message{},
		EstimatedInputTokens: 12,
		Usage:                llm.Usage{InputTokens: 40, ReasoningTokens: 6},
		AssistantLatency:     25,
		Error:                ErrMessageGenerationCanceled,
		StartedAt:            time.Now(),
	}

	if !shouldPersistInterruptedMessageGeneration(input) {
		t.Fatal("expected canceled generation with observed usage to be retained")
	}
	metrics := resolveInterruptedMessageGenerationMetrics(input)
	if metrics.InputTokens != 40 || metrics.ReasoningTokens != 6 {
		t.Fatalf("expected observed usage to be preserved, got %#v", metrics)
	}
	if status := retainedGenerationStatus(input.Error); status != "canceled" {
		t.Fatalf("expected canceled status, got %q", status)
	}
}

func TestCanceledGenerationUsesEstimatedTotalWhenObservedUsageIsPartial(t *testing.T) {
	input := persistInterruptedMessageGenerationInput{
		UserMessage:          &model.Message{},
		AssistantMessage:     &model.Message{},
		EstimatedInputTokens: 96,
		Usage:                llm.Usage{InputTokens: 40},
		Error:                ErrMessageGenerationCanceled,
		StartedAt:            time.Now(),
	}

	metrics := resolveInterruptedMessageGenerationMetrics(input)
	if metrics.InputTokens != 96 {
		t.Fatalf("expected estimated total input tokens to cover partial observed usage, got %#v", metrics)
	}
}

func TestCanceledGenerationWithoutUsageOrOutputIsNotRetained(t *testing.T) {
	input := persistInterruptedMessageGenerationInput{
		UserMessage:      &model.Message{},
		AssistantMessage: &model.Message{},
		Error:            ErrMessageGenerationCanceled,
		StartedAt:        time.Now(),
	}

	if shouldPersistInterruptedMessageGeneration(input) {
		t.Fatal("expected empty canceled generation to stay non-billable")
	}
}

func TestCanceledGenerationAfterUpstreamCallUsesEstimatedInputFallback(t *testing.T) {
	input := persistInterruptedMessageGenerationInput{
		UserMessage:          &model.Message{},
		AssistantMessage:     &model.Message{},
		EstimatedInputTokens: 32,
		UpstreamCallStarted:  true,
		Error:                ErrMessageGenerationCanceled,
		StartedAt:            time.Now(),
	}

	if !shouldPersistInterruptedMessageGeneration(input) {
		t.Fatal("expected canceled upstream call to be retained with estimated input usage")
	}
	metrics := resolveInterruptedMessageGenerationMetrics(input)
	if metrics.InputTokens != 32 || metrics.OutputTokens != 0 {
		t.Fatalf("expected estimated input fallback without output charge, got %#v", metrics)
	}
}

func TestCanceledGenerationEstimatesVisibleReasoningUsage(t *testing.T) {
	reasoningText := "正在分析用户请求，并检查终止时已经显示的思考内容。"
	input := persistInterruptedMessageGenerationInput{
		UserMessage:            &model.Message{},
		AssistantMessage:       &model.Message{},
		AssistantReasoningText: reasoningText,
		EstimatedInputTokens:   12,
		Error:                  ErrMessageGenerationCanceled,
		StartedAt:              time.Now(),
	}
	if !shouldPersistInterruptedMessageGeneration(input) {
		t.Fatal("reasoning-only visible output must be retained for moderation")
	}

	metrics := resolveInterruptedMessageGenerationMetrics(input)
	if metrics.OutputTokens != 0 || metrics.ReasoningTokens != estimateTokens(reasoningText) {
		t.Fatalf("expected visible reasoning to be estimated separately, got %#v", metrics)
	}
	if source := interruptedUsageSource(input, metrics); source != interruptedUsageSourceEstimated {
		t.Fatalf("usage source = %q, want estimated", source)
	}
}

func TestCanceledGenerationDoesNotDoubleCountCombinedObservedOutput(t *testing.T) {
	input := persistInterruptedMessageGenerationInput{
		UserMessage:            &model.Message{},
		AssistantMessage:       &model.Message{},
		AssistantText:          "可见回复",
		AssistantReasoningText: "可见思考内容",
		Usage:                  llm.Usage{OutputTokens: 2},
		Error:                  ErrMessageGenerationCanceled,
		StartedAt:              time.Now(),
	}

	metrics := resolveInterruptedMessageGenerationMetrics(input)
	wantOutput := resolveObservedOrHigherEstimatedTokens(
		input.Usage.OutputTokens,
		estimateTokens(input.AssistantText)+estimateTokens(input.AssistantReasoningText),
	)
	if metrics.OutputTokens != wantOutput || metrics.ReasoningTokens != 0 {
		t.Fatalf("expected combined output without duplicated reasoning, got %#v", metrics)
	}
}

func TestCanceledGenerationRecoveredUsageIsAuthoritative(t *testing.T) {
	input := persistInterruptedMessageGenerationInput{
		UserMessage:            &model.Message{},
		AssistantMessage:       &model.Message{},
		AssistantText:          "这是一段明显更长的可见回复，用于确认恢复值不会被估算覆盖。",
		AssistantReasoningText: "这是一段明显更长的思考内容，用于确认恢复值不会被估算覆盖。",
		Usage:                  llm.Usage{InputTokens: 10, OutputTokens: 48, ReasoningTokens: 16},
		UsageRecovered:         true,
		Error:                  ErrMessageGenerationCanceled,
		StartedAt:              time.Now(),
	}

	metrics := resolveInterruptedMessageGenerationMetrics(input)
	if metrics.InputTokens != 10 || metrics.OutputTokens != 48 || metrics.ReasoningTokens != 16 {
		t.Fatalf("expected recovered usage to remain authoritative, got %#v", metrics)
	}
	if source := interruptedUsageSource(input, metrics); source != interruptedUsageSourceRecovered {
		t.Fatalf("usage source = %q, want recovered", source)
	}
}

func TestCanceledGenerationRecoveredUsageRetainsEarlierEstimatedInput(t *testing.T) {
	input := persistInterruptedMessageGenerationInput{
		UserMessage:          &model.Message{},
		AssistantMessage:     &model.Message{},
		EstimatedInputTokens: 38,
		Usage:                llm.Usage{InputTokens: 24, OutputTokens: 12},
		UsageRecovered:       true,
		Error:                ErrMessageGenerationCanceled,
		StartedAt:            time.Now(),
	}

	metrics := resolveInterruptedMessageGenerationMetrics(input)
	if metrics.InputTokens != 38 || metrics.OutputTokens != 12 {
		t.Fatalf("expected recovered current-call usage plus earlier estimated input, got %#v", metrics)
	}
	if source := interruptedUsageSource(input, metrics); source != interruptedUsageSourceMixed {
		t.Fatalf("usage source = %q, want mixed", source)
	}
}

// 中断的生成同样要保住已产出的推理内容：落库更新是无条件覆盖，
// 若不带上 ReasoningContent 就会把该轮推理抹成空字符串，
// 而 interrupted 状态的 assistant 消息仍会进入后续轮次的上下文。
func TestInterruptedGenerationRetainsReasoningContent(t *testing.T) {
	assistant := &model.Message{ReasoningContent: "stale"}
	input := persistInterruptedMessageGenerationInput{
		UserMessage:            &model.Message{},
		AssistantMessage:       assistant,
		AssistantText:          "部分可见回复",
		AssistantReasoningText: "  中断前已产出的思考内容  ",
		Error:                  ErrMessageGenerationCanceled,
		StartedAt:              time.Now(),
	}

	applyInterruptedMessageGenerationState(input, resolveInterruptedMessageGenerationMetrics(input))

	if assistant.ReasoningContent != "中断前已产出的思考内容" {
		t.Fatalf("expected trimmed reasoning to be retained, got %q", assistant.ReasoningContent)
	}
}

func TestInterruptedGenerationMovesArtifactContentOutOfReasoning(t *testing.T) {
	assistant := &model.Message{}
	input := persistInterruptedMessageGenerationInput{
		UserMessage:            &model.Message{},
		AssistantMessage:       assistant,
		AssistantText:          "部分可见回复",
		AssistantReasoningText: "处理中\n```html\n<main>preview</main>\n```\n完成",
		Error:                  ErrMessageGenerationCanceled,
		StartedAt:              time.Now(),
	}
	metrics := resolveInterruptedMessageGenerationMetrics(input)
	input.AssistantText, input.AssistantReasoningText = normalizeAssistantArtifactContent(
		input.AssistantText,
		input.AssistantReasoningText,
	)

	applyInterruptedMessageGenerationState(input, metrics)

	if !strings.Contains(assistant.Content, "```html\n<main>preview</main>\n```") {
		t.Fatalf("expected artifact in visible content, got %q", assistant.Content)
	}
	if assistant.ReasoningContent != "处理中\n完成" {
		t.Fatalf("expected artifact removed from reasoning, got %q", assistant.ReasoningContent)
	}
}

func TestModerationOutputTextIncludesVisibleReasoningWithoutDuplicates(t *testing.T) {
	got := moderationOutputText("assistant answer", "visible reasoning", "visible reasoning")
	want := "assistant answer\n\nvisible reasoning"
	if got != want {
		t.Fatalf("moderation output=%q, want %q", got, want)
	}
}
