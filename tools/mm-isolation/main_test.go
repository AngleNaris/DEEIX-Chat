package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func signedMeta(uid, cid uint, requestID, callID string, ts int64) map[string]any {
	canonical := fmt.Sprintf("user_id=%d\nconversation_id=%d\nrequest_id=%s\ncall_id=%s\nts=%d", uid, cid, requestID, callID, ts)
	mac := hmac.New(sha256.New, []byte("secret"))
	_, _ = mac.Write([]byte(canonical))
	return map[string]any{
		"user_id": uid, "conversation_id": cid, "request_id": requestID,
		"call_id": callID, "ts": ts, "sig": hex.EncodeToString(mac.Sum(nil)),
	}
}

func TestMMOutputRetentionDurationOverrides(t *testing.T) {
	t.Setenv("MM_OUTPUT_TTL_SEC", "")
	if got := durationEnv("MM_OUTPUT_TTL_SEC", defaultMMSweepTTL); got != 24*time.Hour {
		t.Fatalf("default output TTL = %v", got)
	}
	t.Setenv("MM_OUTPUT_TTL_SEC", "120")
	if got := durationEnv("MM_OUTPUT_TTL_SEC", defaultMMSweepTTL); got != 2*time.Minute {
		t.Fatalf("overridden output TTL = %v", got)
	}
}

func TestParseMetaAndReplay(t *testing.T) {
	now := time.Now()
	m, err := parseMeta(signedMeta(4, 9, "r1", "c1", now.Unix()), []byte("secret"))
	if err != nil || m.UserID != 4 || m.ConversationID != 9 || m.ToolCallID != "c1" {
		t.Fatalf("parse signed meta: %+v %v", m, err)
	}
	if _, err := parseMeta(signedMeta(4, 0, "r1", "c1", now.Unix()), []byte("secret")); err == nil {
		t.Fatal("zero conversation scope accepted")
	}
	missingCall := signedMeta(4, 9, "r1", "c1", now.Unix())
	delete(missingCall, "call_id")
	if _, err := parseMeta(missingCall, []byte("secret")); err == nil {
		t.Fatal("missing call ID accepted")
	}
	if _, err := parseMeta(signedMeta(4, 9, "r1", "c1", now.Add(-6*time.Minute).Unix()), []byte("secret")); err == nil {
		t.Fatal("expired metadata accepted")
	}

	cache := replayCache{values: map[string]time.Time{}}
	if !cache.accept("4/9/r1/c1", now) || cache.accept("4/9/r1/c1", now) {
		t.Fatal("identical call replay accepted")
	}
	if !cache.accept("4/9/r1/c2", now) {
		t.Fatal("second call in the same request rejected")
	}
}

