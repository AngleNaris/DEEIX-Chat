package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxBody        = 32 << 20
	maxInput       = 64 << 20
	maxOutput      = 20 << 20
	maxOutputTotal = 64 << 20
	maxOutputFiles = 32
	replayWindow   = 5 * time.Minute
	// defaultMMUpstreamTimeout 单次 tools/call 上游调用的超时上限：防止 hung 住的
	// 上游无限期占用全局 callMu 锁，阻塞后续所有多媒体调用。
	defaultMMUpstreamTimeout = 10 * time.Minute
)

var scopeRE = regexp.MustCompile(`^deeix-[0-9]+-[0-9]+$`)

type stagedCall struct {
	scope          string
	laneName       string
	lane           string
	outputDir      string
	outputVirtual  string
	producerOutput bool
}

type exportItem struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type meta struct {
	UserID         uint
	ConversationID uint
	RequestID      string
	ToolCallID     string
}
type replayCache struct {
	sync.Mutex
	values map[string]time.Time
}
type proxy struct {
	upstream                         *url.URL
	shared, imports, staging, output string
	key                              []byte
	replays                          replayCache
	callMu                           sync.Mutex
	upstreamTimeout                  time.Duration
	transport                        http.Handler
}

type envelope struct {
	Method string `json:"method"`
	Params struct {
		Name      string         `json:"name"`
		Meta      map[string]any `json:"_meta"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

func main() {
	p, err := newProxy()
	if err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}
	addr := env("MM_ISOLATION_ADDR", "0.0.0.0:8082")
	slog.Info("mm isolation proxy listening", "addr", addr, "upstream", p.upstream.String())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: addr, Handler: p, ReadHeaderTimeout: 10 * time.Second}
	go p.startSweeper(ctx)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server exited", "error", err)
		os.Exit(1)
	}
}
func newProxy() (*proxy, error) {
	u, err := url.Parse(env("MM_UPSTREAM", "http://mm-upstream:8082"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid MM_UPSTREAM")
	}
	key := strings.TrimSpace(os.Getenv("MM_META_HMAC_KEY"))
	if key == "" {
		return nil, fmt.Errorf("MM_META_HMAC_KEY is required")
	}
	p := &proxy{
		upstream: u,
		shared:   filepath.Clean(env("MM_SOURCE_SHARED", "/input")),
		imports:  filepath.Clean(env("MM_SOURCE_IMPORTS", "/imports")),
		staging:  filepath.Clean(env("MM_STAGING_ROOT", "/staging")),
		output:   filepath.Clean(strings.TrimSpace(os.Getenv("MM_OUTPUT_SHARED"))),
		key:      []byte(key),
		replays:  replayCache{values: map[string]time.Time{}},

		upstreamTimeout: durationEnv("MM_UPSTREAM_TIMEOUT_SEC", defaultMMUpstreamTimeout),
	}
	if p.shared == "." || p.imports == "." || p.staging == "." {
		return nil, fmt.Errorf("invalid filesystem roots")
	}
	p.transport = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) { r.SetURL(u); r.Out.Host = u.Host },
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ResponseHeaderTimeout: p.upstreamTimeout,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConnsPerHost:   4,
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "upstream timed out", http.StatusGatewayTimeout)
				return
			}
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			slog.Warn("MM upstream failed", "error", err)
		},
	}
	return p, nil
}
func (p *proxy) startSweeper(ctx context.Context) {
	if p.output == "" || p.output == "." {
		return
	}
	ttl := durationEnv("MM_OUTPUT_TTL_SEC", defaultMMSweepTTL)
	interval := durationEnv("MM_OUTPUT_SWEEP_INTERVAL_SEC", defaultMMSweepInterval)
	if ttl <= 0 || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			p.callMu.Lock()
			if err := sweepMMOutputs(p.output, now, ttl); err != nil {
				slog.Warn("sweep MM outputs", "err", err)
			}
			p.callMu.Unlock()
		}
	}
}
func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	if r.URL.Path == "/readyz" {
		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, p.upstream.String(), nil)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Do(request)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = response.Body.Close()
		if response.StatusCode >= http.StatusInternalServerError {
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		p.transport.ServeHTTP(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	var e envelope
	if err := json.Unmarshal(body, &e); err != nil {
		http.Error(w, "invalid JSON-RPC body", http.StatusBadRequest)
		return
	}
	if e.Method == "tools/call" {
		m, err := parseMeta(e.Params.Meta, p.key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		replayKey := fmt.Sprintf("%d/%d/%s/%s", m.UserID, m.ConversationID, m.RequestID, m.ToolCallID)
		if !p.replays.accept(replayKey, time.Now()) {
			http.Error(w, "replayed request", http.StatusConflict)
			return
		}

		// The upstream has one staging mount. Serialize file-bearing calls so that
		// only the current signed call is visible through that mount.
		p.callMu.Lock()
		defer p.callMu.Unlock()
		if err := p.resetStaging(); err != nil {
			http.Error(w, "staging is unavailable", http.StatusInternalServerError)
			return
		}
		defer func() {
			if err := p.resetStaging(); err != nil {
				slog.Warn("failed to clean MM staging", "error", err)
			}
		}()
		staged, err := p.stage(e.Params.Name, e.Params.Arguments, *m)
		if err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		var object map[string]any
		if err := json.Unmarshal(body, &object); err != nil {
			http.Error(w, "invalid JSON-RPC body", http.StatusBadRequest)
			return
		}
		params, ok := object["params"].(map[string]any)
		if !ok {
			http.Error(w, "invalid JSON-RPC params", http.StatusBadRequest)
			return
		}
		params["arguments"] = e.Params.Arguments
		body, err = json.Marshal(object)
		if err != nil {
			http.Error(w, "invalid JSON-RPC body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Del("Accept-Encoding")

		// Bound the upstream call so a hung upstream cannot hold callMu forever.
		if p.upstreamTimeout > 0 {
			ctx, cancel := context.WithTimeout(r.Context(), p.upstreamTimeout)
			defer cancel()
			r = r.WithContext(ctx)
		}

		recorder := httptest.NewRecorder()
		p.transport.ServeHTTP(recorder, r)
		response := recorder.Result()
		defer response.Body.Close() //nolint:errcheck
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
		if readErr != nil || len(responseBody) > maxBody {
			http.Error(w, "upstream response too large", http.StatusBadGateway)
			return
		}
		if staged.producerOutput && response.StatusCode >= 200 && response.StatusCode < 300 {
			exports, replacements, publishedDir, collectErr := p.collectProducerOutputs(staged)
			if collectErr != nil {
				http.Error(w, "invalid MM output", http.StatusBadGateway)
				return
			}
			if len(exports) > 0 {
				responseBody, err = rewriteMCPResponse(response.Header.Get("Content-Type"), responseBody, replacements, exports)
				if err != nil {
					if removeErr := os.RemoveAll(publishedDir); removeErr != nil {
						slog.Warn("failed to discard MM output", "error", removeErr)
					}
					http.Error(w, "invalid MM response", http.StatusBadGateway)
					return
				}
			}
		}
		copyResponse(w, response.Header, response.StatusCode, responseBody)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	p.transport.ServeHTTP(w, r)
}
func parseMeta(raw map[string]any, key []byte) (*meta, error) {
	uid, ok := uintValue(raw["user_id"])
	if !ok || uid == 0 {
		return nil, fmt.Errorf("invalid signed user_id")
	}
	cid, ok := uintValue(raw["conversation_id"])
	if !ok || cid == 0 {
		return nil, fmt.Errorf("invalid signed conversation_id")
	}
	rid, _ := raw["request_id"].(string)
	rid = strings.TrimSpace(rid)
	if rid == "" {
		return nil, fmt.Errorf("invalid signed request_id")
	}
	callID, _ := raw["call_id"].(string)
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil, fmt.Errorf("invalid signed call_id")
	}
	ts, ok := intValue(raw["ts"])
	if !ok {
		return nil, fmt.Errorf("invalid signed timestamp")
	}
	drift := time.Since(time.Unix(ts, 0))
	if drift > replayWindow || drift < -replayWindow {
		return nil, fmt.Errorf("expired signed metadata")
	}
	canonical := fmt.Sprintf("user_id=%d\nconversation_id=%d\nrequest_id=%s\ncall_id=%s\nts=%d", uid, cid, rid, callID, ts)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))
	sig, _ := raw["sig"].(string)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return nil, fmt.Errorf("invalid signed metadata")
	}
	return &meta{UserID: uid, ConversationID: cid, RequestID: rid, ToolCallID: callID}, nil
}

func (p *proxy) resetStaging() error {
	if err := os.MkdirAll(p.staging, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(p.staging)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(p.staging, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func (p *proxy) stage(toolName string, args map[string]any, m meta) (stagedCall, error) {
	scope := fmt.Sprintf("deeix-%d-%d", m.UserID, m.ConversationID)
	if !scopeRE.MatchString(scope) {
		return stagedCall{}, fmt.Errorf("invalid scope")
	}
	laneName := hashedName(m.ToolCallID, "call")
	lane := filepath.Join(p.staging, scope, laneName)
	if err := os.MkdirAll(lane, 0755); err != nil {
		return stagedCall{}, err
	}
	result := stagedCall{scope: scope, laneName: laneName, lane: lane}
	if isProducerTool(toolName) {
		if p.output == "" || p.output == "." {
			return stagedCall{}, fmt.Errorf("MM producer output is not configured")
		}
		result.outputDir = filepath.Join(lane, "outputs")
		result.outputVirtual = "/shared/" + path.Join(scope, laneName, "outputs")
		result.producerOutput = true
		if err := os.MkdirAll(result.outputDir, 0777); err != nil {
			return stagedCall{}, err
		}
		if toolName == "save_view" {
			args["output_dir"] = result.outputVirtual
		} else {
			args["output_path"] = result.outputVirtual + "/" + producerOutputName(toolName, args)
		}
	}
	rewritten := make(map[string]string)
	if err := walkValues(args, "", func(key, value string) (string, error) {
		if isProducerOutputArgument(toolName, key) &&
			(value == result.outputVirtual || strings.HasPrefix(value, result.outputVirtual+"/")) {
			return value, nil
		}
		if staged, ok := rewritten[value]; ok {
			return staged, nil
		}
		staged, err := p.stagePath(key, value, scope, lane, laneName)
		if err == nil {
			rewritten[value] = staged
		}
		return staged, err
	}); err != nil {
		return stagedCall{}, err
	}
	return result, nil
}

func isProducerTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "crop", "draw_bbox", "save_view":
		return true
	default:
		return false
	}
}

func isProducerOutputArgument(toolName, key string) bool {
	if toolName == "save_view" {
		return key == "output_dir"
	}
	return (toolName == "crop" || toolName == "draw_bbox") && key == "output_path"
}

func producerOutputName(toolName string, args map[string]any) string {
	name := toolName + ".png"
	if rawPath, ok := args["image_path"].(string); ok {
		base := safeName(filepath.Base(rawPath))
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		if stem != "" && stem != "request" {
			name = stem + "_" + toolName + ".png"
		}
	}
	return name
}

func (p *proxy) collectProducerOutputs(call stagedCall) ([]exportItem, map[string]string, string, error) {
	entries := make([]string, 0)
	var expectedTotal int64
	err := filepath.WalkDir(call.outputDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == call.outputDir {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink output is not allowed")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("output is not a regular file")
		}
		if info.Size() <= 0 || info.Size() > maxOutput {
			return fmt.Errorf("output is empty or too large")
		}
		expectedTotal += info.Size()
		if expectedTotal > maxOutputTotal {
			return fmt.Errorf("total output is too large")
		}
		entries = append(entries, filePath)
		if len(entries) > maxOutputFiles {
			return fmt.Errorf("too many output files")
		}
		return nil
	})
	if err != nil {
		return nil, nil, "", err
	}
	if len(entries) == 0 {
		return nil, nil, "", nil
	}

	publishedName := producerPublishedName(call)
	published, err := createScopedOutputDir(p.output, call.scope, publishedName)
	if err != nil {
		return nil, nil, "", err
	}
	defer published.Cleanup()

	exports := make([]exportItem, 0, len(entries))
	publishedVirtual := "/shared/" + path.Join(call.scope, publishedName)
	replacements := map[string]string{call.outputVirtual: publishedVirtual}
	var total int64
	for _, sourcePath := range entries {
		rel, relErr := filepath.Rel(p.staging, sourcePath)
		if relErr != nil || !isRelativePath(rel) {
			return nil, nil, "", fmt.Errorf("output is outside staging")
		}
		source, size, openErr := openScopedRegularFile(p.staging, rel)
		if openErr != nil {
			return nil, nil, "", openErr
		}
		if size <= 0 || size > maxOutput || total+size > maxOutputTotal {
			_ = source.Close()
			return nil, nil, "", fmt.Errorf("output is empty or too large")
		}
		total += size
		outputRel, relErr := filepath.Rel(call.outputDir, sourcePath)
		if relErr != nil || !isRelativePath(outputRel) {
			_ = source.Close()
			return nil, nil, "", fmt.Errorf("output is outside call lane")
		}
		destinationName := hashedFileName(safeName(filepath.Base(sourcePath)), filepath.ToSlash(outputRel))
		output, createErr := published.CreateFile(destinationName)
		if createErr != nil {
			_ = source.Close()
			return nil, nil, "", createErr
		}
		copied, copyErr := io.Copy(output, io.LimitReader(source, maxOutput+1))
		sourceCloseErr := source.Close()
		outputCloseErr := output.Close()
		if copyErr != nil {
			return nil, nil, "", copyErr
		}
		if copied != size {
			return nil, nil, "", fmt.Errorf("output changed during collection")
		}
		if sourceCloseErr != nil {
			return nil, nil, "", sourceCloseErr
		}
		if outputCloseErr != nil {
			return nil, nil, "", outputCloseErr
		}
		publishedPath := publishedVirtual + "/" + destinationName
		stagingPath := call.outputVirtual + "/" + filepath.ToSlash(outputRel)
		replacements[stagingPath] = publishedPath
		exports = append(exports, exportItem{Path: publishedPath, Name: filepath.Base(sourcePath)})
	}
	finalDir, err := published.Commit(publishedName)
	if err != nil {
		return nil, nil, "", err
	}
	return exports, replacements, finalDir, nil
}

func producerPublishedName(call stagedCall) string {
	return "mm-" + hashedName(call.laneName, "output")
}

func rewriteMCPResponse(contentType string, body []byte, replacements map[string]string, exports []exportItem) ([]byte, error) {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if strings.EqualFold(mediaType, "text/event-stream") {
		return rewriteSSEBody(body, replacements, exports)
	}
	return rewriteJSONRPCBody(body, replacements, exports)
}

func rewriteJSONRPCBody(body []byte, replacements map[string]string, exports []exportItem) ([]byte, error) {
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing JSON-RPC result")
	}
	rewriteStringValues(result, replacements)
	result["__export__"] = exports
	return json.Marshal(response)
}

func rewriteSSEBody(body []byte, replacements map[string]string, exports []exportItem) ([]byte, error) {
	lines := strings.Split(string(body), "\n")
	rewritten := false
	for index, line := range lines {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		prefix := "data:"
		payload := strings.TrimPrefix(line, prefix)
		space := ""
		if strings.HasPrefix(payload, " ") {
			space = " "
			payload = strings.TrimPrefix(payload, " ")
		}
		updated, err := rewriteJSONRPCBody([]byte(payload), replacements, exports)
		if err != nil {
			continue
		}
		lines[index] = prefix + space + string(updated)
		rewritten = true
		break
	}
	if !rewritten {
		return nil, fmt.Errorf("missing JSON-RPC SSE data")
	}
	return []byte(strings.Join(lines, "\n")), nil
}

func rewriteStringValues(value any, replacements map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if text, ok := child.(string); ok {
				typed[key] = replacePaths(text, replacements)
				continue
			}
			rewriteStringValues(child, replacements)
		}
	case []any:
		for index, child := range typed {
			if text, ok := child.(string); ok {
				typed[index] = replacePaths(text, replacements)
				continue
			}
			rewriteStringValues(child, replacements)
		}
	}
}

func replacePaths(text string, replacements map[string]string) string {
	paths := make([]string, 0, len(replacements))
	for oldPath := range replacements {
		paths = append(paths, oldPath)
	}
	sort.Slice(paths, func(i, j int) bool {
		return len(paths[i]) > len(paths[j])
	})
	for _, oldPath := range paths {
		text = strings.ReplaceAll(text, oldPath, replacements[oldPath])
	}
	return text
}

func copyResponse(w http.ResponseWriter, header http.Header, status int, body []byte) {
	for key, values := range header {
		if strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (p *proxy) stagePath(key, value, scope, lane, laneName string) (string, error) {
	root, rel, err := p.resolveSourcePath(value, scope)
	if err != nil {
		return "", err
	}
	src, size, err := openScopedRegularFile(root, rel)
	if err != nil {
		return "", fmt.Errorf("input is unavailable: %w", err)
	}
	defer src.Close()
	if size > maxInput {
		return "", fmt.Errorf("input is unavailable or too large")
	}

	base := safeName(filepath.Base(rel))
	if base == "request" {
		base = "input"
	}
	dstName := hashedFileName(base, filepath.ToSlash(rel))
	dst := filepath.Join(lane, dstName)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	copied, copyErr := io.Copy(out, io.LimitReader(src, maxInput+1))
	closeErr := out.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if copied > maxInput {
		_ = os.Remove(dst)
		return "", fmt.Errorf("input is unavailable or too large")
	}
	return "/shared/" + path.Join(scope, laneName, dstName), nil
}

func (p *proxy) resolveSourcePath(value, scope string) (string, string, error) {
	clean := filepath.Clean(value)
	var root, rel string
	switch {
	case hasVirtualRoot(clean, "/shared"):
		root = p.shared
		rel = strings.TrimPrefix(filepath.ToSlash(clean), "/shared/")
	case hasVirtualRoot(clean, "/imports"):
		root = p.imports
		rel = strings.TrimPrefix(filepath.ToSlash(clean), "/imports/")
	default:
		for _, candidate := range []string{p.shared, p.imports} {
			candidateRel, relErr := filepath.Rel(candidate, clean)
			if relErr == nil && isRelativePath(candidateRel) {
				root = candidate
				rel = candidateRel
				break
			}
		}
	}
	if root == "" {
		return "", "", fmt.Errorf("path is outside allowed input roots")
	}
	rel = filepath.Clean(filepath.FromSlash(rel))
	scopePrefix := scope + string(filepath.Separator)
	if !strings.HasPrefix(rel, scopePrefix) {
		return "", "", fmt.Errorf("path is outside signed scope")
	}
	rel = strings.TrimPrefix(rel, scopePrefix)
	if !isRelativePath(rel) || rel == "." {
		return "", "", fmt.Errorf("path is outside signed scope")
	}
	return root, filepath.Join(scope, rel), nil
}

func hasVirtualRoot(value, root string) bool {
	return value == root || strings.HasPrefix(value, root+string(filepath.Separator))
}

func isRelativePath(value string) bool {
	return value != ".." && !filepath.IsAbs(value) && !strings.HasPrefix(value, ".."+string(filepath.Separator))
}

func walkValues(v any, key string, rewrite func(string, string) (string, error)) error {
	switch x := v.(type) {
	case map[string]any:
		for childKey, item := range x {
			if s, ok := item.(string); ok && isPathValue(childKey, s) {
				next, err := rewrite(childKey, s)
				if err != nil {
					return err
				}
				x[childKey] = next
			} else if err := walkValues(item, childKey, rewrite); err != nil {
				return err
			}
		}
	case []any:
		for i, item := range x {
			if s, ok := item.(string); ok && isPathValue(key, s) {
				next, err := rewrite(key, s)
				if err != nil {
					return err
				}
				x[i] = next
			} else if err := walkValues(item, key, rewrite); err != nil {
				return err
			}
		}
	}
	return nil
}
func isPathValue(key, value string) bool {
	k := strings.ToLower(key)
	v := filepath.Clean(value)
	return hasVirtualRoot(v, "/shared") || hasVirtualRoot(v, "/imports") ||
		(strings.HasPrefix(v, string(filepath.Separator)) && (strings.Contains(k, "path") || strings.HasSuffix(k, "file") || strings.HasSuffix(k, "filename")))
}
func safeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "request"
	}
	if b.Len() > 48 {
		return b.String()[:48]
	}
	return b.String()
}

func hashedName(value, fallback string) string {
	name := safeName(value)
	if name == "request" {
		name = fallback
	}
	sum := sha256.Sum256([]byte(value))
	return name + "-" + hex.EncodeToString(sum[:6])
}

func hashedFileName(base, identity string) string {
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem = "input"
	}
	sum := sha256.Sum256([]byte(identity))
	return stem + "-" + hex.EncodeToString(sum[:6]) + ext
}
func uintValue(v any) (uint, bool) {
	switch n := v.(type) {
	case uint:
		return n, n > 0
	case int:
		return uint(n), n > 0
	case float64:
		return uint(n), n > 0 && n == float64(uint(n))
	case string:
		x, e := strconv.ParseUint(strings.TrimSpace(n), 10, 64)
		return uint(x), e == nil && x > 0
	}
	return 0, false
}
func intValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case uint:
		return int64(n), true
	case float64:
		return int64(n), n == float64(int64(n))
	case string:
		x, e := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return x, e == nil
	}
	return 0, false
}
func (c *replayCache) accept(key string, now time.Time) bool {
	c.Lock()
	defer c.Unlock()
	for k, t := range c.values {
		if now.Sub(t) > replayWindow {
			delete(c.values, k)
		}
	}
	if _, ok := c.values[key]; ok {
		return false
	}
	c.values[key] = now
	return true
}
func durationEnv(key string, fallback time.Duration) time.Duration {
	if raw := strings.TrimSpace(os.Getenv(key)); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return fallback
}
func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}
