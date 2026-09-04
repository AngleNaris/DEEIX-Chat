import assert from "node:assert/strict";
import test from "node:test";

import {
  isTerminalAttemptStatus,
  shouldApplyGroupAttemptEvent,
} from "./group-run-event-policy.ts";

test("terminal attempts ignore replayed mutations", () => {
  assert.equal(isTerminalAttemptStatus("success"), true);
  assert.equal(
    shouldApplyGroupAttemptEvent({ stepStatus: "success", attemptStatus: "success" }, "group_step_output_delta"),
    false,
  );
});

test("terminal steps reject stale starts but allow a new retry attempt", () => {
  assert.equal(
    shouldApplyGroupAttemptEvent({ stepStatus: "success", attemptStatus: "pending" }, "group_step_started"),
    false,
  );
  assert.equal(
    shouldApplyGroupAttemptEvent({ stepStatus: "success", attemptStatus: "pending" }, "group_step_retry_started"),
    true,
  );
});

test("detail snapshots wait for their started event before accepting deltas", () => {
  const policy = { stepStatus: "running", attemptStatus: "running", detailSnapshot: true };
  assert.equal(shouldApplyGroupAttemptEvent(policy, "group_step_output_delta"), false);
  assert.equal(shouldApplyGroupAttemptEvent(policy, "upstream_think_delta"), false);
  assert.equal(shouldApplyGroupAttemptEvent(policy, "group_step_started"), true);
  assert.equal(shouldApplyGroupAttemptEvent(policy, "group_step_completed"), true);
  assert.equal(shouldApplyGroupAttemptEvent(policy, "group_step_failed"), true);
});
