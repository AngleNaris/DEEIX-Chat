package conversation

import (
	"strings"
	"testing"
	"time"
)

func TestExpandSystemPromptVars(t *testing.T) {
	now := time.Date(2026, 8, 8, 14, 30, 5, 0, time.Local)
	vars := newSystemPromptVars(now, "zh-CN", "alice")

	cases := []struct {
		input string
		want  string
	}{
		{"today is {{date}}", "today is 2026-08-08"},
		{"now {{time}}", "now 14:30:05"},
		{"at {{datetime}}", "at 2026-08-08 14:30:05"},
		{"weekday {{weekday}}", "weekday Saturday"},
		{"lang {{language}} user {{username}}", "lang zh-CN user alice"},
		{"unknown {{foobar}} stays", "unknown {{foobar}} stays"},
		{"no vars here", "no vars here"},
		{"{{ date }} trimmed", "2026-08-08 trimmed"},
	}
	for _, tc := range cases {
		if got := expandSystemPromptVars(tc.input, vars); got != tc.want {
			t.Fatalf("%q: got %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExpandSystemPromptJSVars(t *testing.T) {
	vars := newSystemPromptVars(time.Now(), "", "")
	// 表达式值插入。
	got := expandSystemPromptVars("result: {{js: 6 * 7}}", vars)
	if got != "result: 42" {
		t.Fatalf("js expression: got %q", got)
	}
	// console 输出兜底。
	got = expandSystemPromptVars("{{js: console.log('hello')}}", vars)
	if !strings.Contains(got, "hello") {
		t.Fatalf("js console: got %q", got)
	}
	// 语法错误 → 空串，不阻塞。
	got = expandSystemPromptVars("a={{js: const = }}b", vars)
	if got != "a=b" {
		t.Fatalf("js error must render empty: got %q", got)
	}
	// 上限：单个提示词最多 3 个 js 变量。
	input := "{{js: 1}} {{js: 2}} {{js: 3}} {{js: 4}}"
	got = expandSystemPromptVars(input, vars)
	if strings.Count(got, " ") != 3 {
		t.Fatalf("js var cap: got %q", got)
	}
}

func TestResolveSystemPromptVarsNoProfile(t *testing.T) {
	svc := &Service{}
	vars := svc.resolveSystemPromptVars(t.Context(), 0)
	if vars.Language != "" || vars.Username != "" {
		t.Fatalf("expected empty profile vars, got %+v", vars)
	}
	if vars.Date == "" {
		t.Fatalf("date var must always be available")
	}
}