func TestStagePathScopeCollisionsAndRewrite(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	imports := filepath.Join(root, "imports")
	staging := filepath.Join(root, "staging")
	scope := filepath.Join(shared, "deeix-4-9")
	for _, dir := range []string{"one", "two"} {
		if err := os.MkdirAll(filepath.Join(scope, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(scope, "one", "photo.png")
	second := filepath.Join(scope, "two", "photo.png")
	if err := os.WriteFile(first, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}

	p := &proxy{shared: shared, imports: imports, staging: staging}
	args := map[string]any{
		"image_paths": []any{
			"/shared/deeix-4-9/one/photo.png",
			"/shared/deeix-4-9/two/photo.png",
			"/shared/deeix-4-9/one/photo.png",
		},
		"prompt": "keep this",
	}
	staged, err := p.stage("inspect", args, meta{UserID: 4, ConversationID: 9, RequestID: "r1", ToolCallID: "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	lane := staged.lane
	paths := args["image_paths"].([]any)
	if paths[0] == paths[1] {
		t.Fatalf("same-basename inputs collided: %v", paths)
	}
	if paths[0] != paths[2] {
		t.Fatalf("duplicate source was not reused: %v", paths)
	}
	entries, err := os.ReadDir(lane)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("staged %d files, want 2", len(entries))
	}
	contents := map[string]bool{}
	for _, entry := range entries {
		data, readErr := os.ReadFile(filepath.Join(lane, entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		contents[string(data)] = true
	}
	if !contents["first"] || !contents["second"] {
		t.Fatalf("unexpected staged contents: %v", contents)
	}

	otherStaged, err := p.stage("inspect", map[string]any{}, meta{UserID: 4, ConversationID: 9, RequestID: "r1", ToolCallID: "ab"})
	if err != nil {
		t.Fatal(err)
	}
	otherLane := otherStaged.lane
	if lane == otherLane {
		t.Fatal("sanitized call ID collision produced the same lane")
	}

	bad := map[string]any{"image_path": "/shared/deeix-4-10/photo.png"}
	if _, err := p.stage("inspect", bad, meta{UserID: 4, ConversationID: 9, RequestID: "r2", ToolCallID: "c2"}); err == nil {
		t.Fatal("cross-scope path accepted")
	}
}

func TestStageRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	scope := filepath.Join(shared, "deeix-4-9")
	if err := os.MkdirAll(scope, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(scope, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	p := &proxy{shared: shared, imports: filepath.Join(root, "imports"), staging: filepath.Join(root, "staging")}
	args := map[string]any{"file_path": "/shared/deeix-4-9/escape.txt"}
	if _, err := p.stage("inspect", args, meta{UserID: 4, ConversationID: 9, RequestID: "r1", ToolCallID: "c1"}); err == nil {
		t.Fatal("symlink escape accepted")
	}
}

func TestProxySerializesCallsAndCleansStaging(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	staging := filepath.Join(root, "staging")
	for _, scope := range []string{"deeix-4-9", "deeix-5-10"} {
		if err := os.MkdirAll(filepath.Join(shared, scope), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(shared, scope, "input.txt"), []byte(scope), 0600); err != nil {
			t.Fatal(err)
		}
	}

	var active atomic.Int32
	var maxActive atomic.Int32
	var observedMu sync.Mutex
	observed := make([]string, 0, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			maximum := maxActive.Load()
			if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
				break
			}
		}
		files := regularFileContents(t, staging)
		observedMu.Lock()
		observed = append(observed, strings.Join(files, ","))
		observedMu.Unlock()
		time.Sleep(75 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	p := &proxy{
		upstream: u, shared: shared, imports: filepath.Join(root, "imports"), staging: staging,
		key: []byte("secret"), replays: replayCache{values: map[string]time.Time{}},
	}
	p.transport = &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(u) }}

	type call struct {
		uid, cid uint
		request  string
		callID   string
	}
	calls := []call{{4, 9, "request", "call-a"}, {5, 10, "request", "call-b"}}
	start := make(chan struct{})
	results := make(chan int, len(calls))
	for _, item := range calls {
		item := item
		go func() {
			<-start
			body := toolCallBody(item.uid, item.cid, item.request, item.callID)
			req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
			recorder := httptest.NewRecorder()
			p.ServeHTTP(recorder, req)
			results <- recorder.Code
		}()
	}
	close(start)
	for range calls {
		if code := <-results; code != http.StatusOK {
			t.Fatalf("proxy returned status %d", code)
		}
	}
	if maxActive.Load() != 1 {
		t.Fatalf("upstream observed %d concurrent calls", maxActive.Load())
	}
	observedMu.Lock()
	defer observedMu.Unlock()
	if len(observed) != 2 {
		t.Fatalf("upstream observed %d calls", len(observed))
	}
	for _, contents := range observed {
		if contents != "deeix-4-9" && contents != "deeix-5-10" {
			t.Fatalf("upstream saw cross-call staging contents %q", contents)
		}
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging not cleaned: %v", entries)
	}
}

func toolCallBody(uid, cid uint, requestID, callID string) []byte {
	envelope := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name": "inspect", "arguments": map[string]any{"file_path": fmt.Sprintf("/shared/deeix-%d-%d/input.txt", uid, cid)},
			"_meta": signedMeta(uid, cid, requestID, callID, time.Now().Unix()),
		},
	}
	body, _ := json.Marshal(envelope)
	return body
}

func regularFileContents(t *testing.T, root string) []string {
	t.Helper()
	result := make([]string, 0)
	err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() {
			data, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			result = append(result, string(data))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestProducerStageForcesOutputArguments(t *testing.T) {
	root := t.TempDir()
	p := &proxy{
		shared:  filepath.Join(root, "shared"),
		imports: filepath.Join(root, "imports"),
		staging: filepath.Join(root, "staging"),
		output:  filepath.Join(root, "output"),
	}
	if err := os.MkdirAll(p.output, 0755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		tool      string
		outputKey string
		wantFile  bool
	}{
		{tool: "crop", outputKey: "output_path", wantFile: true},
		{tool: "draw_bbox", outputKey: "output_path", wantFile: true},
		{tool: "save_view", outputKey: "output_dir"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			args := map[string]any{tt.outputKey: "/shared/deeix-999-999/stolen.png"}
			staged, err := p.stage(tt.tool, args, meta{UserID: 4, ConversationID: 9, RequestID: "r1", ToolCallID: tt.tool})
			if err != nil {
				t.Fatal(err)
			}
			got, _ := args[tt.outputKey].(string)
			if tt.wantFile {
				if !strings.HasPrefix(got, staged.outputVirtual+"/") {
					t.Fatalf("output path was not forced into call lane: %q", got)
				}
			} else if got != staged.outputVirtual {
				t.Fatalf("output directory was not forced into call lane: %q", got)
			}
			if strings.Contains(got, "deeix-999-999") {
				t.Fatalf("caller-controlled output scope survived: %q", got)
			}

			forged := map[string]any{
				"nested": map[string]any{tt.outputKey: "/shared/deeix-999-999/stolen.png"},
			}
			if _, err := p.stage(tt.tool, forged, meta{UserID: 4, ConversationID: 9, RequestID: "r1", ToolCallID: tt.tool + "-forged"}); err == nil {
				t.Fatal("nested caller-controlled output scope was accepted")
			}
		})
	}
}

func TestCollectProducerOutputsRejectsUnsafeAndOversizedFiles(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, outputDir string)
	}{
		{
			name: "symlink",
			setup: func(t *testing.T, outputDir string) {
				outside := filepath.Join(filepath.Dir(outputDir), "outside.png")
				if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Join(outputDir, "link.png")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			},
		},
		{
			name: "per-file-limit",
			setup: func(t *testing.T, outputDir string) {
				writeSparseFile(t, filepath.Join(outputDir, "large.png"), maxOutput+1)
			},
		},
		{
			name: "total-limit",
			setup: func(t *testing.T, outputDir string) {
				for i := 0; i < 4; i++ {
					writeSparseFile(t, filepath.Join(outputDir, fmt.Sprintf("part-%d.png", i)), 17<<20)
				}
			},
		},
		{
			name: "file-count-limit",
			setup: func(t *testing.T, outputDir string) {
				for i := 0; i <= maxOutputFiles; i++ {
					if err := os.WriteFile(filepath.Join(outputDir, fmt.Sprintf("part-%d.png", i)), []byte("x"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			p := &proxy{staging: filepath.Join(root, "staging"), output: filepath.Join(root, "shared")}
			if err := os.MkdirAll(p.output, 0755); err != nil {
				t.Fatal(err)
			}
			staged, err := p.stage("save_view", map[string]any{}, meta{UserID: 4, ConversationID: 9, RequestID: "r1", ToolCallID: tt.name})
			if err != nil {
				t.Fatal(err)
			}
			tt.setup(t, staged.outputDir)
			if _, _, _, err := p.collectProducerOutputs(staged); err == nil {
				t.Fatal("unsafe producer output was accepted")
			}
			if files := regularFileContents(t, p.output); len(files) != 0 {
				t.Fatalf("partial output was published: %v", files)
			}
		})
	}
}

func TestCollectProducerOutputsRejectsSymlinkScope(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "shared")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(output, "deeix-4-9")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	p := &proxy{staging: filepath.Join(root, "staging"), output: output}
	staged, err := p.stage("crop", map[string]any{}, meta{UserID: 4, ConversationID: 9, RequestID: "r1", ToolCallID: "scope-link"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged.outputDir, "crop.png"), []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := p.collectProducerOutputs(staged); err == nil {
		t.Fatal("symlink output scope was accepted")
	}
	if files := regularFileContents(t, outside); len(files) != 0 {
		t.Fatalf("output escaped through scope symlink: %v", files)
	}
}

func TestRewriteMCPResponseJSONAndSSE(t *testing.T) {
	oldDir := "/shared/deeix-4-9/call/outputs"
	oldPath := oldDir + "/view.png"
	newDir := "/shared/deeix-4-9/mm-result"
	newPath := newDir + "/view-123.png"
	exports := []exportItem{{Path: newPath, Name: "view.png"}}
	jsonBody := []byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Saved to: ` + oldPath + ` in ` + oldDir + `"}],"structured":{"path":"` + oldPath + `"}}}`)

	tests := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{name: "json", contentType: "application/json", body: jsonBody},
		{name: "sse", contentType: "text/event-stream", body: []byte("event: message\ndata: " + string(jsonBody) + "\n\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewritten, err := rewriteMCPResponse(tt.contentType, tt.body, map[string]string{
				oldPath: newPath,
				oldDir:  newDir,
			}, exports)
			if err != nil {
				t.Fatal(err)
			}
			text := string(rewritten)
			if strings.Contains(text, oldDir) || !strings.Contains(text, newDir) || !strings.Contains(text, newPath) {
				t.Fatalf("internal path was not rewritten: %s", text)
			}
			if !strings.Contains(text, `"__export__"`) || !strings.Contains(text, `"name":"view.png"`) {
				t.Fatalf("export metadata missing: %s", text)
			}
		})
	}
}

func TestProxyPublishesProducerOutputAndCleansStaging(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	staging := filepath.Join(root, "staging")
	scopeDir := filepath.Join(shared, "deeix-4-9")
	if err := os.MkdirAll(scopeDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scopeDir, "input.png"), []byte("input"), 0600); err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		outputPath, _ := request.Params.Arguments["output_path"].(string)
		if !strings.HasPrefix(outputPath, "/shared/deeix-4-9/") || strings.Contains(outputPath, "outside") {
			http.Error(w, "output path not forced", http.StatusBadRequest)
			return
		}
		hostPath := filepath.Join(staging, filepath.FromSlash(strings.TrimPrefix(outputPath, "/shared/")))
		if err := os.WriteFile(hostPath, []byte("generated"), 0644); err != nil {
			http.Error(w, "write output", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"Saved to: ` + outputPath + `"}]}}`))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, shared, staging)
	body := producerToolCallBody("crop", "call-producer", map[string]any{
		"image_path":  "/shared/deeix-4-9/input.png",
		"output_path": "/shared/deeix-4-9/outside/stolen.png",
	})
	recorder := httptest.NewRecorder()
	p.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("proxy returned %d: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			Export []exportItem `json:"__export__"`
		} `json:"result"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Result.Export) != 1 {
		t.Fatalf("unexpected exports: %#v", response.Result.Export)
	}
	exported := response.Result.Export[0]
	if strings.Contains(exported.Path, "/outputs/") || !strings.HasPrefix(exported.Path, "/shared/deeix-4-9/mm-") {
		t.Fatalf("unexpected exported path: %q", exported.Path)
	}
	if len(response.Result.Content) != 1 || strings.Contains(response.Result.Content[0].Text, "/outputs/") || !strings.Contains(response.Result.Content[0].Text, exported.Path) {
		t.Fatalf("response retained internal path: %#v", response.Result.Content)
	}
	publishedPath := filepath.Join(shared, filepath.FromSlash(strings.TrimPrefix(exported.Path, "/shared/")))
	data, err := os.ReadFile(publishedPath)
	if err != nil || string(data) != "generated" {
		t.Fatalf("published output: %q %v", data, err)
	}
	assertDirectoryEmpty(t, staging)
}

