type BufferedUpstreamThinkEvent = {
  delta?: string;
  contentMarkdown?: string;
  roundID?: string;
  eventID?: string;
  kind?: string;
  status?: string;
  trace?: {
    enabled?: boolean;
  };
  groupRunID?: string;
  stepID?: string;
  attemptID?: string;
  actorMemberID?: string;
};

export type UpstreamThinkBufferSegment<TEvent extends BufferedUpstreamThinkEvent = BufferedUpstreamThinkEvent> = {
  event: TEvent;
  delta: string;
};

function normalizeKeyPart(value: string | undefined) {
  return value?.trim() || "";
}

function eventMergeKey(event: BufferedUpstreamThinkEvent) {
  return [
    event.roundID,
    event.eventID,
    event.kind,
    event.status,
    event.groupRunID,
    event.stepID,
    event.attemptID,
    event.actorMemberID,
  ]
    .map(normalizeKeyPart)
    .join("\u0000");
}

function isOrderedBarrier(event: BufferedUpstreamThinkEvent) {
  return Boolean(event.trace?.enabled) || typeof event.contentMarkdown === "string";
}

export function enqueueUpstreamThinkSegment<TEvent extends BufferedUpstreamThinkEvent>(
  segments: UpstreamThinkBufferSegment<TEvent>[],
  event: TEvent,
) {
  const delta = typeof event.delta === "string" ? event.delta : "";
  const barrier = isOrderedBarrier(event);
  const previous = segments.at(-1);
  if (
    !barrier &&
    delta &&
    previous &&
    previous.delta &&
    !isOrderedBarrier(previous.event) &&
    eventMergeKey(previous.event) === eventMergeKey(event)
  ) {
    previous.delta += delta;
    previous.event = { ...event, delta: "" };
    return;
  }
  segments.push({
    event: { ...event, delta: "" },
    delta: barrier ? "" : delta,
  });
}

export function dequeueUpstreamThinkEvent<TEvent extends BufferedUpstreamThinkEvent>(
  segments: UpstreamThinkBufferSegment<TEvent>[],
  maxDeltaChars: number,
): TEvent | null {
  const segment = segments[0];
  if (!segment) {
    return null;
  }
  if (!segment.delta) {
    segments.shift();
    return segment.event;
  }
  const size = Math.min(segment.delta.length, Math.max(1, maxDeltaChars));
  const delta = segment.delta.slice(0, size);
  segment.delta = segment.delta.slice(size);
  if (!segment.delta) {
    segments.shift();
  }
  return {
    ...segment.event,
    delta,
    contentMarkdown: undefined,
  };
}
