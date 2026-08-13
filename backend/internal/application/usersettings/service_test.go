package usersettings

import (
	"strconv"
	"strings"
	"testing"
)

func TestValidateDefaultMCPToolIDs(t *testing.T) {
	t.Parallel()

	validValues := []string{
		"[]",
		"[1]",
		"[1,2,3]",
		" [42] ",
		mcpToolIDJSON(256),
	}
	for _, value := range validValues {
		if err := validateDefaultMCPToolIDs(value, "chat.default_mcp_tool_ids"); err != nil {
			t.Fatalf("expected %s to be valid, got %v", value, err)
		}
	}

	invalidValues := []string{
		"",
		"{}",
		"[0]",
		"[-1]",
		"[1.5]",
		`["1"]`,
	}
	for _, value := range invalidValues {
		if err := validateDefaultMCPToolIDs(value, "chat.default_mcp_tool_ids"); err == nil {
			t.Fatalf("expected %s to be invalid", value)
		}
	}
}

func mcpToolIDJSON(count int) string {
	values := make([]string, count)
	for index := range values {
		values[index] = strconv.Itoa(index + 1)
	}
	return "[" + strings.Join(values, ",") + "]"
}

func TestDefaultMCPToolIDsSettingIsAllowed(t *testing.T) {
	t.Parallel()

	if got := allowedKeys["chat.default_mcp_tool_ids"]; got != "[]" {
		t.Fatalf("expected chat.default_mcp_tool_ids default to be [], got %q", got)
	}
	if err := validateValue("chat.default_mcp_tool_ids", "[1,2,3]"); err != nil {
		t.Fatalf("expected chat.default_mcp_tool_ids to be accepted, got %v", err)
	}
}

func TestDefaultReasoningEffortAcceptsMax(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		if err := validateValue("chat.default_reasoning_effort", value); err != nil {
			t.Fatalf("expected chat.default_reasoning_effort=%q to be accepted, got %v", value, err)
		}
	}
	if err := validateValue("chat.default_reasoning_effort", "highest"); err == nil {
		t.Fatal("expected invalid chat.default_reasoning_effort to be rejected")
	}
}

func TestContentWidthSettingIsAllowed(t *testing.T) {
	t.Parallel()

	if got := allowedKeys["chat.content_width"]; got != "compact" {
		t.Fatalf("expected chat.content_width default to be compact, got %q", got)
	}
	for _, value := range []string{"compact", "standard", "wide"} {
		if err := validateValue("chat.content_width", value); err != nil {
			t.Fatalf("expected chat.content_width=%s to be accepted, got %v", value, err)
		}
	}
	if err := validateValue("chat.content_width", "loose"); err == nil {
		t.Fatal("expected invalid chat.content_width to be rejected")
	}
}

func TestReuseModelOptionsSettingIsAllowed(t *testing.T) {
	t.Parallel()

	if got := allowedKeys["chat.reuse_model_options"]; got != "true" {
		t.Fatalf("expected chat.reuse_model_options default to be true, got %q", got)
	}
	for _, value := range []string{"true", "false"} {
		if err := validateValue("chat.reuse_model_options", value); err != nil {
			t.Fatalf("expected chat.reuse_model_options=%s to be accepted, got %v", value, err)
		}
	}
	if err := validateValue("chat.reuse_model_options", "yes"); err == nil {
		t.Fatal("expected invalid chat.reuse_model_options to be rejected")
	}
}

func TestReasoningContentPassbackSettingIsAllowed(t *testing.T) {
	t.Parallel()

	if got := allowedKeys["chat.reasoning_content_passback"]; got != "true" {
		t.Fatalf("expected chat.reasoning_content_passback default to be true, got %q", got)
	}
	for _, value := range []string{"true", "false"} {
		if err := validateValue("chat.reasoning_content_passback", value); err != nil {
			t.Fatalf("expected chat.reasoning_content_passback=%s to be accepted, got %v", value, err)
		}
	}
	if err := validateValue("chat.reasoning_content_passback", "yes"); err == nil {
		t.Fatal("expected invalid chat.reasoning_content_passback to be rejected")
	}
}

func TestAutoGenerateLabelsSettingIsAllowed(t *testing.T) {
	t.Parallel()

	if got := allowedKeys["chat.auto_generate_labels"]; got != "true" {
		t.Fatalf("expected chat.auto_generate_labels default to be true, got %q", got)
	}
	for _, value := range []string{"true", "false"} {
		if err := validateValue("chat.auto_generate_labels", value); err != nil {
			t.Fatalf("expected chat.auto_generate_labels=%s to be accepted, got %v", value, err)
		}
	}
	if err := validateValue("chat.auto_generate_labels", "yes"); err == nil {
		t.Fatal("expected invalid chat.auto_generate_labels to be rejected")
	}
}
