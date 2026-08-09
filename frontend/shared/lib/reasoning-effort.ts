import type { ConversationOptions } from "@/shared/api/conversation.types";

// 思考强度语义档位（系统级统一语义，与后端 conversation/reasoning_effort.go 保持一致）。
// 空串表示“未设置”，由调用方决定回退来源（用户全局默认或模型 defaultOptions）。
export const REASONING_EFFORT_DEFAULT = "";

// 从低到高排列的可选档位（语义顺序与字典序一致）。
export const REASONING_EFFORT_LEVELS = ["low", "medium", "high", "xhigh", "max"] as const;

export type ReasoningEffortLevelValue = typeof REASONING_EFFORT_LEVELS[number];

export type ReasoningEffortLevel = ReasoningEffortLevelValue | "";

// anthropic thinking 预算制：档位 → budget_tokens（发送时按 max_tokens 收敛）。
export const ANTHROPIC_REASONING_EFFORT_BUDGETS: Readonly<Record<ReasoningEffortLevelValue, number>> = {
  low: 2048,
  medium: 4096,
  high: 8192,
  xhigh: 16384,
  max: 32000,
};

export type ReasoningEffortProtocolMapping = {
  /** options 中的参数路径（点分），与后端推理注入路径一致。 */
  path: string;
  /** 该协议族支持的最高档位（仅支持 3 档的端点将 xhigh 截断为 high）。 */
  maxLevel: ReasoningEffortLevelValue;
  /** 显式档位词汇表（缺省按 maxLevel 截断，供分族词汇表使用）。 */
  levels?: readonly ReasoningEffortLevelValue[];
  /** 视觉配置中应隐藏的底层参数键（点分，含复合路径如 thinking.type）。 */
  optionKeys: readonly string[];
  /** 预算制协议的档位 → budget_tokens 映射（如 anthropic thinking）。 */
  budgets?: Readonly<Record<ReasoningEffortLevelValue, number>>;
};

// 各协议族的思考强度参数映射（与后端 reasoning_effort.go 保持一致）。
// maxLevel 决定选单最大档位：chat_completions 直通 max；responses 系截断到 xhigh
// （GPT-5.5 最高 xhigh，max 会报错）；gemini 截断到 high；anthropic 预算制到 max。
export const REASONING_EFFORT_PROTOCOL_PATHS: Readonly<Record<string, ReasoningEffortProtocolMapping>> = {
  openai_chat_completions: { path: "reasoning_effort", maxLevel: "max", optionKeys: ["reasoning_effort"] },
  openrouter_chat_completions: { path: "reasoning_effort", maxLevel: "max", optionKeys: ["reasoning_effort"] },
  openai_responses: { path: "reasoning.effort", maxLevel: "xhigh", optionKeys: ["reasoning.effort"] },
  openrouter_responses: { path: "reasoning.effort", maxLevel: "xhigh", optionKeys: ["reasoning.effort"] },
  xai_responses: { path: "reasoning.effort", maxLevel: "xhigh", optionKeys: ["reasoning.effort"] },
  gemini_interactions: { path: "generation_config.thinking_level", maxLevel: "high", optionKeys: ["generation_config.thinking_level"] },
  // anthropic 为预算制：档位经 thinking.type + thinking.budget_tokens 复合写入。
  anthropic_messages: {
    path: "thinking",
    maxLevel: "max",
    optionKeys: ["thinking.type", "thinking.budget_tokens"],
    budgets: ANTHROPIC_REASONING_EFFORT_BUDGETS,
  },
};

export function isReasoningEffortLevel(value: string): value is ReasoningEffortLevel {
  return value === REASONING_EFFORT_DEFAULT || (REASONING_EFFORT_LEVELS as readonly string[]).includes(value);
}

// resolveReasoningEffortForProtocols 取第一个支持思考强度的协议映射；
// 没有任何支持协议时返回 null（调用方应隐藏选择器）。
export function resolveReasoningEffortForProtocols(
  protocols: readonly string[],
): ReasoningEffortProtocolMapping | null {
  for (const protocol of protocols) {
    const mapping = REASONING_EFFORT_PROTOCOL_PATHS[protocol];
    if (mapping) {
      return mapping;
    }
  }
  return null;
}

// resolveReasoningEffortProtocol 取第一个支持思考强度的协议 key；无则 null。
export function resolveReasoningEffortProtocol(protocols: readonly string[]): string | null {
  for (const protocol of protocols) {
    if (REASONING_EFFORT_PROTOCOL_PATHS[protocol]) {
      return protocol;
    }
  }
  return null;
}

// 该协议族可选的档位列表（显式词汇表优先；否则按 maxLevel 截断，如 xhigh 不支持时只到 high）。
export function reasoningEffortLevelsForMapping(
  mapping: ReasoningEffortProtocolMapping,
): readonly ReasoningEffortLevelValue[] {
  if (mapping.levels && mapping.levels.length > 0) {
    return mapping.levels;
  }
  const maxIndex = REASONING_EFFORT_LEVELS.indexOf(mapping.maxLevel);
  return maxIndex >= 0 ? REASONING_EFFORT_LEVELS.slice(0, maxIndex + 1) : [];
}

