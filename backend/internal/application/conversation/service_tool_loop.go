package conversation

import (
	"encoding/json"
	"strings"

	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/conversation"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

func syncUpstreamOutputThinking(traceRecorder *messageTraceRecorder, output *llm.GenerateOutput) string {
	if output == nil {
		return ""
	}
	assistantText, extractedThink := splitAssistantOutputThinkingContent(output.Text)
	if assistantText == "" && strings.TrimSpace(extractedThink) == "" {
		assistantText = strings.TrimSpace(output.Text)
	}
	if traceRecorder != nil && output.Reasoning != nil {
		traceRecorder.syncStructuredThink(
			output.Reasoning.Text,
			output.Reasoning.Summary,
			reasoningPayload(&llm.ReasoningDelta{
				EventType:        "response.completed",
				ItemID:           output.Reasoning.ItemID,
				Status:           output.Reasoning.Status,
				Kind:             messageTraceThinkKindContent,
				EncryptedContent: output.Reasoning.EncryptedContent,
			}),
		)
	} else if traceRecorder != nil && strings.TrimSpace(extractedThink) != "" {
		traceRecorder.syncStructuredThink(extractedThink, "", nil)
	}
	if traceRecorder != nil {
		traceRecorder.completeUpstreamThink()
	}
	return assistantText
}

func syncUpstreamOutputTrace(traceRecorder *messageTraceRecorder, output *llm.GenerateOutput, runID string) (string, []model.ToolCall) {
	if output == nil {
		return "", nil
	}
	// 原生 server-side tools 是上游在同一次 Responses 调用内部完成的工具。
	// 当本轮没有本地函数调用时，先记录工具再记录最终 reasoning，避免 UI 看起来缺少工具后的最后一次思考。
	var serverToolRows []model.ToolCall
	if shouldSyncServerToolsBeforeThinking(output) {
		serverToolRows = syncUpstreamServerToolCalls(traceRecorder, output, runID)
		return syncUpstreamOutputThinking(traceRecorder, output), serverToolRows
	}
	assistantText := syncUpstreamOutputThinking(traceRecorder, output)
	serverToolRows = syncUpstreamServerToolCalls(traceRecorder, output, runID)
	return assistantText, serverToolRows
}

func outputReasoningContent(output *llm.GenerateOutput) string {
	if output == nil {
		return ""
	}
	if output.Reasoning != nil {
		if text := strings.TrimSpace(output.Reasoning.Text); text != "" {
			return text
		}
	}
	_, extractedThink := splitAssistantOutputThinkingContent(output.Text)
	return strings.TrimSpace(extractedThink)
}

func shouldSyncServerToolsBeforeThinking(output *llm.GenerateOutput) bool {
	return output != nil && len(output.ServerToolCalls) > 0 && len(output.ToolCalls) == 0
}

func syncUpstreamServerToolCalls(traceRecorder *messageTraceRecorder, output *llm.GenerateOutput, runID string) []model.ToolCall {
	if output == nil || len(output.ServerToolCalls) == 0 {
		return nil
	}
	rows := make([]model.ToolCall, 0, len(output.ServerToolCalls))
	for _, item := range output.ServerToolCalls {
		status := strings.TrimSpace(item.Status)
		switch status {
		case "", "completed":
			status = "success"
		case "in_progress", "queued":
			status = "streaming"
		}
		outputJSON := strings.TrimSpace(item.OutputJSON)
		if outputJSON == "" && isSearchServerToolCall(item) {
			outputJSON = citationsToolOutputJSON(output.Citations)
		}
		rows = append(rows, model.ToolCall{
			RunID:      strings.TrimSpace(runID),
			ToolCallID: strings.TrimSpace(item.ToolCallID),
			ToolType:   strings.TrimSpace(item.ToolType),
			ToolName:   strings.TrimSpace(item.ToolName),
			Status:     status,
			InputJSON:  strings.TrimSpace(item.ArgumentsJSON),
			OutputJSON: outputJSON,
			ErrorJSON:  strings.TrimSpace(item.ErrorJSON),
		})
	}
	if traceRecorder != nil {
		summary, markdown, payload := buildToolTrace(rows)
		traceRecorder.appendToolSection(summary, markdown, payload, messageTraceStatusCompleted)
	}
	return rows
}

func normalizeStreamServerToolStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "", "completed", "success":
		return "success"
	case "in_progress", "queued", "searching", "generating":
		return "streaming"
	case "failed", "error":
		return "error"
	default:
		return strings.TrimSpace(status)
	}
}

func traceStatusFromToolStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "streaming", "requested":
		return messageTraceStatusStreaming
	case "error", "failed":
		return messageTraceStatusError
	default:
		return messageTraceStatusCompleted
	}
}

func isSearchServerToolCall(item llm.ToolCall) bool {
	toolType := strings.ToLower(strings.TrimSpace(item.ToolType))
	toolName := strings.ToLower(strings.TrimSpace(item.ToolName))
	return strings.Contains(toolType, "search") || strings.Contains(toolName, "search")
}

func citationsToolOutputJSON(citations []string) string {
	if len(citations) == 0 {
		return ""
	}
	items := make([]map[string]string, 0, len(citations))
	for _, citation := range citations {
		if value := strings.TrimSpace(citation); value != "" {
			items = append(items, map[string]string{"url": value})
		}
	}
	if len(items) == 0 {
		return ""
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return ""
	}
	return string(payload)
}

func addLLMUsage(left llm.Usage, right llm.Usage) llm.Usage {
	return llm.Usage{
		InputTokens:        left.InputTokens + right.InputTokens,
		OutputTokens:       left.OutputTokens + right.OutputTokens,
		CacheReadTokens:    left.CacheReadTokens + right.CacheReadTokens,
		CacheWriteTokens:   left.CacheWriteTokens + right.CacheWriteTokens,
		CacheWrite5mTokens: left.CacheWrite5mTokens + right.CacheWrite5mTokens,
		CacheWrite1hTokens: left.CacheWrite1hTokens + right.CacheWrite1hTokens,
		ReasoningTokens:    left.ReasoningTokens + right.ReasoningTokens,
		Speed:              mergeLLMUsageSpeed(left.Speed, right.Speed),
		ServiceTier:        mergeLLMUsageServiceTier(left.ServiceTier, right.ServiceTier),
		RawUsageJSON:       llm.MergeRawUsageJSON(left.RawUsageJSON, right.RawUsageJSON),
	}
}

func diffLLMUsage(current llm.Usage, previous llm.Usage) llm.Usage {
	return llm.Usage{
		InputTokens:        nonNegativeTokenDelta(current.InputTokens, previous.InputTokens),
		OutputTokens:       nonNegativeTokenDelta(current.OutputTokens, previous.OutputTokens),
		CacheReadTokens:    nonNegativeTokenDelta(current.CacheReadTokens, previous.CacheReadTokens),
		CacheWriteTokens:   nonNegativeTokenDelta(current.CacheWriteTokens, previous.CacheWriteTokens),
		CacheWrite5mTokens: nonNegativeTokenDelta(current.CacheWrite5mTokens, previous.CacheWrite5mTokens),
		CacheWrite1hTokens: nonNegativeTokenDelta(current.CacheWrite1hTokens, previous.CacheWrite1hTokens),
		ReasoningTokens:    nonNegativeTokenDelta(current.ReasoningTokens, previous.ReasoningTokens),
		Speed:              strings.TrimSpace(current.Speed),
		ServiceTier:        strings.TrimSpace(current.ServiceTier),
		RawUsageJSON:       diffLLMUsageRawJSON(current.RawUsageJSON, previous.RawUsageJSON),
	}
}

func nonNegativeTokenDelta(current int64, previous int64) int64 {
	if current <= previous {
		return 0
	}
	return current - previous
}

