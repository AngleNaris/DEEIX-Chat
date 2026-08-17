package conversation

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/mcp"
)

func TestInjectMCPToolGuidanceOnlyAddsPolicy(t *testing.T) {
	messages := []llm.Message{{Role: "user", Content: "搜索 DEEIX Chat"}}
	runtime := selectedToolRuntime{
		definitions: []llm.ToolDefinition{{
			Name:        "bing_search",
			Description: "搜索网页",
			InputSchema: []byte(`{"type":"object","properties":{"query":{"type":"string"},"count":{"type":"number"}},"required":["query"]}`),
		}},
	}

	result := injectMCPToolGuidance(messages, runtime, "")
	if len(result) != 2 {
		t.Fatalf("expected guidance message to be injected, got %#v", result)
	}
	guidance := result[0].Content
	for _, want := range []string{"# tool_use", "declared separately via the API schema", "Use the fewest useful calls"} {
		if !strings.Contains(guidance, want) {
			t.Fatalf("expected guidance to contain %q, got %q", want, guidance)
		}
	}
	for _, unwanted := range []string{"# tools", "bing_search", "query:string", "count:number"} {
		if strings.Contains(guidance, unwanted) {
			t.Fatalf("expected guidance not to duplicate tool schema %q, got %q", unwanted, guidance)
		}
	}
}

func TestInjectMCPToolGuidanceUsesCustomPrompt(t *testing.T) {
	messages := []llm.Message{{Role: "user", Content: "搜索 DEEIX Chat"}}
	runtime := selectedToolRuntime{
		definitions: []llm.ToolDefinition{{Name: "bing_search"}},
	}

	result := injectMCPToolGuidance(messages, runtime, "Use MCP tools only after checking user intent.")
	if len(result) != 2 {
		t.Fatalf("expected guidance message to be injected, got %#v", result)
	}
	if result[0].Content != "Use MCP tools only after checking user intent." {
		t.Fatalf("expected custom prompt, got %q", result[0].Content)
	}
}

func TestSelectedToolRuntimeDisclosesMCPToolsAfterActivation(t *testing.T) {
	persisted := make([][]uint, 0, 1)
	runtime := selectedToolRuntime{
		authorizedMCPTools: map[string]authorizedMCPTool{
			"search": {
				serverID: 7,
				definition: llm.ToolDefinition{
					Name:        "search",
					Description: "Search the web",
					InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
				},
				toolName: "search",
				config:   mcp.CallConfig{BaseURL: "https://example.com/mcp"},
				schema:   json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
			},
		},
		authorizedMCPOrder: []string{"search"},
		authorizedMCPServers: map[uint]authorizedMCPServer{
			7: {id: 7, name: "Web", description: "Current public web data"},
		},
		mcpActivation: newMCPActivationState(nil),
		onMCPActivation: func(_ context.Context, serverIDs []uint) error {
			persisted = append(persisted, append([]uint(nil), serverIDs...))
			return nil
		},
	}

	initial := runtime.visibleRuntime()
	if got := toolDefinitionNames(initial.definitions); !reflect.DeepEqual(got, []string{mcpActivateServerToolName}) {
		t.Fatalf("expected only activation control before activation, got %v", got)
	}
	if _, ok := initial.mcpConfigs["search"]; ok {
		t.Fatalf("MCP config became executable before activation")
	}
	if !strings.Contains(initial.definitions[0].Description, "server_id=7; name=Web") {
		t.Fatalf("activation directory missing stable server id and name: %q", initial.definitions[0].Description)
	}
	if !strings.Contains(initial.definitions[0].Description, "automatic same-run follow-up") ||
		!strings.Contains(initial.definitions[0].Description, "Never ask the user to send another message") {
		t.Fatalf("activation directory missing same-run continuation guidance: %q", initial.definitions[0].Description)
	}

	changed, err := runtime.activateMCPServer(t.Context(), 7)
	if err != nil || !changed {
		t.Fatalf("activate authorized server: changed=%v err=%v", changed, err)
	}
	if !reflect.DeepEqual(persisted, [][]uint{{7}}) {
		t.Fatalf("unexpected persisted activation state: %#v", persisted)
	}

	visible := runtime.visibleRuntime()
	if got := toolDefinitionNames(visible.definitions); !reflect.DeepEqual(got, []string{mcpActivateServerToolName, "search"}) {
		t.Fatalf("expected selected server tools after activation, got %v", got)
	}
	if visible.nameMap["search"] != "search" || visible.mcpConfigs["search"].BaseURL == "" {
		t.Fatalf("activated tool was not executable: %#v", visible)
	}

	changed, err = runtime.activateMCPServer(t.Context(), 7)
	if err != nil || changed {
		t.Fatalf("duplicate activation should be a no-op: changed=%v err=%v", changed, err)
	}
	if len(persisted) != 1 {
		t.Fatalf("duplicate activation persisted another snapshot: %#v", persisted)
	}
}

func TestSelectedToolRuntimeRejectsUnauthorizedActivationAndPrunesRestoredState(t *testing.T) {
	activation := newMCPActivationState([]uint{7, 99})
	activation.retainAuthorizedServers(map[uint]authorizedMCPServer{7: {id: 7}})
	if got := activation.activeServerIDs(); !reflect.DeepEqual(got, []uint{7}) {
		t.Fatalf("expected restored activation to be intersected with authorized servers, got %v", got)
	}

	runtime := selectedToolRuntime{
		authorizedMCPServers: map[uint]authorizedMCPServer{7: {id: 7}},
		mcpActivation:        activation,
	}
	changed, err := runtime.activateMCPServer(t.Context(), 99)
	if err == nil || changed || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("expected unauthorized activation to fail closed, changed=%v err=%v", changed, err)
	}
}

func TestSelectedToolRuntimeDirectoryUsesStableIDsForDuplicateNames(t *testing.T) {
	runtime := selectedToolRuntime{
		authorizedMCPServers: map[uint]authorizedMCPServer{
			3: {id: 3, name: "Search", description: "Internal documents"},
			8: {id: 8, name: "Search", description: "Public web"},
		},
	}
	description := runtime.mcpActivationDescription()
	for _, expected := range []string{
		"server_id=3; name=Search; description=Internal documents",
		"server_id=8; name=Search; description=Public web",
	} {
		if !strings.Contains(description, expected) {
			t.Fatalf("directory missing %q: %s", expected, description)
		}
	}
}

func toolDefinitionNames(definitions []llm.ToolDefinition) []string {
	result := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, definition.Name)
	}
	return result
}

func TestAppendTextOnlyModelToolWarning(t *testing.T) {
	warned := appendTextOnlyModelToolWarning("Read an image file and return its visual content.")
	if !strings.Contains(warned, "text-only model") || !strings.Contains(warned, "cannot view image content") {
		t.Fatalf("warning missing text-only guidance: %s", warned)
	}
	if !strings.HasPrefix(warned, "Read an image file") {
		t.Fatalf("warning must preserve original description: %s", warned)
	}
	if strings.Contains(appendTextOnlyModelToolWarning(""), "Read") {
		t.Fatalf("empty description must produce only the warning")
	}
	if !strings.Contains(appendTextOnlyModelToolWarning(""), "ask the user") {
		t.Fatalf("warning must guide asking the user for text: %s", warned)
	}
}
