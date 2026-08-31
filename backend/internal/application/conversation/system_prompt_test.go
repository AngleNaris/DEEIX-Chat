package conversation

import (
	"strings"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

func TestResolveMessageSystemPromptInjectionUsesNativeSystemPrompt(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol:              llm.AdapterOpenAIResponses,
		ModelSystemPrompt:     "model rule",
		ModelCapabilitiesJSON: `{"supportsSystemPrompt":true}`,
	}

	got := resolveMessageSystemPromptInjection(config.Config{DefaultSystemPrompt: "global rule"}, route, "project rule", "role rule", false, systemPromptVars{})
	if got.Content == "" {
		t.Fatal("expected system prompt content")
	}
	if got.InlineToUser {
		t.Fatal("expected native system prompt")
	}
	for _, want := range []string{`<layers order="high_to_low">`, `<platform p="100">`, "global rule", `<model p="80">`, "model rule", `<project p="60" override="no">`, "project rule", `<role p="40" override="no">`, "role rule"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("expected content to contain %q, got %q", want, got.Content)
		}
	}
	if !strings.Contains(got.Content, "not a compliance percentage") {
		t.Fatalf("expected priority semantics to be explicit, got %q", got.Content)
	}
	if strings.Contains(got.Content, "# Global instructions") {
		t.Fatalf("expected XML prompt layers, got %q", got.Content)
	}
}

func TestResolveMessageSystemPromptInjectionExpandsEveryEditableLayer(t *testing.T) {
	vars := systemPromptVars{
		Date:     "2026-08-31",
		Language: "zh-CN",
		Username: "alice",
	}
	route := &channel.ResolvedRoute{
		Protocol:          llm.AdapterOpenAIResponses,
		ModelSystemPrompt: "model {{language}}",
	}

	got := resolveMessageSystemPromptInjection(
		config.Config{DefaultSystemPrompt: "platform {{date}}"},
		route,
		"project {{username}}",
		"role {{js: 6 * 7}}",
		false,
		vars,
	)
	for _, want := range []string{"platform 2026-08-31", "model zh-CN", "project alice", "role 42"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("expanded prompt missing %q: %s", want, got.Content)
		}
	}
	if strings.Contains(got.Content, "{{") {
		t.Fatalf("supported dynamic variable leaked into prompt: %s", got.Content)
	}
}

func TestResolveMessageSystemPromptInjectionAddsHTMLVisualPrompt(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol: llm.AdapterOpenAIResponses,
	}

	got := resolveMessageSystemPromptInjection(config.Config{}, route, "", "", true, systemPromptVars{})
	if got.Content == "" {
		t.Fatal("expected request-level system prompt content")
	}
	if got.InlineToUser {
		t.Fatal("expected native system prompt")
	}
	for _, want := range []string{`<format p="100" scope="request">`, "html-visual", "遵循用户语言", "HTML 实时渲染", "theme-variables", "--background", "var(--card)"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("expected content to contain %q, got %q", want, got.Content)
		}
	}
	if strings.Contains(got.Content, "使用简体中文") {
		t.Fatalf("expected user-language prompt, got %q", got.Content)
	}
}

func TestResolveMessageSystemPromptInjectionRestrictsHTMLVisualThemeVariables(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol: llm.AdapterOpenAIResponses,
	}

	got := resolveMessageSystemPromptInjection(config.Config{}, route, "", "", true, systemPromptVars{})
	for _, want := range []string{"只能引用上述变量", "禁止在 style 中定义或覆盖 CSS 自定义属性", "--card 搭配 --card-foreground", "color-mix()"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("expected HTML theme-variable constraint %q, got %q", want, got.Content)
		}
	}
	if strings.Contains(got.Content, "当前深色模式") || strings.Contains(got.Content, "当前浅色模式") {
		t.Fatalf("expected theme variables instead of generation-time color mode, got %q", got.Content)
	}
}

func TestResolveMessageSystemPromptInjectionOrdersProjectAndRoleBeforeResponseFormat(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol: llm.AdapterOpenAIResponses,
	}

	got := resolveMessageSystemPromptInjection(config.Config{}, route, "project rule", "role rule", true, systemPromptVars{})
	projectIndex := strings.Index(got.Content, `<project p="100" override="no">`)
	roleIndex := strings.Index(got.Content, `<role p="80" override="no">`)
	responseIndex := strings.Index(got.Content, `<format p="60" scope="request">`)
	if projectIndex < 0 || roleIndex < 0 || responseIndex < 0 {
		t.Fatalf("expected project, role, and response format layers, got %q", got.Content)
	}
	if projectIndex > roleIndex || roleIndex > responseIndex {
		t.Fatalf("expected project and role instructions before response format instructions, got %q", got.Content)
	}
}

func TestResolveMessageSystemPromptInjectionCompactsActiveLayerPriorities(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol: llm.AdapterOpenAIResponses,
	}

	got := resolveMessageSystemPromptInjection(config.Config{DefaultSystemPrompt: "global rule"}, route, "project rule", "", true, systemPromptVars{})
	for _, want := range []string{`<platform p="100">`, `<project p="80" override="no">`, `<format p="60" scope="request">`} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("expected compacted active priority %q, got %q", want, got.Content)
		}
	}
	if strings.Contains(got.Content, `p="40"`) {
		t.Fatalf("expected no skipped priority slot when model layer is empty, got %q", got.Content)
	}
}

