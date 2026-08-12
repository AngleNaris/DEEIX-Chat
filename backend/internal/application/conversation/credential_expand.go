package conversation

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// 凭据占位符语法：{{credential: name}}（与 {{script: name}} 变量风格一致）。
// 模型在工具参数中引用凭据名，执行层在发送前展开为真实值；
// 展开结果只存在于后端内存/执行瞬间，落库与 trace 均保留占位符原文。
var credentialRefPattern = regexp.MustCompile(`\{\{\s*credential\s*:\s*([^{}]+?)\s*\}\}`)

// expandCredentialRefsInJSON 递归展开参数 JSON 字符串值中的 {{credential: name}} 占位符。
// 只替换 JSON 字符串字面量内部（保证展开值中的引号/反斜杠不破坏 JSON）；
// JSON 解析失败或凭据不存在时保留原样。
func (s *Service) expandCredentialRefsInJSON(ctx context.Context, userID uint, argumentsJSON string) string {
	if s.credentials == nil || !strings.Contains(argumentsJSON, "{{") {
		return argumentsJSON
	}
	var value interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &value); err != nil {
		return argumentsJSON
	}
	expanded := s.expandCredentialRefsRecursive(ctx, userID, value)
	data, err := json.Marshal(expanded)
	if err != nil {
		return argumentsJSON
	}
	return string(data)
}

func (s *Service) expandCredentialRefsRecursive(ctx context.Context, userID uint, value interface{}) interface{} {
	switch typed := value.(type) {
	case string:
		return s.expandCredentialRefsInString(ctx, userID, typed)
	case map[string]interface{}:
		for key, item := range typed {
			typed[key] = s.expandCredentialRefsRecursive(ctx, userID, item)
		}
		return typed
	case []interface{}:
		for i, item := range typed {
			typed[i] = s.expandCredentialRefsRecursive(ctx, userID, item)
		}
		return typed
	default:
		return value
	}
}

func (s *Service) expandCredentialRefsInString(ctx context.Context, userID uint, text string) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	return credentialRefPattern.ReplaceAllStringFunc(text, func(match string) string {
		submatch := credentialRefPattern.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		name := strings.TrimSpace(submatch[1])
		if name == "" {
			return match
		}
		resolved, err := s.credentials.ResolveValue(ctx, userID, name)
		if err != nil || resolved == "" {
			// 凭据不存在或解密失败：保留占位符，不阻塞执行。
			return match
		}
		return resolved
	})
}

// maskCredentialToolInput 对凭据管理工具（credential_*）的输入落库前打码：
// 创建/更新参数中的 value 字段替换为 [REDACTED]，避免密钥明文进入 tool_calls 表与 trace。
// 仅对平台工具生效（isPlatformTool 区分，避免误伤同名 MCP 工具参数）。
func maskCredentialToolInput(executionToolName string, isPlatformTool bool, inputJSON string) string {
	if !isPlatformTool || !isCredentialPlatformTool(executionToolName) {
		return inputJSON
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(inputJSON), &obj); err != nil {
		return inputJSON
	}
	if _, ok := obj["value"]; ok {
		obj["value"] = "[REDACTED]"
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return inputJSON
	}
	return string(data)
}
