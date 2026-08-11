package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

// minimalDeEIXClient 模拟 DEEIX 后端的 MCP 客户端调用流程
// （initialize -> notifications/initialized -> tools/list -> tools/call），
// 协议细节参照 backend/internal/infra/mcp/client.go。
type minimalDeEIXClient struct {
	baseURL string
	token   string
	session string
}

func (c *minimalDeEIXClient) rpc(t *testing.T, method string, params map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	req, err := http.NewRequest(http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if next := strings.TrimSpace(resp.Header.Get("Mcp-Session-Id")); next != "" {
		c.session = next
	}
	data, _ := io.ReadAll(resp.Body)
	payload := strings.TrimSpace(string(data))
	mediaType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(mediaType, "text/event-stream") {
		// 抽取 data: 行（DEEIX parseRPCResponse 同款逻辑）
		var lines []string
		for _, line := range strings.Split(payload, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		payload = strings.Join(lines, "\n")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("rpc %s: bad response %q (status %d): %v", method, payload, resp.StatusCode, err)
	}
	return out
}

func (c *minimalDeEIXClient) initialize(t *testing.T) {
	t.Helper()
	out := c.rpc(t, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "deeix-test", "version": "0"},
	})
	if out["error"] != nil {
		t.Fatalf("initialize failed: %v", out["error"])
	}
	result, _ := out["result"].(map[string]any)
	if result["protocolVersion"] == nil {
		t.Fatalf("initialize missing protocolVersion: %v", out)
	}
	c.rpc(t, "notifications/initialized", nil)
}

func (c *minimalDeEIXClient) listTools(t *testing.T) []string {
	t.Helper()
	out := c.rpc(t, "tools/list", map[string]any{})
	if out["error"] != nil {
		t.Fatalf("tools/list failed: %v", out["error"])
	}
	result, _ := out["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		if name, ok := tool["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func (c *minimalDeEIXClient) callTool(t *testing.T, name string, arguments map[string]any, meta map[string]any) (map[string]any, int) {
	t.Helper()
	params := map[string]any{"name": name, "arguments": arguments}
	if meta != nil {
		params["_meta"] = meta
	}
	out := c.rpc(t, "tools/call", params)
	return out, http.StatusOK
}

func newTestServer(t *testing.T, apiKey string) (*httptest.Server, *SessionManager) {
	t.Helper()
	cfg := &Config{
		ListenAddr:       "127.0.0.1:0",
		APIKey:           apiKey,
		BaseImage:        "deeix-sandbox-base:latest",
		WorkspaceDir:     "/workspace",
		LeaseTTL:         5 * 1000 * 1000 * 1000,
		ExecTimeout:      10 * 1000 * 1000 * 1000,
		MaxExecTimeout:   30 * 1000 * 1000 * 1000,
		OutputLimitBytes: 65536,
		MemoryLimit:      "1g",
		PidsLimit:        256,
		CPUsLimit:        0.5,
		CacheVolume:      "deeix-sandbox-cache",
		MaxTasksPerSession: 4,
	}
	d := &dockerClient{} // 无真实 Docker（本测试不触 Docker 路径）
	mgr := NewSessionManager(cfg, d)
	s := newSandboxServer(cfg, mgr)
	mcpServer := server.NewMCPServer("deeix-sandbox-mcp", "1.0.0")
	registerTools(mcpServer, s)
	httpSrv := server.NewStreamableHTTPServer(mcpServer)
	ts := httptest.NewServer(authMiddleware(cfg, httpSrv))
	t.Cleanup(ts.Close)
	return ts, mgr
}

func TestHandshakeAndToolList(t *testing.T) {
	ts, _ := newTestServer(t, "test-key")
	client := &minimalDeEIXClient{baseURL: ts.URL, token: "test-key"}
	client.initialize(t)
	names := client.listTools(t)
	want := []string{"sandbox_exec", "sandbox_task_start", "sandbox_task_poll", "sandbox_task_cancel",
		"sandbox_write_file", "sandbox_read_file", "sandbox_list_files", "sandbox_download",
		"sandbox_spawn", "sandbox_ps", "sandbox_kill", "sandbox_reset"}
	for _, w := range want {
		found := false
		for _, n := range names {
			if n == w {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tool %s missing from tools/list: %v", w, names)
		}
	}
}

func TestAuthRejectsMissingOrWrongToken(t *testing.T) {
	ts, _ := newTestServer(t, "secret")
	// 无 token -> 401
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	req, _ := http.NewRequest(http.MethodPost, ts.URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", resp.StatusCode)
	}
	// 错误 token -> 401
	client := &minimalDeEIXClient{baseURL: ts.URL, token: "wrong"}
	out := client.rpc(t, "initialize", map[string]any{})
	// rpc helper 不检查 status；直接验证响应含 unauthorized
	if !strings.Contains(mustJSON(out), "unauthorized") {
		t.Fatalf("expected unauthorized error, got %v", out)
	}
}

func mustJSON(v map[string]any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func TestCallToolWithoutMetaRejected(t *testing.T) {
	ts, _ := newTestServer(t, "test-key")
	client := &minimalDeEIXClient{baseURL: ts.URL, token: "test-key"}
	client.initialize(t)
	out, _ := client.callTool(t, "sandbox_ps", map[string]any{}, nil)
	result, _ := out["result"].(map[string]any)
	if result == nil {
		t.Fatalf("expected tool result, got %v", out)
	}
	content, _ := result["content"].([]any)
	text := ""
	if len(content) > 0 {
		item, _ := content[0].(map[string]any)
		text, _ = item["text"].(string)
	}
	if !strings.Contains(text, "missing DEEIX _meta") {
		t.Fatalf("expected _meta rejection, got: %s", text)
	}
}

func TestCallToolWithMetaRoutesScope(t *testing.T) {
	ts, mgr := newTestServer(t, "test-key")
	client := &minimalDeEIXClient{baseURL: ts.URL, token: "test-key"}
	client.initialize(t)
	// sandbox_ps 不触 Docker：带合法 _meta 应返回 sessions 列表
	out, _ := client.callTool(t, "sandbox_ps", map[string]any{}, map[string]any{
		"user_id": 42, "conversation_id": 7, "request_id": "req-1",
	})
	result, _ := out["result"].(map[string]any)
	content, _ := result["content"].([]any)
	text := ""
	if len(content) > 0 {
		item, _ := content[0].(map[string]any)
		text, _ = item["text"].(string)
	}
	if !strings.Contains(text, `"sessions"`) {
		t.Fatalf("expected sessions payload, got: %s", text)
	}
	if len(mgr.List()) != 0 {
		t.Fatalf("sandbox_ps should not create sessions, got %d", len(mgr.List()))
	}
}

func TestAuthDisabledWhenKeyEmpty(t *testing.T) {
	ts, _ := newTestServer(t, "")
	client := &minimalDeEIXClient{baseURL: ts.URL, token: ""}
	client.initialize(t) // 无 token 也应通过
}

var _ = context.Background
