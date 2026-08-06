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
)

// ReasoningEffortValid 判断档位是否合法。
func ReasoningEffortValid(level string) bool {
	switch level {
	case ReasoningEffortDefault, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh:
		return true
	}
	return false
}

// reasoningEffortParams 各协议族的思考强度参数映射：
// path 为 options 中的参数路径（点分），values 为档位 → 注入值。
// 仅支持 4 档的端点直通 xhigh；仅支持 3 档的端点（gemini_interactions）截断为 high。
var reasoningEffortParams = map[string]struct {
	path   string
	values map[string]string
}{
	llm.AdapterOpenAIChatCompletions: {
		path: "reasoning_effort",
		values: map[string]string{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "xhigh",
		},
	},
	llm.AdapterOpenRouterChat: {
		path: "reasoning_effort",
		values: map[string]string{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "xhigh",
		},
	},
	llm.AdapterOpenAIResponses: {
		path: "reasoning.effort",
		values: map[string]string{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "high",
		},
	},
	llm.AdapterOpenRouterResponses: {
		path: "reasoning.effort",
		values: map[string]string{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "high",
		},
	},
	llm.AdapterXAIResponses: {
		path: "reasoning.effort",
		values: map[string]string{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "high",
		},
	},
	llm.AdapterGeminiInteractions: {
		path: "generation_config.thinking_level",
		values: map[string]string{
			ReasoningEffortLow: "low", ReasoningEffortMedium: "medium",
			ReasoningEffortHigh: "high", ReasoningEffortXHigh: "high",
		},
	},
}

// ReasoningEffortProtocolSupported 判断协议端点是否支持思考强度参数。
func ReasoningEffortProtocolSupported(protocol string) bool {
	_, ok := reasoningEffortParams[protocol]
	return ok
}

// ReasoningEffortParamForProtocol 返回协议对应的思考强度参数路径与注入值。
// 协议不支持或档位非法时 ok 为 false。
func ReasoningEffortParamForProtocol(protocol, level string) (path string, value string, ok bool) {
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
	path, value, ok := ReasoningEffortParamForProtocol(route.Protocol, level)
	if !ok {
		return base
	}
	options := cloneModelOptionMap(base)
	setNestedOptionValue(options, path, value)
	return options
}
