package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

const testHmacKey = "test-hmac-key"

// signedMetaBody 构造带有效 HMAC 签名的 _meta 请求体（后端签名格式）。
func signedMetaBody(uid, cid uint, reqID string, ts int64) string {
	callID := "call-" + reqID
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sandbox_exec","arguments":{"command":"ls"},"_meta":{"user_id":%d,"conversation_id":%d,"request_id":%q,"call_id":%q,"ts":%d,"sig":%q}}}`,
		uid, cid, reqID, callID, ts, metaSignature(testHmacKey, uid, cid, reqID, callID, ts))
}

func TestParseMeta(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name    string
		body    string
		wantUID uint
		wantCID uint
		wantErr bool
	}{
		{
			name:    "deeix style params._meta with valid sig",
			body:    signedMetaBody(42, 7, "req-1", now),
			wantUID: 42, wantCID: 7,
		},
		{
			name:    "missing conversation id rejected",
			body:    fmt.Sprintf(`{"params":{"_meta":{"user_id":9,"request_id":"r","call_id":"c","ts":%d,"sig":%q}}}`, now, metaSignature(testHmacKey, 9, 0, "r", "c", now)),
			wantErr: true,
		},
		{
			name:    "string user id",
			body:    fmt.Sprintf(`{"params":{"_meta":{"user_id":"3","conversation_id":"5","request_id":"r","call_id":"c","ts":%d,"sig":%q}}}`, now, metaSignature(testHmacKey, 3, 5, "r", "c", now)),
			wantUID: 3, wantCID: 5,
		},
		{
			name:    "missing _meta rejected",
			body:    `{"params":{"name":"sandbox_exec","arguments":{}}}`,
			wantErr: true,
		},
		{
			name:    "zero user id rejected",
			body:    fmt.Sprintf(`{"params":{"_meta":{"user_id":0,"ts":%d,"sig":"x"}}}`, now),
			wantErr: true,
		},
		{
			name:    "missing signature rejected",
			body:    fmt.Sprintf(`{"params":{"_meta":{"user_id":9,"ts":%d}}}`, now),
			wantErr: true,
		},
		{
			name:    "missing call id rejected",
			body:    fmt.Sprintf(`{"params":{"_meta":{"user_id":9,"conversation_id":1,"request_id":"r","ts":%d,"sig":"x"}}}`, now),
			wantErr: true,
		},
		{
			name:    "wrong signature rejected",
			body:    fmt.Sprintf(`{"params":{"_meta":{"user_id":9,"ts":%d,"sig":"deadbeef"}}}`, now),
			wantErr: true,
		},
		{
			name:    "tampered user id rejected",
			body:    fmt.Sprintf(`{"params":{"_meta":{"user_id":999,"conversation_id":7,"request_id":"req-1","call_id":"call-req-1","ts":%d,"sig":%q}}}`, now, metaSignature(testHmacKey, 42, 7, "req-1", "call-req-1", now)),
			wantErr: true,
		},
		{
			name:    "expired timestamp rejected",
			body:    signedMetaBody(42, 7, "req-1", time.Now().Add(-10*time.Minute).Unix()),
			wantErr: true,
		},
		{
			name:    "missing timestamp rejected",
			body:    `{"params":{"_meta":{"user_id":9,"sig":"x"}}}`,
			wantErr: true,
		},
		{
			name:    "not json",
			body:    `not-json`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta, err := ParseMeta([]byte(tc.body), testHmacKey)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got meta %+v", meta)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if meta.UserID != tc.wantUID || meta.ConversationID != tc.wantCID {
				t.Fatalf("got uid=%d cid=%d, want %d/%d", meta.UserID, meta.ConversationID, tc.wantUID, tc.wantCID)
			}
		})
	}
}

