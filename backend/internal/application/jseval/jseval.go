// Package jseval 提供纯计算 JavaScript 沙箱执行能力，供平台工具
// execute_js / execute_skill_script 使用。
//
// 安全边界：基于 goja（纯 Go 解释器，无 cgo），不暴露文件系统、网络与进程
// 能力（不注入任何 IO 相关全局对象）。仅注入：
//   - console.log/info/error/warn：捕获输出到 stdout/stderr
//   - args：脚本参数数组（execute_skill_script 传入）
//
// 资源限制：执行超时（默认 5s，上限 15s，超时通过 vm.Interrupt 中断
// 无限循环）；捕获输出上限 64KB，超限截断并标记 truncated。
package jseval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// DefaultTimeout 默认执行超时。
const DefaultTimeout = 5 * time.Second

// MaxTimeout 允许的最大执行超时（调用方可覆盖）。
const MaxTimeout = 15 * time.Second

// DefaultMaxOutputBytes 捕获输出（stdout/stderr 合计）上限。
const DefaultMaxOutputBytes = 64 * 1024

// ErrTimeout 执行超时被中断。
var ErrTimeout = errors.New("javascript execution timed out")

// ScriptError 脚本语法或运行错误（非系统错误，模型可读）。
type ScriptError struct {
	Message string
}

func (e *ScriptError) Error() string {
	return "javascript error: " + e.Message
}

// Options 单次执行选项。
type Options struct {
	// Args 注入到脚本的 args 数组（execute_skill_script 使用）。
	Args []interface{}
	// Timeout 执行超时，零值使用 DefaultTimeout，超 MaxTimeout 被截断。
	Timeout time.Duration
	// MaxOutputBytes 输出捕获上限，零值使用 DefaultMaxOutputBytes。
	MaxOutputBytes int
}

// Result 一次执行的结果。
type Result struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Result     string `json:"result"` // 最后表达式语句的 JSON 序列化（可序列化时）
	Text       string `json:"text"`   // Result 副本（前端 code_interpreter 渲染展示）
	DurationMS int64  `json:"duration_ms"`
	Truncated  bool   `json:"truncated"`
}

// cappedBuffer 容量受限的字符串收集器，超限截断并标记。
type cappedBuffer struct {
	mu        sync.Mutex
	b         strings.Builder
	max       int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.max - c.b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			c.b.Write(p[:remaining])
			c.truncated = true
		} else {
			c.b.Write(p)
		}
	} else {
		c.truncated = true
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}

// Run 在纯计算沙箱中执行 code。脚本错误（语法/运行）与超时以
// *ScriptError / ErrTimeout 返回；系统级错误原样返回。
func Run(ctx context.Context, code string, opts Options) (Result, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if timeout > MaxTimeout {
		timeout = MaxTimeout
	}
	maxOut := opts.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = DefaultMaxOutputBytes
	}

	vm := goja.New()
	stdout := &cappedBuffer{max: maxOut}
	stderr := &cappedBuffer{max: maxOut}
	console := vm.NewObject()
	mkConsole := func(target *cappedBuffer) func(call goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			parts := make([]string, 0, len(call.Arguments))
			for _, a := range call.Arguments {
				parts = append(parts, formatConsoleValue(a))
			}
			_, _ = fmt.Fprintln(target, strings.Join(parts, " "))
			return goja.Undefined()
		}
	}
	_ = console.Set("log", mkConsole(stdout))
	_ = console.Set("info", mkConsole(stdout))
	_ = console.Set("error", mkConsole(stderr))
	_ = console.Set("warn", mkConsole(stderr))
	if err := vm.Set("console", console); err != nil {
		return Result{}, err
	}
	if opts.Args != nil {
		if err := vm.Set("args", opts.Args); err != nil {
			return Result{}, err
		}
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt("execution canceled")
		case <-done:
		}
	}()
	timer := time.AfterFunc(timeout, func() {
		vm.Interrupt("execution timed out")
	})
	defer timer.Stop()

	start := time.Now()
	value, err := vm.RunString(code)
	durMS := time.Since(start).Milliseconds()
	result := Result{
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		DurationMS: durMS,
		Truncated:  stdout.truncated || stderr.truncated,
	}
	if err != nil {
		var interr *goja.InterruptedError
		if errors.As(err, &interr) {
			return result, ErrTimeout
		}
		var exc *goja.Exception
		if errors.As(err, &exc) {
			return result, &ScriptError{Message: exc.String()}
		}
		return result, err
	}
	result.Result = serializeCompletion(value)
	result.Text = result.Result
	return result, nil
}

// formatConsoleValue 把 console 参数渲染为文本：对象/数组 JSON 序列化，
// 其余（字符串/数字/布尔）用 String()。
func formatConsoleValue(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) {
		return "undefined"
	}
	if goja.IsNull(v) {
		return "null"
	}
	exported := v.Export()
	switch exported.(type) {
	case string, float64, int64, bool:
		return v.String()
	default:
		data, err := json.Marshal(exported)
		if err != nil {
			return v.String()
		}
		return string(data)
	}
}

// serializeCompletion 序列化脚本完成值：字符串原样返回，其余 JSON。
func serializeCompletion(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return ""
	}
	exported := v.Export()
	if s, ok := exported.(string); ok {
		return s
	}
	data, err := json.Marshal(exported)
	if err != nil {
		return ""
	}
	return string(data)
}
