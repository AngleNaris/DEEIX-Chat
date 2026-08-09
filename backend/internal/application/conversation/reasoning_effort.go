package conversation

import (
	"context"
	"strings"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

// 思考强度语义档位（系统级统一语义，按 API 端点类型映射为具体参数）。
// 空串表示“未设置”，由调用方决定回退来源（用户全局默认或模型 defaultOptions）。
const (
	ReasoningEffortDefault = ""
	ReasoningEffortLow     = "low"
	ReasoningEffortMedium  = "medium"
	ReasoningEffortHigh    = "high"
	ReasoningEffortXHigh   = "xhigh"
	ReasoningEffortMax     = "max"
)

// ReasoningEffortValid 判断档位是否合法。
func ReasoningEffortValid(level string) bool {
	switch level {
	case ReasoningEffortDefault, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax:
		return true
	}
	return false
}

// reasoningEffortParams 各协议族的思考强度参数映射：
// path 为 options 中的参数路径（点分），values 为档位 → 注入值。
// chat_completions 直通 max；responses 系支持到 xhigh（max 截断为 xhigh，官方模型无 max 档）；
// gemini 截断为 high；anthropic 为预算制：注入 thinking 复合键（type=enabled + budget_tokens），发送时按 max_tokens 收敛。
var reasoningEffortParams = map[string]struct {
	path   string
	values map[string]interface{}
}{
	llm.AdapterOpenAIChatCompletions: {
		path: "reasoning_effort",
		values: map[string]interface{}{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "xhigh",
			ReasoningEffortMax: "max",
		},
	},
	llm.AdapterOpenRouterChat: {
		path: "reasoning_effort",
		values: map[string]interface{}{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "xhigh",
			ReasoningEffortMax: "max",
		},
	},
	llm.AdapterOpenAIResponses: {
		path: "reasoning.effort",
		values: map[string]interface{}{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "xhigh",
			ReasoningEffortMax: "xhigh",
		},
	},
	llm.AdapterOpenRouterResponses: {
		path: "reasoning.effort",
		values: map[string]interface{}{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "xhigh",
			ReasoningEffortMax: "xhigh",
		},
	},
	llm.AdapterXAIResponses: {
		path: "reasoning.effort",
		values: map[string]interface{}{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "xhigh",
			ReasoningEffortMax: "xhigh",
		},
	},
	llm.AdapterGeminiInteractions: {
		path: "generation_config.thinking_level",
		values: map[string]interface{}{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "high",
			ReasoningEffortMax: "high",
		},
	},
	// anthropic thinking 预算制：档位映射为 budget_tokens（type 固定 enabled）。
	llm.AdapterAnthropicMessages: {
		path: "thinking",
		values: map[string]interface{}{
			ReasoningEffortLow:    map[string]interface{}{"type": "enabled", "budget_tokens": 2048},
			ReasoningEffortMedium: map[string]interface{}{"type": "enabled", "budget_tokens": 4096},
			ReasoningEffortHigh:   map[string]interface{}{"type": "enabled", "budget_tokens": 8192},
			ReasoningEffortXHigh:  map[string]interface{}{"type": "enabled", "budget_tokens": 16384},
			ReasoningEffortMax:    map[string]interface{}{"type": "enabled", "budget_tokens": 32000},
		},
	},
}

// ReasoningEffortProtocolSupported 判断协议端点是否支持思考强度参数。
func ReasoningEffortProtocolSupported(protocol string) bool {
	_, ok := reasoningEffortParams[protocol]
	return ok
}

// ReasoningEffortParamForProtocol 返回协议对应的思考强度参数路径与注入值。
// 协议不支持或档位非法时 ok 为 false；值为 map 的协议（anthropic）为复合键注入。
func ReasoningEffortParamForProtocol(protocol, level string) (path string, value interface{}, ok bool) {
	entry, supported := reasoningEffortParams[protocol]
	if !supported {
		return "", "", false
	}
	value, ok = entry.values[level]
	if !ok {
		return "", "", false
	}
	return entry.path, value, true
}

// setNestedOptionValue 按点分路径写入嵌套 options map（如 "reasoning.effort" → {reasoning:{effort:...}}）。
func setNestedOptionValue(options map[string]interface{}, path string, value interface{}) {
	segments := splitModelOptionPath(path)
	if len(segments) == 0 {
		return
	}
	current := options
	for _, segment := range segments[:len(segments)-1] {
		next, ok := current[segment].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[segment] = next
		}
		current = next
	}
	current[segments[len(segments)-1]] = value
}