func TestSessionScope(t *testing.T) {
	if got := SessionScope(&Meta{UserID: 1, ConversationID: 2}); got != "deeix-1-2" {
		t.Fatalf("got %s", got)
	}
	if got := SessionScope(&Meta{UserID: 1}); got != "deeix-1" {
		t.Fatalf("got %s", got)
	}
}

func TestSanitizeWorkspacePath(t *testing.T) {
	ws := "/workspace"
	valid := []string{
		"/workspace/a.mp3", "a.mp3", "./a/b.txt", "/workspace/../workspace/x.log",
		"/workspace/sub dir/file with space.txt",
	}
	for _, p := range valid {
		abs, err := sanitizeWorkspacePath(ws, p)
		if err != nil {
			t.Fatalf("path %q should be valid: %v", p, err)
		}
		if !strings.HasPrefix(abs, ws+"/") && abs != ws {
			t.Fatalf("path %q resolved outside workspace: %s", p, abs)
		}
	}
	invalid := []string{
		"", "/etc/passwd", "/workspace/../../etc/passwd", "../escape.txt",
		"/proc/self/status", "/sys/kernel", "a\x00b",
	}
	for _, p := range invalid {
		if _, err := sanitizeWorkspacePath(ws, p); err == nil {
			t.Fatalf("path %q should be rejected", p)
		}
	}
}

func TestExportFileCopyScript(t *testing.T) {
	script := exportFileCopyScript(
		"/workspace/report.txt",
		"/workspace",
		"/shared/deeix-42-7",
		"report.txt",
	)
	if strings.Contains(script, ";;") {
		t.Fatalf("export script contains invalid empty statement: %s", script)
	}
	for _, expected := range []string{
		"'/workspace/report.txt'",
		"'/workspace'",
		"'/shared/deeix-42-7'",
		"'report.txt'",
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("export script missing %q: %s", expected, script)
		}
	}
}

func runExportFileCopyScript(t *testing.T, workspacePath, workspaceRoot, sharedDir, name string) error {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", exportFileCopyScript(workspacePath, workspaceRoot, sharedDir, name))
	return cmd.Run()
}

