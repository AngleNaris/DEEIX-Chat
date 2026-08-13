package mcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/security"
)

func TestClientCallToolUsesStreamableHTTPJSONRPC(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			t.Fatalf("expected /mcp path, got %s", r.URL.Path)
		}
		var req struct {
			ID     interface{}            `json:"id"`
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		methods = append(methods, req.Method)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session_1")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"protocolVersion": protocolVersion,
					"capabilities":    map[string]interface{}{},
					"serverInfo":      map[string]string{"name": "test", "version": "1.0.0"},
				},
			})
		case "notifications/initialized":
			if r.Header.Get("Mcp-Session-Id") != "session_1" {
				t.Fatalf("expected session header on initialized notification")
			}
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			if r.Header.Get("Mcp-Session-Id") != "session_1" {
				t.Fatalf("expected session header on tools/call")
			}
			if req.Params["name"] != "memory.list" {
				t.Fatalf("expected tool name, got %#v", req.Params["name"])
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"content": []map[string]string{{"type": "text", "text": "ok"}},
				},
			})
		default:
			t.Fatalf("unexpected method %s", req.Method)
		}
	}))
	defer server.Close()

	client := NewClient(security.NewStrictOutboundPolicy(true), "")
	output, err := client.CallTool(context.Background(), CallConfig{BaseURL: server.URL + "/mcp"}, CallInput{
		ToolName:      "memory.list",
		ArgumentsJSON: `{"scope":"user"}`,
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if output != `{"content":[{"text":"ok","type":"text"}]}` {
		t.Fatalf("unexpected output: %s", output)
	}
	if len(methods) != 3 || methods[0] != "initialize" || methods[1] != "notifications/initialized" || methods[2] != "tools/call" {
		t.Fatalf("unexpected methods: %#v", methods)
	}
}

func TestClientCallToolRejectsInvalidArgumentsJSON(t *testing.T) {
	client := NewClient(security.OutboundPolicy{}, "")
	_, err := client.CallTool(context.Background(), CallConfig{BaseURL: "http://127.0.0.1/mcp"}, CallInput{
		ToolName:      "bing_search",
		ArgumentsJSON: `{bad`,
	})
	if err == nil || err.Error() != "mcp tool arguments must be a valid JSON object" {
		t.Fatalf("expected invalid arguments error, got %v", err)
	}
}

func TestClientCallToolTreatsMCPResultErrorAsExecutionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session_1")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": map[string]interface{}{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"isError": true,
					"content": []map[string]string{{"type": "text", "text": "missing required field query"}},
				},
			})
		default:
			t.Fatalf("unexpected method %s", req.Method)
		}
	}))
	defer server.Close()

	client := NewClient(security.OutboundPolicy{}, "")
	_, err := client.CallTool(context.Background(), CallConfig{BaseURL: server.URL}, CallInput{
		ToolName:      "bing_search",
		ArgumentsJSON: `{}`,
	})
	if err == nil || err.Error() != "mcp tool error: missing required field query" {
		t.Fatalf("expected MCP tool error, got %v", err)
	}
}

func TestClientCallToolTreatsWrappedMCPProtocolErrorAsExecutionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session_1")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": map[string]interface{}{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]interface{}{
					"content": []map[string]string{{
						"type": "text",
						"text": "MCP error -32602: Input validation error: Invalid arguments for tool bing_search",
					}},
				},
			})
		default:
			t.Fatalf("unexpected method %s", req.Method)
		}
	}))
	defer server.Close()

	client := NewClient(security.OutboundPolicy{}, "")
	_, err := client.CallTool(context.Background(), CallConfig{BaseURL: server.URL}, CallInput{
		ToolName:      "bing_search",
		ArgumentsJSON: `{}`,
	})
	if err == nil || err.Error() != "mcp tool error: MCP error -32602: Input validation error: Invalid arguments for tool bing_search" {
		t.Fatalf("expected wrapped MCP protocol error, got %v", err)
	}
}

func TestClientListToolsParsesSSEJSONRPC(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     interface{} `json:"id"`
			Method string      `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": map[string]interface{}{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":3,\"result\":{\"tools\":[{\"name\":\"memory.list\",\"description\":\"List memories\"}]}}\n\n"))
		default:
			t.Fatalf("unexpected method %s", req.Method)
		}
	}))
	defer server.Close()

	client := NewClient(security.OutboundPolicy{}, "")
	tools, err := client.ListTools(context.Background(), CallConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "memory.list" || tools[0].Description != "List memories" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
}

func TestParseRPCResponseParsesLargeSingleLineSSEData(t *testing.T) {
	toolDescription := strings.Repeat("x", 70*1024)
	payload := `event: message
data: {"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"memory.list","description":"` + toolDescription + `"}]}}

`

	result, err := parseRPCResponse("text/event-stream", []byte(payload))
	if err != nil {
		t.Fatalf("parse rpc response: %v", err)
	}

	var parsed struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(parsed.Tools) != 1 || parsed.Tools[0].Name != "memory.list" {
		t.Fatalf("unexpected tools: %#v", parsed.Tools)
	}
	if parsed.Tools[0].Description != toolDescription {
		t.Fatalf("unexpected tool description length: got %d want %d", len(parsed.Tools[0].Description), len(toolDescription))
	}
}

// TestCallToolSignsMeta 验证配置 HMAC 密钥后 _meta 附带 ts/sig，
// canonical 串与 tools/sandbox-mcp 的 metaSignature 保持一致（P0-07 服务间身份）。
func TestCallToolSignsMeta(t *testing.T) {
	const key = "shared-hmac-key"
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session_sig")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": 1,
				"result": map[string]interface{}{
					"protocolVersion": protocolVersion,
					"capabilities":    map[string]interface{}{},
					"serverInfo":      map[string]string{"name": "test", "version": "1.0.0"},
				},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/call":
			captured, _ = req.Params["_meta"].(map[string]interface{})
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"jsonrpc": "2.0", "id": 1,
				"result": map[string]interface{}{"content": []map[string]string{{"type": "text", "text": "ok"}}},
			})
		}
	}))
	defer server.Close()

	client := NewClient(security.NewStrictOutboundPolicy(true), key)
	if _, err := client.CallTool(context.Background(), CallConfig{BaseURL: server.URL + "/mcp"}, CallInput{
		ToolName: "memory.list", ArgumentsJSON: `{"scope":"user"}`,
		UserID: 42, ConversationID: 7, RequestID: "req-sig",
	}); err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if captured == nil {
		t.Fatal("expected _meta in tools/call params")
	}
	tsFloat, _ := captured["ts"].(float64)
	ts := int64(tsFloat)
	sig, _ := captured["sig"].(string)
	callID, _ := captured["call_id"].(string)
	if strings.TrimSpace(callID) == "" {
		t.Fatal("expected non-empty per-call ID")
	}
	// 独立重算 HMAC（与 sandbox-mcp 相同的 canonical 格式），必须一致。
	canonical := fmt.Sprintf("user_id=%d\nconversation_id=%d\nrequest_id=%s\ncall_id=%s\nts=%d", 42, 7, "req-sig", callID, ts)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))
	if sig != expected {
		t.Fatalf("meta signature mismatch: got %q want %q", sig, expected)
	}
	if ts <= 0 {
		t.Fatalf("expected positive timestamp, got %d", ts)
	}
}
