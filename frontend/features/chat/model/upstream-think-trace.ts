import type { ChatMessageProcessTrace } from "../types/messages";

export function mergeLiveUpstreamThinkTrace(
  base: ChatMessageProcessTrace | undefined,
  live: ChatMessageProcessTrace | undefined,
) {
  if (!live?.upstreamThink) {
    return base;
  }
  const baseThink = base?.upstreamThink;
  const liveThink = live.upstreamThink;
  const sameRound = !baseThink?.roundID || !liveThink.roundID || baseThink.roundID === liveThink.roundID;
  const upstreamThink =
    sameRound && (baseThink?.contentMarkdown?.length ?? 0) >= (liveThink.contentMarkdown?.length ?? 0)
      ? baseThink
      : liveThink;
  return {
    enabled: true,
    status: live.status || base?.status || "streaming",
    process: base?.process,
    tools: base?.tools,
    upstreamThink,
    promptTrace: base?.promptTrace,
    events: base?.events,
  };
}

export function preserveRicherLiveUpstreamThinkTrace(
  base: ChatMessageProcessTrace | undefined,
  live: ChatMessageProcessTrace | undefined,
) {
  const baseContent = base?.upstreamThink?.contentMarkdown ?? "";
  const liveContent = live?.upstreamThink?.contentMarkdown ?? "";
  const sameRound =
    !base?.upstreamThink?.roundID ||
    !live?.upstreamThink?.roundID ||
    base.upstreamThink.roundID === live.upstreamThink.roundID;
  if (!live?.upstreamThink || !sameRound || liveContent.length <= baseContent.length) {
    return base ?? live;
  }
  return mergeLiveUpstreamThinkTrace(base, live);
}

export function shouldClearLiveUpstreamThinkTrace(
  isStreaming: boolean,
  base: ChatMessageProcessTrace | undefined,
  live: ChatMessageProcessTrace | undefined,
) {
  if (isStreaming || !base?.upstreamThink) {
    return false;
  }
  const baseContent = base.upstreamThink.contentMarkdown ?? "";
  const liveContent = live?.upstreamThink?.contentMarkdown ?? "";
  return liveContent.length <= baseContent.length;
}