func TestResolveMessageSystemPromptInjectionKeepsAllLayerPrioritiesDistinct(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol:          llm.AdapterOpenAIResponses,
		ModelSystemPrompt: "model rule",
	}

	got := resolveMessageSystemPromptInjection(config.Config{DefaultSystemPrompt: "global rule"}, route, "project rule", "role rule", true, systemPromptVars{})
	for _, want := range []string{`<platform p="100">`, `<model p="80">`, `<project p="60" override="no">`, `<role p="40" override="no">`, `<format p="20" scope="request">`} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("expected distinct layer priority %q, got %q", want, got.Content)
		}
	}
}

func TestResolveMessageSystemPromptInjectionMarksProjectOverrideBoundary(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol: llm.AdapterOpenAIResponses,
	}

	got := resolveMessageSystemPromptInjection(config.Config{}, route, "project rule", "", false, systemPromptVars{})
	for _, want := range []string{`<project p="100" override="no">`, "must not override platform or model instructions"} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("expected project boundary %q, got %q", want, got.Content)
		}
	}
}

func TestResolveMessageSystemPromptInjectionPreservesXMLLikeContent(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol: llm.AdapterOpenAIResponses,
	}

	got := resolveMessageSystemPromptInjection(config.Config{DefaultSystemPrompt: `keep <tag> and ]]> safely`}, route, "", "", false, systemPromptVars{})
	for _, want := range []string{`<![CDATA[keep <tag> and ]]]]><![CDATA[> safely]]>`, `<platform p="100">`} {
		if !strings.Contains(got.Content, want) {
			t.Fatalf("expected XML-safe content %q, got %q", want, got.Content)
		}
	}
}

func TestResolveMessageSystemPromptInjectionSkipsHTMLVisualPromptWhenDisabled(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol: llm.AdapterOpenAIResponses,
	}

	got := resolveMessageSystemPromptInjection(config.Config{}, route, "", "", false, systemPromptVars{})
	if got.Content != "" {
		t.Fatalf("expected no system prompt content, got %q", got.Content)
	}
}

func TestResolveMessageSystemPromptInjectionFallsBackWhenCapabilitiesDisableSystemPrompt(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol:              llm.AdapterOpenAIResponses,
		ModelCapabilitiesJSON: `{"supportsSystemPrompt":false}`,
	}

	got := resolveMessageSystemPromptInjection(config.Config{DefaultSystemPrompt: "global rule"}, route, "", "", false, systemPromptVars{})
	if !got.InlineToUser {
		t.Fatal("expected user prompt fallback")
	}
}

func TestResolveMessageSystemPromptInjectionFallsBackWithSnakeCaseCapabilities(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol:              llm.AdapterOpenAIResponses,
		ModelCapabilitiesJSON: `{"supports_system_prompt":false}`,
	}

	got := resolveMessageSystemPromptInjection(config.Config{DefaultSystemPrompt: "global rule"}, route, "", "", false, systemPromptVars{})
	if !got.InlineToUser {
		t.Fatal("expected snake_case capability to use user prompt fallback")
	}
}

func TestResolveMessageSystemPromptInjectionFallsBackWhenModeRequestsUserPrompt(t *testing.T) {
	route := &channel.ResolvedRoute{
		Protocol:              llm.AdapterOpenAIResponses,
		ModelCapabilitiesJSON: `{"systemPromptMode":"user"}`,
	}

	got := resolveMessageSystemPromptInjection(config.Config{DefaultSystemPrompt: "global rule"}, route, "", "", false, systemPromptVars{})
	if !got.InlineToUser {
		t.Fatal("expected systemPromptMode=user to use user prompt fallback")
	}
}

func TestResolveMessageSystemPromptInjectionFallsBackForGemma(t *testing.T) {
	route := &channel.ResolvedRoute{
		PlatformModelName: "gemma-3-27b",
		Protocol:          llm.AdapterGoogleGenerateContent,
	}

	got := resolveMessageSystemPromptInjection(config.Config{DefaultSystemPrompt: "global rule"}, route, "", "", false, systemPromptVars{})
	if !got.InlineToUser {
		t.Fatal("expected Gemma to inline system prompt into user prompt")
	}
}

func TestInlineSystemPromptIntoLatestUserMessage(t *testing.T) {
	messages := []llm.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "second"},
	}

	got := inlineSystemPromptIntoLatestUserMessage(messages, "system rule")
	if got[0].Content != "first" {
		t.Fatalf("expected first user message to stay unchanged, got %q", got[0].Content)
	}
	if !strings.Contains(got[2].Content, "<system_instructions>") || !strings.Contains(got[2].Content, "system rule") || !strings.Contains(got[2].Content, "second") {
		t.Fatalf("expected latest user message to include inline system prompt and original content, got %q", got[2].Content)
	}
}
