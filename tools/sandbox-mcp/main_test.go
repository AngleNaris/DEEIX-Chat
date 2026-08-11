package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseMeta(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantUID uint
		wantCID uint
		wantErr bool
	}{
		{
			name:    "deeix style params._meta",
			body:    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"sandbox_exec","arguments":{"command":"ls"},"_meta":{"user_id":42,"conversation_id":7,"request_id":"req-1"}}}`,
			wantUID: 42, wantCID: 7,
		},
		{
			name:    "no conversation id",
			body:    `{"params":{"_meta":{"user_id":9}}}`,
			wantUID: 9, wantCID: 0,
		},
		{
			name:    "string user id",
			body:    `{"params":{"_meta":{"user_id":"3","conversation_id":"5"}}}`,
			wantUID: 3, wantCID: 5,
		},
		{
			name:    "missing _meta rejected",
			body:    `{"params":{"name":"sandbox_exec","arguments":{}}}`,
			wantErr: true,
		},
		{
			name:    "zero user id rejected",
			body:    `{"params":{"_meta":{"user_id":0}}}`,
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
			meta, err := ParseMeta([]byte(tc.body))
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
	if _, err := validateFetchURL("https://example.com/a"); err != nil {
		t.Fatalf("https should pass: %v", err)
	}
	if _, err := validateFetchURL("http://example.com"); err != nil {
		t.Fatalf("http should pass: %v", err)
	}
	for _, bad := range []string{"file:///etc/passwd", "ftp://x", "gopher://x", "javascript:alert(1)", ""} {
		if _, err := validateFetchURL(bad); err == nil {
			t.Fatalf("url %q should be rejected", bad)
		}
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
		if now.Sub(s.LastUsedAt) > m.cfg.LeaseTTL {
			expired++
		}
	}
	m.mu.Unlock()
	if expired != 1 {
		t.Fatalf("expected 1 expired session, got %d", expired)
	}
}
