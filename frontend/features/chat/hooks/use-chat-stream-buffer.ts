"use client";

import * as React from "react";

import { clearLiveGroupRun, upsertLiveGroupRunThink } from "@/features/agent-groups/model/group-run-store";
import {
  dequeueUpstreamThinkEvent,
  enqueueUpstreamThinkSegment,
  type UpstreamThinkBufferSegment,
} from "@/features/chat/model/upstream-think-buffer";
import { clearLiveUpstreamThinkTrace, upsertLiveUpstreamThinkTrace } from "@/features/chat/model/upstream-think-store";
import type { PendingExchangeMap } from "@/features/chat/types/chat-runtime";
import { isGroupStreamAwareEvent } from "@/shared/api/conversation";
import type { StreamMessageEvent } from "@/shared/api/conversation.types";

const STREAM_TEXT_FLUSH_INTERVAL_MS = 50;
const STREAM_THINK_FLUSH_INTERVAL_MS = 40;
const STREAM_THINK_BASE_CHARS_PER_FLUSH = 48;
const STREAM_THINK_CATCHUP_THRESHOLD = 1024;
const STREAM_THINK_CATCHUP_CHARS_PER_FLUSH = 256;

type UpstreamThinkDeltaEvent = Extract<StreamMessageEvent, { type: "upstream_think_delta" }>;

type StreamBuffer = {
  runID: string | null;
  pendingText: string;
  textFrame: number | null;
  textTimeout: number | null;
  lastTextFlushAt: number;
  pendingThinkSegments: UpstreamThinkBufferSegment<UpstreamThinkDeltaEvent>[];
  thinkFrame: number | null;
  thinkTimeout: number | null;
  lastThinkFlushAt: number;
};

function createStreamBuffer(runID?: string): StreamBuffer {
  return {
    runID: runID?.trim() || null,
    pendingText: "",
    textFrame: null,
    textTimeout: null,
    lastTextFlushAt: 0,
    pendingThinkSegments: [],
    thinkFrame: null,
    thinkTimeout: null,
    lastThinkFlushAt: 0,
  };
}

function resolveThinkFlushSize(pendingLength: number) {
  if (pendingLength > STREAM_THINK_CATCHUP_THRESHOLD) {
    return Math.min(pendingLength, STREAM_THINK_CATCHUP_CHARS_PER_FLUSH);
  }
  return Math.min(pendingLength, STREAM_THINK_BASE_CHARS_PER_FLUSH);
}

function cancelBufferTimers(buffer: StreamBuffer) {
  if (buffer.textFrame !== null) {
    window.cancelAnimationFrame(buffer.textFrame);
  }
  if (buffer.textTimeout !== null) {
    window.clearTimeout(buffer.textTimeout);
  }
  if (buffer.thinkFrame !== null) {
    window.cancelAnimationFrame(buffer.thinkFrame);
  }
  if (buffer.thinkTimeout !== null) {
    window.clearTimeout(buffer.thinkTimeout);
  }
}

