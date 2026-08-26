import assert from "node:assert/strict";
import test from "node:test";

import {
  dequeueUpstreamThinkEvent,
  enqueueUpstreamThinkSegment,
} from "./upstream-think-buffer.ts";
import { mergeUpstreamThinkContent } from "./upstream-think-content.ts";
import {
  mergeLiveUpstreamThinkTrace,
  preserveRicherLiveUpstreamThinkTrace,
  shouldClearLiveUpstreamThinkTrace,
} from "./upstream-think-trace.ts";

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

test("a richer live reasoning trace survives a stale completion snapshot", () => {
  const base = {
    enabled: true,
    status: "completed",
    upstreamThink: { contentMarkdown: "partial", status: "completed" },
  };
  const live = {
    enabled: true,
    status: "streaming",
    upstreamThink: { contentMarkdown: "partial and still streaming", status: "streaming" },
  };

  assert.equal(
    preserveRicherLiveUpstreamThinkTrace(base, live)?.upstreamThink?.contentMarkdown,
    "partial and still streaming",
  );
  assert.equal(shouldClearLiveUpstreamThinkTrace(false, base, live), false);
});

test("live reasoning is cleared only after the persisted trace catches up", () => {
  const persisted = {
    enabled: true,
    status: "completed",
    upstreamThink: { contentMarkdown: "complete reasoning", status: "completed" },
  };
  const live = {
    enabled: true,
    status: "completed",
    upstreamThink: { contentMarkdown: "complete reasoning", status: "completed" },
  };

  assert.equal(shouldClearLiveUpstreamThinkTrace(true, persisted, live), false);
  assert.equal(shouldClearLiveUpstreamThinkTrace(false, persisted, live), true);
});

test("a richer persisted trace wins immediately over stale live reasoning", () => {
  const persisted = {
    enabled: true,
    status: "completed",
    upstreamThink: { roundID: "round_1", contentMarkdown: "complete reasoning", status: "completed" },
  };
  const live = {
    enabled: true,
    status: "streaming",
    upstreamThink: { roundID: "round_1", contentMarkdown: "partial", status: "streaming" },
  };

  assert.equal(
    mergeLiveUpstreamThinkTrace(persisted, live)?.upstreamThink?.contentMarkdown,
    "complete reasoning",
  );
});

test("a different live reasoning round replaces the previous persisted round", () => {
  const persisted = {
    enabled: true,
    status: "completed",
    upstreamThink: { roundID: "round_1", contentMarkdown: "long previous round", status: "completed" },
  };
  const live = {
    enabled: true,
    status: "streaming",
    upstreamThink: { roundID: "round_2", contentMarkdown: "new", status: "streaming" },
  };

  assert.equal(mergeLiveUpstreamThinkTrace(persisted, live)?.upstreamThink?.roundID, "round_2");
  assert.equal(preserveRicherLiveUpstreamThinkTrace(persisted, live)?.upstreamThink?.roundID, "round_2");
});

test("an equal-length live reasoning update replaces stale persisted content", () => {
  const persisted = {
    enabled: true,
    status: "completed",
    upstreamThink: { roundID: "round_1", contentMarkdown: "old text", status: "completed" },
  };
  const live = {
    enabled: true,
    status: "streaming",
    upstreamThink: { roundID: "round_1", contentMarkdown: "new text", status: "streaming" },
  };

  assert.equal(mergeLiveUpstreamThinkTrace(persisted, live)?.upstreamThink?.contentMarkdown, "new text");
  assert.equal(preserveRicherLiveUpstreamThinkTrace(persisted, live)?.upstreamThink?.contentMarkdown, "new text");
});
