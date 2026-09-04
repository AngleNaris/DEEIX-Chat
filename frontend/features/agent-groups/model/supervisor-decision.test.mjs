import assert from "node:assert/strict";
import test from "node:test";

import { parseSupervisorDecision } from "./supervisor-decision.ts";

test("parses a fenced supervisor decision", () => {
  assert.deepEqual(parseSupervisorDecision('```json\n{"action":"finish","answer":"done"}\n```').decision, {
    action: "finish",
    answer: "done",
  });
});

test("uses the last valid decision from correction rounds", () => {
  const result = parseSupervisorDecision(
    '{"action":"delegate","memberID":"old"}\n{"action":"finish","answer":"final"}',
  );
  assert.deepEqual(result.decision, { action: "finish", answer: "final" });
});

test("identifies incomplete supervisor JSON without rendering it as prose", () => {
  const result = parseSupervisorDecision('{"action":"delegate","memberID":"member-1"');
  assert.equal(result.decision, null);
  assert.equal(result.hasStructuredCandidate, true);
});

test("keeps a balanced but invalid supervisor object available for markdown fallback", () => {
  const result = parseSupervisorDecision('{"action": }');
  assert.equal(result.decision, null);
  assert.equal(result.hasStructuredCandidate, false);
});

test("does not classify ordinary prose as supervisor JSON", () => {
  const result = parseSupervisorDecision("Please review the draft and summarize it.");
  assert.equal(result.decision, null);
  assert.equal(result.hasStructuredCandidate, false);
});