// resolveReasoningEffortLevel 解析最终生效的思考强度档位：输入指定 > 用户全局默认。
func (s *Service) resolveReasoningEffortLevel(ctx context.Context, userID uint, inputLevel string) string {
	level := strings.TrimSpace(inputLevel)
	if level != "" {
		return level
	}
	if value, err := s.getUserSettingCached(ctx, userID, "chat.default_reasoning_effort"); err == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

// modelOptionValueExists 判断 options 中指定点分路径是否已有值。
func modelOptionValueExists(options map[string]interface{}, path string) bool {
	segments := splitModelOptionPath(path)
	var current interface{} = options
	for _, segment := range segments {
		typed, ok := current.(map[string]interface{})
		if !ok {
			return false
		}
		next, ok := typed[segment]
		if !ok {
			return false
		}
		current = next
	}
	return true
}

// modelOptionIntValue 读取 options 中的整数值（JSON 解析后为 float64）。
func modelOptionIntValue(options map[string]interface{}, key string) (int, bool) {
	raw, ok := options[key]
	if !ok {
		return 0, false
	}
	switch typed := raw.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	}
	return 0, false
}

// clampAnthropicThinkingBudget 按 options 中 max_tokens/max_output_tokens 收敛 thinking 预算
// （Claude 要求 budget_tokens 小于 max_tokens）；无法满足时返回 nil，调用方跳过注入。
// 未显式设置 max_tokens 时走适配器默认（64000），无需收敛。
func clampAnthropicThinkingBudget(options map[string]interface{}, thinking map[string]interface{}) map[string]interface{} {
	budget, ok := modelOptionIntValue(thinking, "budget_tokens")
	if !ok {
		return thinking
	}
	maxTokens, hasMax := modelOptionIntValue(options, "max_output_tokens")
	if !hasMax {
		maxTokens, hasMax = modelOptionIntValue(options, "max_tokens")
	}
	if !hasMax {
		return thinking
	}
	if maxTokens < 2048 {
		return nil
	}
	clamped := min(budget, maxTokens-1024)
	if clamped < 1024 {
		return nil
	}
	next := make(map[string]interface{}, len(thinking))
	for key, value := range thinking {
		next[key] = value
	}
	next["budget_tokens"] = clamped
	return next
}

// applyReasoningEffortInjection 把思考强度写入 options 副本（不修改入参）。
// inputLevel 为空（普通会话，未显式指定档位）且 options 已含思考强度时保留现有选择，
// 避免覆盖 composer 的选择；inputLevel 非空（群组成员显式指定）时始终覆盖。
func applyReasoningEffortInjection(protocol, inputLevel, level string, base map[string]interface{}) map[string]interface{} {
	path, value, ok := ReasoningEffortParamForProtocol(protocol, level)
	if !ok {
		return base
	}
	if inputLevel == "" && modelOptionValueExists(base, path) {
		return base
	}
	options := cloneModelOptionMap(base)
	if thinking, isThinking := value.(map[string]interface{}); isThinking {
		clamped := clampAnthropicThinkingBudget(options, thinking)
		if clamped == nil {
			return options
		}
		setNestedOptionValue(options, path, clamped)
		return options
	}
	setNestedOptionValue(options, path, value)
	return options
}

// injectReasoningEffortOptions 按路由协议把思考强度参数写入 options 副本（不修改入参）。
// 协议不支持或最终档位为空时不注入；写入发生在白名单过滤之前，路径均在白名单内，
// 由 filterModelOptions 统一把关。
func (s *Service) injectReasoningEffortOptions(
	ctx context.Context,
	userID uint,
	inputLevel string,
	route *channel.ResolvedRoute,
	base map[string]interface{},
) map[string]interface{} {
	level := s.resolveReasoningEffortLevel(ctx, userID, inputLevel)
	if level == "" {
		return base
	}
	return applyReasoningEffortInjection(route.Protocol, inputLevel, level, base)
}