func TestProxyDiscardsPublishedOutputWhenResponseCannotBeRewritten(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	staging := filepath.Join(root, "staging")
	if err := os.MkdirAll(shared, 0755); err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Params struct {
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		outputPath, _ := request.Params.Arguments["output_path"].(string)
		hostPath := filepath.Join(staging, filepath.FromSlash(strings.TrimPrefix(outputPath, "/shared/")))
		if err := os.WriteFile(hostPath, []byte("generated"), 0644); err != nil {
			http.Error(w, "write output", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-json"))
	}))
	defer upstream.Close()

	p := newTestProxy(t, upstream.URL, shared, staging)
	body := producerToolCallBody("draw_bbox", "call-invalid-response", map[string]any{})
	recorder := httptest.NewRecorder()
	p.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body)))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("proxy returned %d: %s", recorder.Code, recorder.Body.String())
	}
	published := filepath.Join(shared, "deeix-4-9", producerPublishedName(stagedCall{laneName: hashedName("call-invalid-response", "call")}))
	if _, err := os.Stat(published); !os.IsNotExist(err) {
		t.Fatalf("failed response retained published output: %v", err)
	}
	assertDirectoryEmpty(t, staging)
}

func TestHealthAndReadinessEndpoints(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	p := newTestProxy(t, upstream.URL, t.TempDir(), t.TempDir())

	for _, path := range []string{"/healthz", "/readyz"} {
		recorder := httptest.NewRecorder()
		p.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, recorder.Code, recorder.Body.String())
		}
	}
	upstream.Close()
	recorder := httptest.NewRecorder()
	p.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable upstream readiness returned %d", recorder.Code)
	}
}

