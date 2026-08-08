package jseval

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunBasicMath(t *testing.T) {
	res, err := Run(context.Background(), `1 + 2 * 3`, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Result != "7" {
		t.Fatalf("unexpected result %q", res.Result)
	}
	if res.Text != res.Result {
		t.Fatalf("text must mirror result, got %q", res.Text)
	}
	if res.DurationMS < 0 {
		t.Fatalf("invalid duration %d", res.DurationMS)
	}
}

func TestRunConsoleCapture(t *testing.T) {
	res, err := Run(context.Background(), `
console.log("hello", 42);
console.info({a: 1, b: [1, 2]});
console.error("bad");
console.warn("warn");
`, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(res.Stdout, "hello 42") {
		t.Fatalf("stdout missing hello: %q", res.Stdout)
	}
	if !strings.Contains(res.Stdout, `{"a":1,"b":[1,2]}`) {
		t.Fatalf("stdout missing object json: %q", res.Stdout)
	}
	if !strings.Contains(res.Stderr, "bad") || !strings.Contains(res.Stderr, "warn") {
		t.Fatalf("stderr missing messages: %q", res.Stderr)
	}
}

func TestRunCompletionValues(t *testing.T) {
	cases := []struct {
		code string
		want string
	}{
		{`"abc"`, "abc"},
		{`[1, 2, 3]`, `[1,2,3]`},
		{`({x: 1})`, `{"x":1}`},
		{`Math.random()`, ""}, // 结果不可预测但必须非空，单独断言
	}
	for _, tc := range cases {
		res, err := Run(context.Background(), tc.code, Options{})
		if err != nil {
			t.Fatalf("%q: %v", tc.code, err)
		}
		if tc.want == "" {
			if res.Result == "" {
				t.Fatalf("%q: expected non-empty result", tc.code)
			}
			continue
		}
		if res.Result != tc.want {
			t.Fatalf("%q: got %q, want %q", tc.code, res.Result, tc.want)
		}
	}
}

func TestRunArgsInjection(t *testing.T) {
	res, err := Run(context.Background(), `JSON.stringify(args)`, Options{
		Args: []interface{}{"a", 2, true},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Result != `["a",2,true]` {
		t.Fatalf("unexpected args result %q", res.Result)
	}
}

func TestRunScriptError(t *testing.T) {
	// 语法错误。
	_, err := Run(context.Background(), `const = 1`, Options{})
	var scriptErr *ScriptError
	if !errors.As(err, &scriptErr) {
		t.Fatalf("expected *ScriptError, got %v", err)
	}
	// 运行错误。
	_, err = Run(context.Background(), `throw new Error("boom")`, Options{})
	if !errors.As(err, &scriptErr) || !strings.Contains(scriptErr.Message, "boom") {
		t.Fatalf("expected ScriptError with boom, got %v", err)
	}
}

func TestRunTimeout(t *testing.T) {
	start := time.Now()
	_, err := Run(context.Background(), `while (true) {}`, Options{
		Timeout: 200 * time.Millisecond,
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("timeout did not interrupt promptly: %v", elapsed)
	}
}

func TestRunOutputTruncation(t *testing.T) {
	res, err := Run(context.Background(), `console.log("x".repeat(10000))`, Options{
		MaxOutputBytes: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Truncated {
		t.Fatalf("expected truncated flag")
	}
	if len(res.Stdout) > 100 {
		t.Fatalf("stdout not truncated: %d bytes", len(res.Stdout))
	}
}

func TestRunSandboxBoundaries(t *testing.T) {
	// 沙箱内不可用宿主能力。
	res, err := Run(context.Background(), `JSON.stringify({require: typeof require, process: typeof process, global: typeof global, fetch: typeof fetch})`, Options{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var exported map[string]string
	if err := json.Unmarshal([]byte(res.Result), &exported); err != nil {
		t.Fatalf("bad result %q: %v", res.Result, err)
	}
	for name, typ := range exported {
		if typ != "undefined" {
			t.Fatalf("sandbox leaked %q (type %q)", name, typ)
		}
	}
}
