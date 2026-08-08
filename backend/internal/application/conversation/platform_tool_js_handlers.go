package conversation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/jseval"
)

// 平台工具 JS 执行域：execute_js / execute_skill_script。
// 均在纯计算沙箱（jseval）中运行：无文件系统/网络/进程，超时与输出上限兜底。
// 执行代码属于写操作（write 类），受 write_enabled 开关与用户批准模式管控，
// 与其他写工具一致（ask 模式下 code/args 存入审批记录，批准后执行）。

const (
	// platformJSCodeLimitBytes 单次执行脚本源码上限。
	platformJSCodeLimitBytes = 32 * 1024
	// platformJSExecutableExts execute_skill_script 允许执行的脚本扩展名。
	platformJSExecutableExts = ".js,.mjs,.cjs"
)

// platformExecuteJs 在纯计算沙箱中执行 AI 现场编写的 JavaScript。
// 适合计算、随机数、数据处理等场景；console.log 输出作为结果。
func (s *Service) platformExecuteJs(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		Code           string `json:"code"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	code := strings.TrimSpace(args.Code)
	if code == "" {
		return "", fmt.Errorf("code is required")
	}
	if len(code) > platformJSCodeLimitBytes {
		return "", fmt.Errorf("code exceeds %d bytes limit", platformJSCodeLimitBytes)
	}
	result, runErr := jseval.Run(ctx, code, jseval.Options{
		Timeout: time.Duration(args.TimeoutSeconds) * time.Second,
	})
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.execute_js", "", map[string]interface{}{
		"bytes":       len(code),
		"duration_ms": result.DurationMS,
	})
	return s.marshalJSRunResult(result, runErr, nil)
}

// platformExecuteSkillScript 执行 skill 包内的 JavaScript 脚本（.js/.mjs/.cjs）。
// 复用 GetPackageFile 的清单白名单 + 防穿越校验读取脚本内容，
// 参数通过沙箱的 args 数组注入。
func (s *Service) platformExecuteSkillScript(ctx context.Context, call platformToolCallContext) (string, error) {
	var args struct {
		SkillID        uint          `json:"skill_id"`
		Path           string        `json:"path"`
		ScriptArgs     []interface{} `json:"args"`
		TimeoutSeconds int           `json:"timeout_seconds"`
	}
	if err := decodePlatformArgs(call.Arguments, &args); err != nil {
		return "", err
	}
	if args.SkillID == 0 {
		return "", fmt.Errorf("skill_id is required")
	}
	path := strings.TrimSpace(args.Path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !strings.Contains(platformJSExecutableExts, ext) {
		return "", fmt.Errorf("path must be a javascript file (.js/.mjs/.cjs), got %q", path)
	}
	if s.skillResolver == nil {
		return "", fmt.Errorf("skill service is unavailable")
	}
	content, err := s.skillResolver.GetPackageFile(ctx, call.UserID, args.SkillID, path)
	if err != nil {
		return "", err
	}
	if len(content) > platformJSCodeLimitBytes {
		return "", fmt.Errorf("script exceeds %d bytes limit", platformJSCodeLimitBytes)
	}
	result, runErr := jseval.Run(ctx, string(content), jseval.Options{
		Args:    args.ScriptArgs,
		Timeout: time.Duration(args.TimeoutSeconds) * time.Second,
	})
	s.recordPlatformAudit(ctx, callCtx{userID: call.UserID, requestID: call.RequestID}, "platform_tools.execute_skill_script",
		fmt.Sprintf("%d:%s", args.SkillID, path),
		map[string]interface{}{
			"skill_id":    args.SkillID,
			"path":        path,
			"duration_ms": result.DurationMS,
		})
	return s.marshalJSRunResult(result, runErr, map[string]interface{}{
		"skill_id": args.SkillID,
		"path":     path,
	})
}

// marshalJSRunResult 统一构造 JS 执行工具输出：
// 成功 → status=ok；超时/脚本错误 → status=error + error 描述（正常工具结果，
// 模型可读）；系统级错误上抛给工具循环。
func (s *Service) marshalJSRunResult(result jseval.Result, runErr error, extra map[string]interface{}) (string, error) {
	payload := map[string]interface{}{
		"stdout":      result.Stdout,
		"stderr":      result.Stderr,
		"result":      result.Result,
		"text":        result.Text,
		"duration_ms": result.DurationMS,
		"truncated":   result.Truncated,
	}
	for k, v := range extra {
		payload[k] = v
	}
	switch {
	case runErr == nil:
		payload["status"] = "ok"
	case errors.Is(runErr, jseval.ErrTimeout):
		payload["status"] = "error"
		payload["error"] = "execution timed out"
	default:
		var scriptErr *jseval.ScriptError
		if errors.As(runErr, &scriptErr) {
			payload["status"] = "error"
			payload["error"] = scriptErr.Message
		} else {
			return "", runErr
		}
	}
	return marshalPlatformResult(payload)
}