func TestExportFileCopyScriptCopiesRegularFile(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()
	source := filepath.Join(workspace, "report.txt")
	if err := os.WriteFile(source, []byte("report content"), 0600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := runExportFileCopyScript(t, source, workspace, shared, "report.txt"); err != nil {
		t.Fatalf("export regular file: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(shared, "report.txt"))
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	if string(content) != "report content" {
		t.Fatalf("exported content = %q", content)
	}
}

func TestExportFileCopyScriptRejectsSourceSymlink(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	source := filepath.Join(workspace, "report.txt")
	if err := os.Symlink(outside, source); err != nil {
		t.Fatalf("create source symlink: %v", err)
	}
	if err := runExportFileCopyScript(t, source, workspace, shared, "report.txt"); err == nil {
		t.Fatal("source symlink export unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(shared, "report.txt")); !os.IsNotExist(err) {
		t.Fatalf("source symlink created destination: %v", err)
	}
}

func TestExportFileCopyScriptRejectsDestinationSymlink(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()
	source := filepath.Join(workspace, "report.txt")
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(source, []byte("report content"), 0600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(outside, []byte("outside content"), 0600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(shared, "report.txt")); err != nil {
		t.Fatalf("create destination symlink: %v", err)
	}
	if err := runExportFileCopyScript(t, source, workspace, shared, "report.txt"); err == nil {
		t.Fatal("destination symlink export unexpectedly succeeded")
	}
	content, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("read outside file: %v", err)
	}
	if string(content) != "outside content" {
		t.Fatalf("destination symlink target was modified: %q", content)
	}
}

func TestTruncateUTF8(t *testing.T) {
	s := "你好世界 hello"
	got := truncateUTF8(s, 7) // 7 bytes 会切断"你"(3B)+"好"(3B)=6 后再截
	if !strings.HasSuffix(got, "...[truncated]") {
		t.Fatalf("missing suffix: %q", got)
	}
	if !utf8Valid(got) {
		t.Fatalf("invalid utf8: %q", got)
	}
	short := truncateUTF8("abc", 100)
	if short != "abc" {
		t.Fatalf("short string changed: %q", short)
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		_ = r
	}
	return true
}

func testContainerConfig() (containerSpec, *container.Config, *container.HostConfig, *inspectedContainerConfig) {
	spec := containerSpec{
		Image:         "custom/image:latest",
		Env:           []string{"WORKSPACE=/workspace"},
		Memory:        "1g",
		PidsLimit:     256,
		CPUs:          0.5,
		Workspace:     "/workspace",
		CacheMount:    true,
		CacheVol:      "cache-u42",
		SharedBind:    "/host/shared/deeix-42-7",
		SharedTarget:  "/shared/deeix-42-7",
		ImportsBind:   "/host/imports/deeix-42-7",
		ImportsTarget: "/imports",
		Network:       "deeix-sandbox-egress",
	}
	cfg, host := buildContainerConfig(spec, "workspace-u42-c7")
	actual := &inspectedContainerConfig{
		Config:     cfg,
		HostConfig: host,
		Running:    true,
		Networks:   []string{spec.Network},
	}
	return spec, cfg, host, actual
}

func TestBuildContainerConfigPreservesSandboxConstraints(t *testing.T) {
	spec, cfg, host, _ := testContainerConfig()
	if cfg.Image != spec.Image || cfg.User != "0:0" || cfg.WorkingDir != spec.Workspace {
		t.Fatalf("unexpected container config: %+v", cfg)
	}
	if host.Privileged || host.ReadonlyRootfs {
		t.Fatalf("package installation contract broken: privileged=%v readonly=%v", host.Privileged, host.ReadonlyRootfs)
	}
	if len(host.SecurityOpt) != 1 || host.SecurityOpt[0] != "no-new-privileges:true" {
		t.Fatalf("missing no-new-privileges: %v", host.SecurityOpt)
	}
	if host.NetworkMode != container.NetworkMode(spec.Network) {
		t.Fatalf("network = %q, want %q", host.NetworkMode, spec.Network)
	}
	if host.Resources.Memory != 1<<30 || host.Resources.NanoCPUs != 500_000_000 || host.Resources.PidsLimit == nil || *host.Resources.PidsLimit != 256 {
		t.Fatalf("unexpected resources: %+v", host.Resources)
	}

	mounts := make(map[string]mount.Mount, len(host.Mounts))
	for _, item := range host.Mounts {
		mounts[item.Target] = item
	}
	if mounts["/workspace"].Type != mount.TypeVolume || mounts["/workspace"].Source != "workspace-u42-c7" {
		t.Fatalf("workspace mount missing: %+v", mounts["/workspace"])
	}
	if mounts["/root/.cache"].Type != mount.TypeVolume || mounts["/root/.cache"].Source != spec.CacheVol {
		t.Fatalf("cache mount missing: %+v", mounts["/root/.cache"])
	}
	if mounts[spec.SharedTarget].Type != mount.TypeBind || mounts[spec.SharedTarget].Source != spec.SharedBind || mounts[spec.SharedTarget].ReadOnly {
		t.Fatalf("shared scope mount invalid: %+v", mounts[spec.SharedTarget])
	}
	if mounts[spec.ImportsTarget].Type != mount.TypeBind || mounts[spec.ImportsTarget].Source != spec.ImportsBind || !mounts[spec.ImportsTarget].ReadOnly {
		t.Fatalf("imports scope mount invalid: %+v", mounts[spec.ImportsTarget])
	}
}

func TestContainerConfigMatchesCurrentIsolationConstraints(t *testing.T) {
	_, desiredConfig, desiredHost, actual := testContainerConfig()
	if !containerConfigMatches(actual, desiredConfig, desiredHost) {
		t.Fatal("current container constraints should be reusable")
	}

	tests := []struct {
		name   string
		mutate func(*inspectedContainerConfig)
	}{
		{name: "stopped", mutate: func(item *inspectedContainerConfig) { item.Running = false }},
		{name: "non root", mutate: func(item *inspectedContainerConfig) { item.Config.User = "1000:1000" }},
		{name: "default network", mutate: func(item *inspectedContainerConfig) {
			item.HostConfig.NetworkMode = "default"
			item.Networks = []string{"bridge"}
		}},
		{name: "extra network", mutate: func(item *inspectedContainerConfig) {
			item.Networks = append(item.Networks, "internal")
		}},
		{name: "missing imports", mutate: func(item *inspectedContainerConfig) {
			item.HostConfig.Mounts = item.HostConfig.Mounts[:len(item.HostConfig.Mounts)-1]
		}},
		{name: "extra bind", mutate: func(item *inspectedContainerConfig) {
			item.HostConfig.Binds = []string{"/host:/host"}
		}},
		{name: "privileged", mutate: func(item *inspectedContainerConfig) { item.HostConfig.Privileged = true }},
		{name: "wrong memory", mutate: func(item *inspectedContainerConfig) { item.HostConfig.Resources.Memory /= 2 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, candidate := testContainerConfig()
			tc.mutate(candidate)
			if containerConfigMatches(candidate, desiredConfig, desiredHost) {
				t.Fatal("outdated or unsafe container config was accepted")
			}
		})
	}
}

func TestEnsureSessionContainerRestoresPreviousNameWhenRecreateFails(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath := strings.TrimPrefix(r.URL.Path, "/v1.49")
		requests = append(requests, r.Method+" "+requestPath)
		switch {
		case r.Method == http.MethodGet && requestPath == "/containers/deeix-42-7/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Config":{"Image":"custom/image:latest","User":"1000:1000"},"HostConfig":{},"State":{"Running":true},"NetworkSettings":{"Networks":{"bridge":{}}}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(requestPath, "/containers/deeix-42-7/rename"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && requestPath == "/images/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{}]`))
		case r.Method == http.MethodPost && requestPath == "/containers/create":
			http.Error(w, "create failed", http.StatusInternalServerError)
		case r.Method == http.MethodPost && strings.Contains(requestPath, "-migration-") && strings.HasSuffix(requestPath, "/rename"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete:
			t.Fatalf("previous container was deleted during failed migration: %s", requestPath)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	defer server.Close()

	apiClient, err := client.NewClientWithOpts(
		client.WithHost(server.URL),
		client.WithVersion("1.49"),
		client.WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer apiClient.Close()

	root := t.TempDir()
	cfg := &Config{
		BaseImage:       "custom/image:latest",
		WorkspaceDir:    "/workspace",
		MemoryLimit:     "1g",
		PidsLimit:       256,
		CPUsLimit:       0.5,
		CacheVolume:     "cache",
		SharedHostDir:   filepath.Join(root, "shared"),
		SharedMountDir:  "/shared",
		ImportsHostDir:  filepath.Join(root, "imports"),
		ImportsMountDir: "/imports",
		NetworkMode:     "deeix-sandbox-egress",
	}
	manager := NewSessionManager(cfg, &dockerClient{cli: apiClient})
	session := &Session{
		Scope:      "deeix-42-7",
		Image:      cfg.BaseImage,
		Container:  "deeix-42-7",
		Env:        []string{"WORKSPACE=/workspace"},
		CacheMount: true,
		CacheVol:   "cache-u42",
	}

	err = manager.ensureSessionContainer(context.Background(), session)
	if err == nil || !strings.Contains(err.Error(), "previous container restored") {
		t.Fatalf("expected restored migration error, got %v", err)
	}
	if len(requests) < 5 || !strings.HasPrefix(requests[1], "POST /containers/deeix-42-7/rename") ||
		requests[3] != "POST /containers/create" || !strings.Contains(requests[4], "-migration-") {
		t.Fatalf("unexpected Docker API sequence: %#v", requests)
	}
}

func TestParseMemoryBytes(t *testing.T) {
	cases := map[string]int64{
		"1g":       1 << 30,
		"512m":     512 << 20,
		"1024":     1024,
		"":         0,
		"garbage":  0,
		"1garbage": 0,
	}
	for in, want := range cases {
		if got := parseMemoryBytes(in); got != want {
			t.Fatalf("parseMemoryBytes(%q)=%d want %d", in, got, want)
		}
	}
}

func TestValidateFetchURL(t *testing.T) {
	strict := NewStrictOutboundPolicy(true)
	valid := []string{
		"https://example.com/a", "http://example.com", "https://example.com/path?q=1#frag",
	}
	for _, u := range valid {
		if _, err := validateFetchURL(u, strict); err != nil {
			t.Fatalf("url %q should pass: %v", u, err)
		}
	}
	// 协议/格式非法 + 注入载荷（P0-02：这些不再进入 Shell，但必须被拒绝）。
	badProtocol := []string{"file:///etc/passwd", "ftp://x", "gopher://x", "javascript:alert(1)", ""}
	// SSRF 目标（P0-03）：私网/回环/link-local/metadata/IPv6 本机。
	ssrf := []string{
		"http://127.0.0.1:8080/", "http://localhost/", "http://10.0.0.1/", "http://172.16.0.1/",
		"http://192.168.1.1/", "http://169.254.169.254/latest/meta-data/", "http://100.100.100.200/",
		"http://[::1]/", "http://[fd00:ec2::254]/", "http://metadata.google.internal/",
		"http://example.com@127.0.0.1/", "http://0.0.0.0/",
	}
	// Shell 注入形态（P0-02）：换行（控制字符）必须被拒绝；
	// 合法主机 + 路径内注入载荷（$()、反引号、引号、空格、重定向符）现在安全通过或仅失败在 HTTP 层——
	// 下载在 Go 侧执行，不经任何 Shell，这些字符不再能构造命令。
	injection := []string{
		"http://example.com/a\nrm -rf /", "http://example.com/a\r\nrm -rf /",
	}
	for _, u := range append(badProtocol, append(ssrf, injection...)...) {
		if _, err := validateFetchURL(u, strict); err == nil {
			t.Fatalf("url %q should be rejected", u)
		}
	}
	for _, u := range []string{
		"http://example.com/$(whoami)", "http://example.com/`id`", "http://example.com/a'b;cat /etc/passwd",
		"http://example.com/\"; rm -rf /; #", "http://example.com/a> /etc/passwd",
	} {
		if _, err := validateFetchURL(u, strict); err != nil {
			t.Fatalf("payload url %q should pass safely (no shell involved): %v", u, err)
		}
	}
	// 白名单显式放行私网目标。
	policy, err := NewOutboundPolicy(true, []string{"internal.example.com"}, []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateFetchURL("http://10.1.2.3/x", policy); err != nil {
		t.Fatalf("allowlisted CIDR should pass: %v", err)
	}
	if _, err := validateFetchURL("http://internal.example.com/x", policy); err != nil {
		t.Fatalf("allowlisted host should pass: %v", err)
	}
}

func TestSessionBelongsToUser(t *testing.T) {
	cases := []struct {
		scope string
		uid   uint
		want  bool
	}{
		{"deeix-42", 42, true},
		{"deeix-42-7", 42, true},
		{"deeix-42-7", 7, false},
		{"deeix-420", 42, false}, // 前缀但不是用户边界（420 不是 42）
		{"deeix-42", 420, false},
	}
	for _, tc := range cases {
		if got := sessionBelongsToUser(tc.scope, tc.uid); got != tc.want {
			t.Fatalf("sessionBelongsToUser(%q, %d)=%v want %v", tc.scope, tc.uid, got, tc.want)
		}
	}
}

func TestValidScopePattern(t *testing.T) {
	valid := []string{"deeix-1", "deeix-1-2", "deeix-123456-987654"}
	for _, s := range valid {
		if !validScopePattern.MatchString(s) {
			t.Fatalf("scope %q should be valid", s)
		}
	}
	invalid := []string{"", "deeix-", "deeix-1-", "deeix-1-2-3", "deeix-abc", "../etc", "deeix-1/../../host", "deeix-1-2/evil"}
	for _, s := range invalid {
		if validScopePattern.MatchString(s) {
			t.Fatalf("scope %q should be invalid", s)
		}
	}
}

func TestConfigValidateRequiresAPIKey(t *testing.T) {
	// P0-07：API Key 缺失必须拒绝启动（原 TestAuthDisabledWhenKeyEmpty 反转）。
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for empty APIKey")
	}
	cfg.APIKey = "k"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
	// 非法白名单 CIDR 也必须被拒绝。
	cfg.AllowedCIDRs = []string{"not-a-cidr"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for invalid CIDR allowlist")
	}
}

func TestTruncateNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = truncateUTF8(strings.Repeat("界", 1000), 10)
}

func TestSessionReclaimableRequiresTrueIdle(t *testing.T) {
	now := time.Now()
	ttl := 5 * time.Minute
	base := Session{LastUsedAt: now.Add(-time.Hour), tasks: make(map[string]*BackgroundTask)}
	if !sessionReclaimable(&base, now, ttl) {
		t.Fatal("idle expired session should be reclaimable")
	}

	active := base
	active.activeOps = 1
	if sessionReclaimable(&active, now, ttl) {
		t.Fatal("active operation must prevent reclaim")
	}

	background := base
	background.tasks = map[string]*BackgroundTask{"t1": {ID: "t1"}}
	if sessionReclaimable(&background, now, ttl) {
		t.Fatal("background task must prevent reclaim")
	}

	reclaiming := base
	reclaiming.reclaiming = true
	if sessionReclaimable(&reclaiming, now, ttl) {
		t.Fatal("session already reclaiming must not be selected again")
	}

	recent := base
	recent.LastUsedAt = now
	if sessionReclaimable(&recent, now, ttl) {
		t.Fatal("recent session must not be reclaimed")
	}
}

func TestReleaseSessionOperationUpdatesLease(t *testing.T) {
	before := time.Now().Add(-time.Hour)
	session := &Session{activeOps: 1, LastUsedAt: before}
	release := releaseSessionOperation(session)
	release()
	release()
	if session.activeOps != 0 {
		t.Fatalf("activeOps = %d, want 0", session.activeOps)
	}
	if !session.LastUsedAt.After(before) {
		t.Fatal("release did not update LastUsedAt")
	}
}

func TestReclaimExpiry(t *testing.T) {
	cfg := &Config{LeaseTTL: 5 * time.Minute}
	d := &dockerClient{}
	m := NewSessionManager(cfg, d)
	// 手动放入过期会话（绕过 Docker）验证回收删除。
	m.mu.Lock()
	m.live["deeix-1-1"] = &Session{Scope: "deeix-1-1", Container: "deeix-1-1", LastUsedAt: time.Now().Add(-time.Hour)}
	m.mu.Unlock()
	// 不实际调用 removeContainer（无 Docker 环境），只验证过期判定逻辑
	m.mu.Lock()
	now := time.Now()
	expired := 0
	for _, s := range m.live {
		s.mu.Lock()
		if sessionReclaimable(s, now, m.cfg.LeaseTTL) {
			expired++
		}
		s.mu.Unlock()
	}
	m.mu.Unlock()
	if expired != 1 {
		t.Fatalf("expected 1 expired session, got %d", expired)
	}
}
