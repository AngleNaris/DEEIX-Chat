import assert from "node:assert/strict";
import test from "node:test";

import {
  isEmptyResumingGroupRunPlaceholder,
  shouldImportGroupRunDetail,
  shouldStartGroupRunDetailRecovery,
} from "./group-run-recovery.ts";

test("an empty resuming placeholder can recover persisted group details", () => {
  const placeholder = {
    groupRunID: "",
    status: "pending",
    steps: [],
    resuming: true,
  };

  assert.equal(isEmptyResumingGroupRunPlaceholder(placeholder), true);
  assert.equal(shouldStartGroupRunDetailRecovery(placeholder), true);
  assert.equal(shouldImportGroupRunDetail(placeholder), true);
});

test("a normal pending placeholder remains owned by the live stream", () => {
  const placeholder = {
    groupRunID: "",
    status: "pending",
    steps: [],
    resuming: false,
  };

  assert.equal(isEmptyResumingGroupRunPlaceholder(placeholder), false);
  assert.equal(shouldStartGroupRunDetailRecovery(placeholder), false);
  assert.equal(shouldImportGroupRunDetail(placeholder), false);
});

test("a populated live run wins if lookup races with stream events", () => {
  const liveRun = {
    groupRunID: "group-run-1",
    status: "running",
    steps: [{ stepID: "step-1" }],
    resuming: true,
  };

  assert.equal(isEmptyResumingGroupRunPlaceholder(liveRun), false);
  assert.equal(shouldStartGroupRunDetailRecovery(liveRun), false);
  assert.equal(shouldImportGroupRunDetail(liveRun), false);
});

test("a retrying run cannot be overwritten by a stale detail response", () => {
  assert.equal(
    shouldImportGroupRunDetail({
      groupRunID: "group-run-1",
      status: "running",
      steps: [{ stepID: "step-1" }],
      retrying: true,
    }),
    false,
  );
});

test("terminal runs remain eligible for a detail import", () => {
  assert.equal(
    shouldImportGroupRunDetail({
      groupRunID: "group-run-1",
      status: "completed",
      steps: [{ stepID: "step-1" }],
      resuming: false,
    }),
    true,
  );
});
