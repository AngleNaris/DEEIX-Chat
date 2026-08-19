import assert from "node:assert/strict";
import test from "node:test";

import {
  dequeueUpstreamThinkEvent,
  enqueueUpstreamThinkSegment,
} from "./upstream-think-buffer.ts";
import { mergeUpstreamThinkContent } from "./upstream-think-content.ts";

test("thinking deltas append within the same round", () => {
  assert.equal(
    mergeUpstreamThinkContent("first", "round_1", {
      delta: " second",
      roundID: "round_1",
    }),
    "first second",
  );
});

test("a new reasoning round starts from its own delta", () => {
  assert.equal(
    mergeUpstreamThinkContent("first round", "round_1", {
      delta: "second round",
      roundID: "round_2",
    }),
    "second round",
  );
});

test("an explicit reasoning snapshot replaces buffered content", () => {
  assert.equal(
    mergeUpstreamThinkContent("stale", "round_1", {
      contentMarkdown: "authoritative",
      roundID: "round_1",
    }),
    "authoritative",
  );
});

test("thinking buffer preserves round boundaries and event order", () => {
  const segments = [];
  enqueueUpstreamThinkSegment(segments, { delta: "A", roundID: "round_1", status: "streaming" });
  enqueueUpstreamThinkSegment(segments, { delta: "B", roundID: "round_2", status: "streaming" });
  enqueueUpstreamThinkSegment(segments, { delta: "C", roundID: "round_1", status: "streaming" });

  assert.deepEqual(
    [
      dequeueUpstreamThinkEvent(segments, 48),
      dequeueUpstreamThinkEvent(segments, 48),
      dequeueUpstreamThinkEvent(segments, 48),
    ].map((event) => [event?.roundID, event?.delta]),
    [
      ["round_1", "A"],
      ["round_2", "B"],
      ["round_1", "C"],
    ],
  );
});

test("thinking snapshots do not discard queued deltas", () => {
  const segments = [];
  enqueueUpstreamThinkSegment(segments, { delta: "before", roundID: "round_1", status: "streaming" });
  enqueueUpstreamThinkSegment(segments, {
    contentMarkdown: "snapshot",
    roundID: "round_1",
    status: "completed",
  });

  assert.deepEqual(dequeueUpstreamThinkEvent(segments, 48), {
    delta: "before",
    roundID: "round_1",
    status: "streaming",
    contentMarkdown: undefined,
  });
  assert.deepEqual(dequeueUpstreamThinkEvent(segments, 48), {
    contentMarkdown: "snapshot",
    delta: "",
    roundID: "round_1",
    status: "completed",
  });
});
