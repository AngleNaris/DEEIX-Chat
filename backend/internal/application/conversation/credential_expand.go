package conversation

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
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

func credentialWriteFromSuccessfulCall(executionToolName string, isPlatformTool bool, inputJSON string) (credentialWrite, bool) {
	if !isPlatformTool {
		return credentialWrite{}, false
	}
	name := strings.TrimSpace(executionToolName)
	if name != "credential_create" && name != "credential_update" {
		return credentialWrite{}, false
	}
	var arguments struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(inputJSON), &arguments); err != nil {
		return credentialWrite{}, false
	}
	arguments.Name = strings.TrimSpace(arguments.Name)
	if arguments.Name == "" || arguments.Value == "" {
		return credentialWrite{}, false
	}
	return credentialWrite{Name: arguments.Name, Value: arguments.Value}, true
}

func applyCredentialWrites(text string, writes []credentialWrite) (string, bool) {
	result := text
	for _, write := range writes {
		if write.Value == "" || write.Name == "" || !strings.Contains(result, write.Value) {
			continue
		}
		result = strings.ReplaceAll(result, write.Value, "{{credential: "+write.Name+"}}")
	}
	return result, result != text
}

func applyCredentialWritesToJSON(raw string, writes []credentialWrite) (string, bool) {
	var payload interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return applyCredentialWrites(raw, writes)
	}
	if !applyCredentialWritesToJSONValue(&payload, writes) {
		return raw, false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	return string(encoded), true
}

func applyCredentialWritesToJSONValue(value *interface{}, writes []credentialWrite) bool {
	if value == nil {
		return false
	}
	switch typed := (*value).(type) {
	case string:
		next, changed := applyCredentialWrites(typed, writes)
		if changed {
			*value = next
		}
		return changed
	case map[string]interface{}:
		changed := false
		for key, child := range typed {
			if applyCredentialWritesToJSONValue(&child, writes) {
				typed[key] = child
				changed = true
			}
		}
		return changed
	case []interface{}:
		changed := false
		for index := range typed {
			if applyCredentialWritesToJSONValue(&typed[index], writes) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func applyCredentialWritesToLLMMessages(messages []llm.Message, writes []credentialWrite) bool {
	changed := false
	for index := range messages {
		if content, contentChanged := applyCredentialWrites(messages[index].Content, writes); contentChanged {
			messages[index].Content = content
			changed = true
		}
		if reasoning, reasoningChanged := applyCredentialWrites(messages[index].ReasoningContent, writes); reasoningChanged {
			messages[index].ReasoningContent = reasoning
			changed = true
		}
		for toolIndex := range messages[index].ToolCalls {
			if arguments, argumentsChanged := applyCredentialWrites(messages[index].ToolCalls[toolIndex].ArgumentsJSON, writes); argumentsChanged {
				messages[index].ToolCalls[toolIndex].ArgumentsJSON = arguments
				changed = true
			}
		}
	}
	return changed
}

func (s *Service) applyCredentialWritesToUserMessage(
	ctx context.Context,
	message *model.Message,
	conversationID uint,
	userID uint,
	writes []credentialWrite,
) (bool, error) {
	if message == nil || len(writes) == 0 {
		return false, nil
	}
	content, changed := applyCredentialWrites(message.Content, writes)
	if !changed {
		return false, nil
	}
	if err := s.repo.UpdateUserMessageContent(ctx, message.ID, conversationID, userID, content); err != nil {
		return false, err
	}
	message.Content = content
	return true, nil
}
