import type { ConversationOptions } from "@/shared/api/conversation.types";

// 思考强度语义档位（系统级统一语义，与后端 conversation/reasoning_effort.go 保持一致）。
// 空串表示“未设置”，由调用方决定回退来源（用户全局默认或模型 defaultOptions）。
export const REASONING_EFFORT_DEFAULT = "";

// 从低到高排列的可选档位（语义顺序与字典序一致）。
export const REASONING_EFFORT_LEVELS = ["low", "medium", "high", "xhigh"] as const;

export type ReasoningEffortLevel = typeof REASONING_EFFORT_LEVELS[number] | "";

export type ReasoningEffortProtocolMapping = {
  /** options 中的参数路径（点分），与后端推理注入路径一致。 */
  path: string;
  /** 该协议族支持的最高档位（仅支持 3 档的端点将 xhigh 截断为 high）。 */
  maxLevel: typeof REASONING_EFFORT_LEVELS[number];
};

// 各协议族的思考强度参数映射（与后端 reasoning_effort.go 保持一致）。
export const REASONING_EFFORT_PROTOCOL_PATHS: Readonly<Record<string, ReasoningEffortProtocolMapping>> = {
  openai_chat_completions: { path: "reasoning_effort", maxLevel: "xhigh" },
  openrouter_chat_completions: { path: "reasoning_effort", maxLevel: "xhigh" },
  openai_responses: { path: "reasoning.effort", maxLevel: "high" },
  openrouter_responses: { path: "reasoning.effort", maxLevel: "high" },
  xai_responses: { path: "reasoning.effort", maxLevel: "high" },
  gemini_interactions: { path: "generation_config.thinking_level", maxLevel: "high" },
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

// 该协议族可选的档位列表（按 maxLevel 截断，如 xhigh 不支持时只到 high）。
export function reasoningEffortLevelsForMapping(
  mapping: ReasoningEffortProtocolMapping,
): readonly typeof REASONING_EFFORT_LEVELS[number][] {
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

// getReasoningEffortOptionValue 按协议读取 options 中的思考强度当前值（无协议或未设置返回 ""）。
export function getReasoningEffortOptionValue(
  protocol: string,
  options: ConversationOptions,
): string {
  const mapping = REASONING_EFFORT_PROTOCOL_PATHS[protocol];
  if (!mapping) {
    return "";
  }
  const value = getModelOptionNestedValue(options, mapping.path);
  return typeof value === "string" ? value : "";
}

// setReasoningEffortOptionValue 写入思考强度档位；档位为空时清除对应路径（继承默认）。
export function setReasoningEffortOptionValue(
  protocol: string,
  options: ConversationOptions,
  level: string,
): ConversationOptions {
  const mapping = REASONING_EFFORT_PROTOCOL_PATHS[protocol];
  if (!mapping) {
    return options;
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
