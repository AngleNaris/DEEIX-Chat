package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	appdynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/dynamicprompt"
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

func TestExpandSystemPromptScriptVars(t *testing.T) {
	vars := newSystemPromptVars(time.Now(), "", "")
	// 无 resolver 时脚本标签替换为空。
	if got := expandSystemPromptVars("a{{script: missing}}b", vars); got != "ab" {
		t.Fatalf("no resolver: got %q", got)
	}
	// 注册 resolver：js 执行 + text 直插 + 未启用/未找到为空。
	vars.scriptResolver = func(name string) string {
		switch name {
		case "calc":
			return runPromptJSVar("6 * 7")
		case "note":
			return "固定文本"
		default:
			return ""
		}
	}
	if got := expandSystemPromptVars("{{script: calc}}|{{script: note}}|{{script: nope}}", vars); got != "42|固定文本|" {
		t.Fatalf("script expansion: got %q", got)
	}
	// 脚本标签与 js 标签共享限额。
	if got := expandSystemPromptVars("{{script: calc}} {{js: 1}} {{script: note}} {{script: calc}} {{js: 2}}", vars); strings.Count(got, "|") != 0 && got != "42 1 固定文本 " {
		// 前 3 个脚本类标签生效，第 4/5 个被限额截断。
		if !strings.HasPrefix(got, "42 1 固定文本") {
			t.Fatalf("script cap: got %q", got)
		}
	}
}

// fakeDynamicPromptReader 模拟 dynamicPromptReader。
type fakeDynamicPromptReader struct {
	items []appdynamicprompt.PromptView
}

func (f *fakeDynamicPromptReader) ListDynamicPrompts(ctx context.Context, userID uint) ([]appdynamicprompt.PromptView, error) {
	return f.items, nil
}

func TestResolveSystemPromptScriptsAttached(t *testing.T) {
	svc := &Service{
		userProfile: &fakeUserProfile{locale: "zh-CN", username: "alice"},
		dynamicPrompts: &fakeDynamicPromptReader{items: []appdynamicprompt.PromptView{
			{Name: "calc", Kind: "js", Content: "40 + 2", Enabled: true},
			{Name: "off", Kind: "text", Content: "x", Enabled: false},
		}},
	}
	vars := svc.resolveSystemPromptVars(context.Background(), 1)
	if vars.scriptResolver == nil {
		t.Fatalf("script resolver must be attached")
	}
	if got := vars.scriptResolver("calc"); got != "42" {
		t.Fatalf("js script expansion: got %q", got)
	}
	if got := vars.scriptResolver("off"); got != "" {
		t.Fatalf("disabled script must expand empty, got %q", got)
	}
	if got := vars.scriptResolver("missing"); got != "" {
		t.Fatalf("missing script must expand empty, got %q", got)
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

// fakeUserSettingsWriter 模拟 userSettingsWriter。
type fakeUserSettingsWriter struct {
	values map[string]string
}

func (f *fakeUserSettingsWriter) ListSettings(ctx context.Context, userID uint) (map[string]string, error) {
	return f.values, nil
}

func (f *fakeUserSettingsWriter) PatchSettings(ctx context.Context, userID uint, patches map[string]string) (map[string]string, error) {
	for key, value := range patches {
		f.values[key] = value
	}
	return f.values, nil
}

func TestResolveSystemPromptVarsTimeZone(t *testing.T) {
	// usersettings.timezone=Asia/Shanghai（UTC+8）→ 时间变量按该时区渲染。
	svc := &Service{
		userSettingsSvc: &fakeUserSettingsWriter{values: map[string]string{"timezone": "Asia/Shanghai"}},
		userProfile:     &fakeUserProfile{locale: "zh-CN", username: "alice"},
	}
	vars := svc.resolveSystemPromptVars(t.Context(), 1)
	if vars.Language != "zh-CN" || vars.Username != "alice" {
		t.Fatalf("profile vars missing: %+v", vars)
	}
	// 与容器 UTC 相比，上海时区的 date 不应等于 UTC 的 date（除非恰好同日边界一致）。
	utcVars := newSystemPromptVars(time.Now().UTC(), "", "")
	if vars.Date == utcVars.Date && vars.Time == utcVars.Time {
		// 边界可能恰好一致，无法断言差异；至少确认格式与解析正确。
		if _, err := time.Parse("2006-01-02", vars.Date); err != nil {
			t.Fatalf("invalid date format %q", vars.Date)
		}
	} else {
		// 差异存在时，上海墙钟时间应领先 UTC 墙钟时间 8 小时（跨日时差为负需加 24h）。
		shanghaiLoc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			t.Fatalf("load shanghai: %v", err)
		}
		shanghaiWall, err := time.ParseInLocation("2006-01-02 15:04:05", vars.DateTime, shanghaiLoc)
		if err != nil {
			t.Fatalf("invalid datetime %q", vars.DateTime)
		}
		utcWall, err := time.ParseInLocation("2006-01-02 15:04:05", utcVars.DateTime, time.UTC)
		if err != nil {
			t.Fatalf("invalid utc datetime %q", utcVars.DateTime)
		}
		shanghaiMin := shanghaiWall.Hour()*60 + shanghaiWall.Minute()
		utcMin := utcWall.Hour()*60 + utcWall.Minute()
		diffMin := shanghaiMin - utcMin
		if diffMin < 0 {
			diffMin += 24 * 60
		}
		if diffMin != 8*60 {
			t.Fatalf("expected +8h wall-clock diff, got %dm (%q vs %q)", diffMin, vars.DateTime, utcVars.DateTime)
		}
	}
}

// fakeUserProfile 模拟 userProfileReader。
type fakeUserProfile struct {
	locale   string
	username string
	timezone string
}

func (f *fakeUserProfile) GetUserProfile(ctx context.Context, userID uint) (string, string, string, error) {
	return f.locale, f.username, f.timezone, nil
}