func emitLLMUsageEvent(onEvent func(eventType string, payload map[string]interface{}) error, usage llm.Usage) error {
	if onEvent == nil || usage == (llm.Usage{}) {
		return nil
	}
	return onEvent("usage", map[string]interface{}{
		"input_tokens":       usage.InputTokens,
		"output_tokens":      usage.OutputTokens,
		"cache_read_tokens":  usage.CacheReadTokens,
		"cache_write_tokens": usage.CacheWriteTokens,
		"reasoning_tokens":   usage.ReasoningTokens,
	})
}

func addServerSideToolUsage(left map[string]int64, right map[string]int64) map[string]int64 {
	if len(left) == 0 && len(right) == 0 {
		return nil
	}
	result := make(map[string]int64, len(left)+len(right))
	for key, value := range left {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey != "" && value > 0 {
			result[normalizedKey] += value
		}
	}
	for key, value := range right {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey != "" && value > 0 {
			result[normalizedKey] += value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func diffLLMUsageRawJSON(current string, previous string) string {
	current = strings.TrimSpace(current)
	if current == "" || current == strings.TrimSpace(previous) {
		return ""
	}
	return current
}

func mergeLLMUsageSpeed(left string, right string) string {
	left = strings.TrimSpace(strings.ToLower(left))
	right = strings.TrimSpace(strings.ToLower(right))
	if left == "fast" || right == "fast" {
		return "fast"
	}
	if right != "" {
		return right
	}
	return left
}

func mergeLLMUsageServiceTier(left string, right string) string {
	left = strings.TrimSpace(strings.ToLower(left))
	right = strings.TrimSpace(strings.ToLower(right))
	if right != "" {
		return right
	}
	return left
}

func buildFinalToolSynthesisMessages(messages []llm.Message, instruction string) []llm.Message {
	result := make([]llm.Message, 0, len(messages)+1)
	result = append(result, messages...)
	result = append(result, llm.Message{
		Role:    "system",
		Content: strings.TrimSpace(instruction),
	})
	return result
}

// toolRunFinalAnswerMissing 判断工具循环在预算耗尽时是否只剩未执行的结构化工具调用。
// 工具禁用轮中被剥离的文本编码调用（TextToolCallsStripped）同样视为模型尚未收尾。
func toolRunFinalAnswerMissing(output *llm.GenerateOutput, toolLoopStarted bool, llmCallCount int, maxLLMCalls int, remainingToolCalls int) bool {
	if output == nil || !toolLoopStarted {
		return false
	}
	if len(output.ToolCalls) == 0 && !output.TextToolCallsStripped {
		return false
	}
	return llmCallCount >= maxLLMCalls || remainingToolCalls <= 0
}

// buildToolStageMergeInstruction 构造阶段合并轮指令：模型在无工具下总结已获信息与剩余工作，
// 系统随后开启新一轮工具预算继续，直到模型认为任务完成。
func buildToolStageMergeInstruction() string {
	return "The tool call budget for this run has been exhausted and you may not call tools in this round. " +
		"First write a concise stage summary (a few sentences): the key information you have obtained so far and the remaining work. " +
		"The system will grant a fresh tool budget after your summary so you can continue the remaining work. " +
		"If you already have enough information, produce the final answer directly instead of a summary."
}

// buildToolStageFinalizeInstruction 构造合并轮收尾引导：模型在禁用工具轮仍输出工具调用
// 语法时，明确告知其无法继续调用工具；若关键信息（如上传图片中的文字）无法读取，
// 直接向用户说明缺失信息并请求补充，而不是反复尝试无效操作。
func buildToolStageFinalizeInstruction() string {
	return "You still may not call tools in this round and no new tool budget is guaranteed. " +
		"If you have enough information, produce the final answer now in plain text. " +
		"If the required information cannot be obtained (for example text inside an uploaded image " +
		"that you cannot read), do not keep retrying tools: state clearly what information is missing " +
		"and what you need from the user, then stop."
}