func newTestProxy(t *testing.T, upstreamURL, shared, staging string) *proxy {
	t.Helper()
	u, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatal(err)
	}
	p := &proxy{
		upstream: u,
		shared:   shared,
		imports:  filepath.Join(filepath.Dir(shared), "imports"),
		staging:  staging,
		output:   shared,
		key:      []byte("secret"),
		replays:  replayCache{values: map[string]time.Time{}},
	}
	p.transport = &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(u) }}
	return p
}

func producerToolCallBody(toolName, callID string, arguments map[string]any) []byte {
	envelope := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name": toolName, "arguments": arguments,
			"_meta": signedMeta(4, 9, "request", callID, time.Now().Unix()),
		},
	}
	body, _ := json.Marshal(envelope)
	return body
}

func writeSparseFile(t *testing.T, filePath string, size int64) {
	t.Helper()
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(size); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertDirectoryEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory not empty: %v", entries)
	}
}

func TestNonPathStringsRemainUntouched(t *testing.T) {
	args := map[string]any{"prompt": "a/path/string", "data": "YWJj"}
	raw, _ := json.Marshal(args)
	if string(raw) == "" {
		t.Fatal("marshal failed")
	}
	if isPathValue("prompt", "a/path/string") || isPathValue("data", "YWJj") {
		t.Fatal("non-path value classified as path")
	}
	if !isPathValue("image_path", "/shared-other/file.png") {
		t.Fatal("absolute path argument was not classified for rejection")
	}
	p := &proxy{shared: t.TempDir(), imports: t.TempDir(), staging: t.TempDir()}
	if _, err := p.stage(
		"inspect",
		map[string]any{"image_path": "/shared-other/file.png"},
		meta{UserID: 4, ConversationID: 9, RequestID: "r1", ToolCallID: "c1"},
	); err == nil {
		t.Fatal("lookalike virtual root path accepted")
	}
}
