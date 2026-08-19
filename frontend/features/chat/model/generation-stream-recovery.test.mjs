import assert from "node:assert/strict";
import test from "node:test";

import { shouldStartGenerationResume } from "./generation-stream-recovery.ts";

test("active original generation prevents a resume stream", () => {
  assert.equal(
    shouldStartGenerationResume({
      conversationID: "conversation-1",
      pendingRunID: "run-1",
      generationActive: true,
      generationFailed: false,
    }),
    false,
  );
});

test("pending inactive generation can be resumed", () => {
  assert.equal(
    shouldStartGenerationResume({
      conversationID: "conversation-1",
      pendingRunID: "run-1",
      generationActive: false,
      generationFailed: false,
    }),
    true,
  );
});

test("failed generation is not resumed automatically", () => {
  assert.equal(
    shouldStartGenerationResume({
      conversationID: "conversation-1",
      pendingRunID: "run-1",
      generationActive: false,
      generationFailed: true,
    }),
    false,
  );
});

test("resume requires both conversation and run identifiers", () => {
  assert.equal(
    shouldStartGenerationResume({
      conversationID: " ",
      pendingRunID: "run-1",
      generationActive: false,
      generationFailed: false,
    }),
    false,
  );
  assert.equal(
    shouldStartGenerationResume({
      conversationID: "conversation-1",
      pendingRunID: "",
      generationActive: false,
      generationFailed: false,
    }),
    false,
  );
});
