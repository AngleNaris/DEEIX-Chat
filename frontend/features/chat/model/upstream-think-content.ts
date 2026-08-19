type UpstreamThinkContentEvent = {
  delta?: string;
  contentMarkdown?: string;
  roundID?: string;
};

export function mergeUpstreamThinkContent(
  previousContent: string,
  previousRoundID: string | undefined,
  event: UpstreamThinkContentEvent,
) {
  if (typeof event.contentMarkdown === "string") {
    return event.contentMarkdown;
  }
  if (typeof event.delta !== "string" || event.delta.length === 0) {
    return previousContent;
  }
  const currentRound = previousRoundID?.trim() || "";
  const nextRound = event.roundID?.trim() || "";
  if (currentRound && nextRound && currentRound !== nextRound) {
    return event.delta;
  }
  return `${previousContent}${event.delta}`;
}
