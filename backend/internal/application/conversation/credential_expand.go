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

func credentialWriteFromSuccessfulCall(
	executionToolName string,
	isPlatformTool bool,
	inputJSON string,
	runtime *selectedToolRuntime,
) (credentialWrite, bool) {
	write, ok := credentialWriteFromToolCall(executionToolName, isPlatformTool, inputJSON)
	if !ok {
		return credentialWrite{}, false
	}
	return runtime.resolveCredentialSecretWrite(write)
}

func credentialWriteFromToolCall(executionToolName string, isPlatformTool bool, inputJSON string) (credentialWrite, bool) {
	if !isPlatformTool || !isCredentialWritePlatformTool(executionToolName) {
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

func credentialAttemptsFromGenerateOutput(output *llm.GenerateOutput, runtime *selectedToolRuntime) []credentialWrite {
	if output == nil || runtime == nil {
		return nil
	}
	return credentialAttemptsFromToolCalls(output.ToolCalls, runtime.nameMap, runtime.platformEntries, runtime)
}

func credentialAttemptsFromToolCalls(
	toolCalls []llm.ToolCall,
	toolNameMap map[string]string,
	platformTools map[string]platformToolEntry,
	runtime *selectedToolRuntime,
) []credentialWrite {
	attempts := make([]credentialWrite, 0)
	for _, call := range toolCalls {
		modelToolName := strings.TrimSpace(call.ToolName)
		_, isPlatformTool := platformTools[modelToolName]
		write, ok := credentialWriteFromToolCall(
			resolveExecutionToolName(modelToolName, toolNameMap),
			isPlatformTool,
			call.ArgumentsJSON,
		)
		if ok {
			write = runtime.protectCredentialWrite(write)
			attempts = mergeCredentialWrites(attempts, []credentialWrite{write})
		}
	}
	return attempts
}

func mergeCredentialWrites(target []credentialWrite, source []credentialWrite) []credentialWrite {
	for _, candidate := range source {
		if strings.TrimSpace(candidate.Name) == "" || candidate.Value == "" {
			continue
		}
		found := false
		for index := range target {
			existing := target[index]
			sameRef := existing.Ref != "" && candidate.Ref != "" && existing.Ref == candidate.Ref
			if sameRef || (existing.Name == candidate.Name && existing.Value == candidate.Value) {
				if target[index].Ref == "" {
					target[index].Ref = candidate.Ref
				}
				if target[index].Value == "" {
					target[index].Value = candidate.Value
				}
				found = true
				break
			}
		}
		if !found {
			target = append(target, candidate)
		}
	}
	return target
}

func isSuccessfulCredentialWrite(candidate credentialWrite, successful []credentialWrite) bool {
	for _, write := range successful {
		sameRef := write.Ref != "" && candidate.Ref != "" && write.Ref == candidate.Ref
		if sameRef || (write.Name == candidate.Name && write.Value == candidate.Value) {
			return true
		}
	}
	return false
}

func applyCredentialReplacements(text string, attempts []credentialWrite, successful []credentialWrite) (string, bool) {
	result, _ := applyCredentialWrites(text, successful)
	for _, attempt := range attempts {
		if isSuccessfulCredentialWrite(attempt, successful) {
			continue
		}
		for _, protected := range []string{attempt.Value, attempt.Ref} {
			if protected != "" && strings.Contains(result, protected) {
				result = strings.ReplaceAll(result, protected, "[REDACTED]")
			}
		}
	}
	return result, result != text
}

func applyCredentialModelReplacements(text string, attempts []credentialWrite, successful []credentialWrite) (string, bool) {
	result, _ := applyCredentialWrites(text, successful)
	for _, attempt := range attempts {
		if isSuccessfulCredentialWrite(attempt, successful) || attempt.Value == "" || !strings.Contains(result, attempt.Value) {
			continue
		}
		replacement := firstNonEmptyString(attempt.Ref, "[REDACTED]")
		result = strings.ReplaceAll(result, attempt.Value, replacement)
	}
	return result, result != text
}

func applyCredentialModelReplacementsToJSON(raw string, attempts []credentialWrite, successful []credentialWrite) (string, bool) {
	var payload interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return applyCredentialModelReplacements(raw, attempts, successful)
	}
	if !applyCredentialModelReplacementsToJSONValue(&payload, attempts, successful) {
		return raw, false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	return string(encoded), true
}

func applyCredentialModelReplacementsToJSONValue(value *interface{}, attempts []credentialWrite, successful []credentialWrite) bool {
	if value == nil {
		return false
	}
	switch typed := (*value).(type) {
	case string:
		next, changed := applyCredentialModelReplacements(typed, attempts, successful)
		if changed {
			*value = next
		}
		return changed
	case map[string]interface{}:
		changed := false
		for key, child := range typed {
			if applyCredentialModelReplacementsToJSONValue(&child, attempts, successful) {
				typed[key] = child
				changed = true
			}
		}
		return changed
	case []interface{}:
		changed := false
		for index := range typed {
			if applyCredentialModelReplacementsToJSONValue(&typed[index], attempts, successful) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func applyCredentialReplacementsToJSON(raw string, attempts []credentialWrite, successful []credentialWrite) (string, bool) {
	var payload interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return applyCredentialReplacements(raw, attempts, successful)
	}
	if !applyCredentialReplacementsToJSONValue(&payload, attempts, successful) {
		return raw, false
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	return string(encoded), true
}

func applyCredentialReplacementsToJSONValue(value *interface{}, attempts []credentialWrite, successful []credentialWrite) bool {
	if value == nil {
		return false
	}
	switch typed := (*value).(type) {
	case string:
		next, changed := applyCredentialReplacements(typed, attempts, successful)
		if changed {
			*value = next
		}
		return changed
	case map[string]interface{}:
		changed := false
		for key, child := range typed {
			if applyCredentialReplacementsToJSONValue(&child, attempts, successful) {
				typed[key] = child
				changed = true
			}
		}
		return changed
	case []interface{}:
		changed := false
		for index := range typed {
			if applyCredentialReplacementsToJSONValue(&typed[index], attempts, successful) {
				changed = true
			}
		}
		return changed
	default:
		return false
	}
}

func applyCredentialReplacementsToLLMMessages(messages []llm.Message, attempts []credentialWrite, successful []credentialWrite) bool {
	changed := false
	for index := range messages {
		if content, contentChanged := applyCredentialReplacements(messages[index].Content, attempts, successful); contentChanged {
			messages[index].Content = content
			changed = true
		}
		if reasoning, reasoningChanged := applyCredentialReplacements(messages[index].ReasoningContent, attempts, successful); reasoningChanged {
			messages[index].ReasoningContent = reasoning
			changed = true
		}
		for toolIndex := range messages[index].ToolCalls {
			if arguments, argumentsChanged := applyCredentialReplacementsToJSON(messages[index].ToolCalls[toolIndex].ArgumentsJSON, attempts, successful); argumentsChanged {
				messages[index].ToolCalls[toolIndex].ArgumentsJSON = arguments
				changed = true
			}
		}
	}
	return changed
}

func applyCredentialModelReplacementsToLLMMessages(messages []llm.Message, attempts []credentialWrite, successful []credentialWrite) bool {
	changed := false
	for index := range messages {
		if content, contentChanged := applyCredentialModelReplacements(messages[index].Content, attempts, successful); contentChanged {
			messages[index].Content = content
			changed = true
		}
		if reasoning, reasoningChanged := applyCredentialModelReplacements(messages[index].ReasoningContent, attempts, successful); reasoningChanged {
			messages[index].ReasoningContent = reasoning
			changed = true
		}
		for toolIndex := range messages[index].ToolCalls {
			if arguments, argumentsChanged := applyCredentialModelReplacementsToJSON(messages[index].ToolCalls[toolIndex].ArgumentsJSON, attempts, successful); argumentsChanged {
				messages[index].ToolCalls[toolIndex].ArgumentsJSON = arguments
				changed = true
			}
		}
	}
	return changed
}

func applyCredentialReplacementsToToolCallRows(rows []model.ToolCall, attempts []credentialWrite, successful []credentialWrite) {
	for index := range rows {
		rows[index].InputJSON, _ = applyCredentialReplacementsToJSON(rows[index].InputJSON, attempts, successful)
		rows[index].OutputJSON, _ = applyCredentialReplacements(rows[index].OutputJSON, attempts, successful)
		rows[index].ErrorJSON, _ = applyCredentialReplacements(rows[index].ErrorJSON, attempts, successful)
	}
}

func (s *Service) scrubPersistedToolCalls(
	ctx context.Context,
	userID uint,
	conversationID uint,
	runID string,
	runIDPrefix bool,
	attempts []credentialWrite,
	successful []credentialWrite,
) error {
	if s == nil || s.repo == nil || len(attempts) == 0 || strings.TrimSpace(runID) == "" {
		return nil
	}
	var (
		rows []model.ToolCall
		err  error
	)
	if runIDPrefix {
		rows, err = s.repo.ListConversationToolCallsByRunIDPrefix(ctx, userID, conversationID, strings.TrimSpace(runID))
	} else {
		rows, err = s.repo.ListConversationToolCallsByRunID(ctx, userID, conversationID, strings.TrimSpace(runID))
	}
	if err != nil {
		return err
	}
	for index := range rows {
		beforeInput := rows[index].InputJSON
		beforeOutput := rows[index].OutputJSON
		beforeError := rows[index].ErrorJSON
		applyCredentialReplacementsToToolCallRows(rows[index:index+1], attempts, successful)
		if rows[index].InputJSON == beforeInput && rows[index].OutputJSON == beforeOutput && rows[index].ErrorJSON == beforeError {
			continue
		}
		if err := s.repo.UpdateConversationToolCallPayload(ctx, userID, conversationID, rows[index].RunID, rows[index]); err != nil {
			return err
		}
	}
	return nil
}

func credentialAttemptValueRemains(text string, attempts []credentialWrite) bool {
	for _, attempt := range attempts {
		for _, protected := range []string{attempt.Value, attempt.Ref} {
			if protected != "" && strings.Contains(text, protected) {
				return true
			}
		}
	}
	return false
}

func credentialWriteToolsAvailable(input llm.GenerateInput, runtime *selectedToolRuntime) bool {
	if runtime == nil || input.DisableTools {
		return false
	}
	for _, definition := range input.Tools {
		modelName := strings.TrimSpace(definition.Name)
		if _, ok := runtime.platformEntries[modelName]; !ok {
			continue
		}
		if isCredentialWritePlatformTool(resolveExecutionToolName(modelName, runtime.nameMap)) {
			return true
		}
	}
	return false
}

func sanitizeGenerateOutputCredentialAttempts(output *llm.GenerateOutput, attempts []credentialWrite) {
	if output == nil || len(attempts) == 0 {
		return
	}
	output.Text, _ = applyCredentialReplacements(output.Text, attempts, nil)
	for index := range output.ToolCalls {
		output.ToolCalls[index].ArgumentsJSON, _ = applyCredentialModelReplacementsToJSON(output.ToolCalls[index].ArgumentsJSON, attempts, nil)
	}
	if output.Reasoning != nil {
		output.Reasoning.Text, _ = applyCredentialReplacements(output.Reasoning.Text, attempts, nil)
		output.Reasoning.Summary, _ = applyCredentialReplacements(output.Reasoning.Summary, attempts, nil)
	}
	for index := range output.ServerToolCalls {
		output.ServerToolCalls[index].ArgumentsJSON, _ = applyCredentialReplacementsToJSON(output.ServerToolCalls[index].ArgumentsJSON, attempts, nil)
		output.ServerToolCalls[index].OutputJSON, _ = applyCredentialReplacements(output.ServerToolCalls[index].OutputJSON, attempts, nil)
		output.ServerToolCalls[index].ErrorJSON, _ = applyCredentialReplacements(output.ServerToolCalls[index].ErrorJSON, attempts, nil)
	}
	for index := range output.Citations {
		output.Citations[index], _ = applyCredentialReplacements(output.Citations[index], attempts, nil)
	}
	for index := range output.GeneratedImages {
		output.GeneratedImages[index].URL, _ = applyCredentialReplacements(output.GeneratedImages[index].URL, attempts, nil)
		output.GeneratedImages[index].RevisedPrompt, _ = applyCredentialReplacements(output.GeneratedImages[index].RevisedPrompt, attempts, nil)
	}
	for index := range output.GeneratedVideos {
		output.GeneratedVideos[index].URL, _ = applyCredentialReplacements(output.GeneratedVideos[index].URL, attempts, nil)
		output.GeneratedVideos[index].FileName, _ = applyCredentialReplacements(output.GeneratedVideos[index].FileName, attempts, nil)
	}
	output.RawJSON = ""
	if output.Debug != nil {
		output.Debug.Request.Body, _ = applyCredentialReplacements(output.Debug.Request.Body, attempts, nil)
		output.Debug.Response.Body, _ = applyCredentialReplacements(output.Debug.Response.Body, attempts, nil)
	}
}

func sanitizeGenerateStreamEventCredentialAttempts(event llm.GenerateStreamEvent, attempts []credentialWrite) llm.GenerateStreamEvent {
	if len(attempts) == 0 {
		return event
	}
	event.Delta, _ = applyCredentialReplacements(event.Delta, attempts, nil)
	if event.Reasoning != nil {
		reasoning := *event.Reasoning
		reasoning.Text, _ = applyCredentialReplacements(reasoning.Text, attempts, nil)
		event.Reasoning = &reasoning
	}
	if event.ServerToolCall != nil {
		call := *event.ServerToolCall
		call.ArgumentsJSON, _ = applyCredentialReplacementsToJSON(call.ArgumentsJSON, attempts, nil)
		call.OutputJSON, _ = applyCredentialReplacements(call.OutputJSON, attempts, nil)
		call.ErrorJSON, _ = applyCredentialReplacements(call.ErrorJSON, attempts, nil)
		event.ServerToolCall = &call
	}
	if event.GeneratedImage != nil {
		image := *event.GeneratedImage
		image.URL, _ = applyCredentialReplacements(image.URL, attempts, nil)
		image.RevisedPrompt, _ = applyCredentialReplacements(image.RevisedPrompt, attempts, nil)
		event.GeneratedImage = &image
	}
	return event
}

func applyCredentialWrites(text string, writes []credentialWrite) (string, bool) {
	result := text
	for _, write := range writes {
		if write.Name == "" {
			continue
		}
		placeholder := "{{credential: " + write.Name + "}}"
		for _, protected := range []string{write.Value, write.Ref} {
			if protected != "" && strings.Contains(result, protected) {
				result = strings.ReplaceAll(result, protected, placeholder)
			}
		}
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