export function useChatStreamBuffer({
  setPendingExchanges,
}: {
  setPendingExchanges: React.Dispatch<React.SetStateAction<PendingExchangeMap>>;
}) {
  const buffersRef = React.useRef(new Map<string, StreamBuffer>());
  const scheduleThinkFlushRef = React.useRef<(exchangeKey: string) => void>(() => undefined);

  const emitUpstreamThinkEvent = React.useCallback((runID: string, event: UpstreamThinkDeltaEvent) => {
    if (isGroupStreamAwareEvent(event)) {
      upsertLiveGroupRunThink(runID, event);
    } else {
      upsertLiveUpstreamThinkTrace(runID, event);
    }
  }, []);

  const flushStreamText = React.useCallback((exchangeKey: string) => {
    const buffer = buffersRef.current.get(exchangeKey);
    if (!buffer) {
      return;
    }
    buffer.textFrame = null;
    buffer.lastTextFlushAt = performance.now();
    const pendingText = buffer.pendingText;
    if (!pendingText) {
      return;
    }
    buffer.pendingText = "";

    setPendingExchanges((current) => {
      const exchange = current[exchangeKey];
      if (!exchange) {
        return current;
      }
      return {
        ...current,
        [exchangeKey]: {
          ...exchange,
          assistantPending: false,
          assistantStreaming: true,
          assistantText: exchange.assistantText + pendingText,
        },
      };
    });
  }, [setPendingExchanges]);

  const flushUpstreamThink = React.useCallback((exchangeKey: string) => {
    const buffer = buffersRef.current.get(exchangeKey);
    if (!buffer) {
      return;
    }
    buffer.thinkFrame = null;
    buffer.lastThinkFlushAt = performance.now();
    if (!buffer.runID || buffer.pendingThinkSegments.length === 0) {
      return;
    }

    const pendingLength = buffer.pendingThinkSegments[0]?.delta.length ?? 0;
    const event = dequeueUpstreamThinkEvent(
      buffer.pendingThinkSegments,
      resolveThinkFlushSize(pendingLength),
    );
    if (!event) {
      return;
    }
    emitUpstreamThinkEvent(buffer.runID, event);

    if (buffer.pendingThinkSegments.length > 0) {
      scheduleThinkFlushRef.current(exchangeKey);
    }
  }, [emitUpstreamThinkEvent]);

  const scheduleStreamFlush = React.useCallback((exchangeKey: string) => {
    const buffer = buffersRef.current.get(exchangeKey);
    if (!buffer || buffer.textFrame !== null || buffer.textTimeout !== null) {
      return;
    }
    const elapsed = performance.now() - buffer.lastTextFlushAt;
    if (elapsed >= STREAM_TEXT_FLUSH_INTERVAL_MS) {
      buffer.textFrame = window.requestAnimationFrame(() => flushStreamText(exchangeKey));
      return;
    }
    buffer.textTimeout = window.setTimeout(() => {
      buffer.textTimeout = null;
      buffer.textFrame = window.requestAnimationFrame(() => flushStreamText(exchangeKey));
    }, STREAM_TEXT_FLUSH_INTERVAL_MS - elapsed);
  }, [flushStreamText]);

  const scheduleUpstreamThinkFlush = React.useCallback((exchangeKey: string) => {
    const buffer = buffersRef.current.get(exchangeKey);
    if (!buffer || buffer.thinkFrame !== null || buffer.thinkTimeout !== null) {
      return;
    }
    const elapsed = performance.now() - buffer.lastThinkFlushAt;
    if (elapsed >= STREAM_THINK_FLUSH_INTERVAL_MS) {
      buffer.thinkFrame = window.requestAnimationFrame(() => flushUpstreamThink(exchangeKey));
      return;
    }
    buffer.thinkTimeout = window.setTimeout(() => {
      buffer.thinkTimeout = null;
      buffer.thinkFrame = window.requestAnimationFrame(() => flushUpstreamThink(exchangeKey));
    }, STREAM_THINK_FLUSH_INTERVAL_MS - elapsed);
  }, [flushUpstreamThink]);

  React.useEffect(() => {
    scheduleThinkFlushRef.current = scheduleUpstreamThinkFlush;
  }, [scheduleUpstreamThinkFlush]);

  const enqueueStreamText = React.useCallback((exchangeKey: string, delta: string) => {
    const buffer = buffersRef.current.get(exchangeKey);
    if (!buffer || !delta) {
      return;
    }
    buffer.pendingText += delta;
    scheduleStreamFlush(exchangeKey);
  }, [scheduleStreamFlush]);

  const enqueueUpstreamThinkDelta = React.useCallback((exchangeKey: string, event: UpstreamThinkDeltaEvent) => {
    const buffer = buffersRef.current.get(exchangeKey);
    if (!buffer) {
      return;
    }
    enqueueUpstreamThinkSegment(buffer.pendingThinkSegments, event);
    scheduleUpstreamThinkFlush(exchangeKey);
  }, [scheduleUpstreamThinkFlush]);

  const startStream = React.useCallback((exchangeKey: string, runID?: string) => {
    const existing = buffersRef.current.get(exchangeKey);
    if (existing) {
      cancelBufferTimers(existing);
    }
    const buffer = createStreamBuffer(runID);
    buffersRef.current.set(exchangeKey, buffer);
    clearLiveUpstreamThinkTrace(buffer.runID);
    clearLiveGroupRun(buffer.runID);
  }, []);

  const flushStreamTextNow = React.useCallback((exchangeKey: string) => {
    const buffer = buffersRef.current.get(exchangeKey);
    if (!buffer) {
      return;
    }
    if (buffer.textFrame !== null) {
      window.cancelAnimationFrame(buffer.textFrame);
      buffer.textFrame = null;
    }
    if (buffer.textTimeout !== null) {
      window.clearTimeout(buffer.textTimeout);
      buffer.textTimeout = null;
    }
    flushStreamText(exchangeKey);
  }, [flushStreamText]);

  const flushUpstreamThinkNow = React.useCallback((exchangeKey: string) => {
    const buffer = buffersRef.current.get(exchangeKey);
    if (!buffer) {
      return;
    }
    if (buffer.thinkFrame !== null) {
      window.cancelAnimationFrame(buffer.thinkFrame);
      buffer.thinkFrame = null;
    }
    if (buffer.thinkTimeout !== null) {
      window.clearTimeout(buffer.thinkTimeout);
      buffer.thinkTimeout = null;
    }
    if (!buffer.runID || buffer.pendingThinkSegments.length === 0) {
      return;
    }
    let event = dequeueUpstreamThinkEvent(buffer.pendingThinkSegments, Number.MAX_SAFE_INTEGER);
    while (event) {
      emitUpstreamThinkEvent(buffer.runID, event);
      event = dequeueUpstreamThinkEvent(buffer.pendingThinkSegments, Number.MAX_SAFE_INTEGER);
    }
  }, [emitUpstreamThinkEvent]);

  const resetStreamBuffer = React.useCallback((exchangeKey?: string) => {
    if (exchangeKey) {
      const buffer = buffersRef.current.get(exchangeKey);
      if (!buffer) {
        return;
      }
      cancelBufferTimers(buffer);
      buffersRef.current.delete(exchangeKey);
      return;
    }
    for (const buffer of buffersRef.current.values()) {
      cancelBufferTimers(buffer);
    }
    buffersRef.current.clear();
  }, []);

  React.useEffect(
    () => () => {
      for (const buffer of buffersRef.current.values()) {
        clearLiveUpstreamThinkTrace(buffer.runID);
        clearLiveGroupRun(buffer.runID);
      }
      resetStreamBuffer();
    },
    [resetStreamBuffer],
  );

  return {
    enqueueUpstreamThinkDelta,
    enqueueStreamText,
    flushStreamTextNow,
    flushUpstreamThinkNow,
    resetStreamBuffer,
    startStream,
  };
}