export function getModelOptionNestedValue(options: ConversationOptions, path: string): unknown {
  const segments = path.split(".").map((segment) => segment.trim()).filter(Boolean);
  if (segments.length === 0) {
    return undefined;
  }
  let current: unknown = options;
  for (const segment of segments) {
    if (current === null || typeof current !== "object" || Array.isArray(current)) {
      return undefined;
    }
    current = (current as Record<string, unknown>)[segment];
  }
  return current;
}

export function setModelOptionNestedValue(
  options: ConversationOptions,
  path: string,
  value: unknown,
): ConversationOptions {
  const segments = path.split(".").map((segment) => segment.trim()).filter(Boolean);
  if (segments.length === 0) {
    return options;
  }
  const result: ConversationOptions = { ...options };
  let current = result;
  for (const segment of segments.slice(0, -1)) {
    const existing = current[segment];
    const next = existing !== null && typeof existing === "object" && !Array.isArray(existing)
      ? { ...(existing as ConversationOptions) }
      : {};
    current[segment] = next;
    current = next;
  }
  current[segments[segments.length - 1]] = value;
  return result;
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

// nearestBudgetLevel 按预算反查最近档位（预算制协议，如 anthropic thinking）。
function nearestBudgetLevel(mapping: ReasoningEffortProtocolMapping, budget: number): string {
  const levels = reasoningEffortLevelsForMapping(mapping);
  let nearest = REASONING_EFFORT_DEFAULT;
  let nearestDistance = Number.POSITIVE_INFINITY;
  for (const level of levels) {
    const levelBudget = mapping.budgets?.[level];
    if (typeof levelBudget !== "number") {
      continue;
    }
    const distance = Math.abs(levelBudget - budget);
    if (distance < nearestDistance) {
      nearest = level;
      nearestDistance = distance;
    }
  }
  return nearest;
}

// getReasoningEffortOptionValue 按协议读取 options 中的思考强度当前值（无协议或未设置返回 ""）。
// 预算制协议（anthropic）读取 thinking 复合键：type 为 enabled 时按 budget_tokens 反查档位。
export function getReasoningEffortOptionValue(
  protocol: string,
  options: ConversationOptions,
): string {
  const mapping = REASONING_EFFORT_PROTOCOL_PATHS[protocol];
  if (!mapping) {
    return "";
  }
  const value = getModelOptionNestedValue(options, mapping.path);
  if (mapping.budgets) {
    if (!isPlainObject(value) || value.type !== "enabled") {
      return "";
    }
    const budget = typeof value.budget_tokens === "number"
      ? value.budget_tokens
      : Number(value.budget_tokens);
    return Number.isFinite(budget) ? nearestBudgetLevel(mapping, budget) : "";
  }
  return typeof value === "string" ? value : "";
}

// setReasoningEffortOptionValue 写入思考强度档位；档位为空时清除对应路径（继承默认）。
// 预算制协议（anthropic）写入 thinking.type + thinking.budget_tokens，清空时仅移除这两个键。
export function setReasoningEffortOptionValue(
  protocol: string,
  options: ConversationOptions,
  level: string,
): ConversationOptions {
  const mapping = REASONING_EFFORT_PROTOCOL_PATHS[protocol];
  if (!mapping) {
    return options;
  }
  if (mapping.budgets) {
    if (!level.trim()) {
      const thinking = getModelOptionNestedValue(options, "thinking");
      if (!isPlainObject(thinking)) {
        return options;
      }
      const next: Record<string, unknown> = { ...thinking };
      delete next.type;
      delete next.budget_tokens;
      if (Object.keys(next).length === 0) {
        return removeModelOptionPath(options, "thinking");
      }
      return setModelOptionNestedValue(options, "thinking", next);
    }
    const budget = mapping.budgets[level as ReasoningEffortLevelValue];
    if (typeof budget !== "number") {
      return options;
    }
    const thinking = getModelOptionNestedValue(options, "thinking");
    const next = {
      ...(isPlainObject(thinking) ? thinking : {}),
      type: "enabled",
      budget_tokens: budget,
    };
    return setModelOptionNestedValue(options, "thinking", next);
  }
  if (!level.trim()) {
    return removeModelOptionPath(options, mapping.path);
  }
  return setModelOptionNestedValue(options, mapping.path, level);
}

function removeModelOptionPath(options: ConversationOptions, path: string): ConversationOptions {
  const segments = path.split(".").map((segment) => segment.trim()).filter(Boolean);
  if (segments.length === 0) {
    return options;
  }
  const result: ConversationOptions = { ...options };
  let current = result;
  const parentSegments = segments.slice(0, -1);
  for (const segment of parentSegments) {
    const existing = current[segment];
    if (existing === null || typeof existing !== "object" || Array.isArray(existing)) {
      return options;
    }
    current = existing as ConversationOptions;
  }
  delete current[segments[segments.length - 1]];
  if (parentSegments.length > 0) {
    let cleanup: ConversationOptions | undefined = result;
    let pathIndex = 0;
    while (cleanup && pathIndex < parentSegments.length) {
      const next = cleanup[parentSegments[pathIndex]];
      if (next === null || typeof next !== "object" || Array.isArray(next)) {
        break;
      }
      const nextObject = next as ConversationOptions;
      if (Object.keys(nextObject).length === 0) {
        delete cleanup[parentSegments[pathIndex]];
      }
      cleanup = Object.keys(nextObject).length === 0 ? undefined : nextObject;
      pathIndex += 1;
    }
  }
  return result;
}
