import assert from "node:assert/strict";
import test from "node:test";

import {
  collectPendingPlatformApprovalIDs,
  initializePlatformApprovalDisplayStates,
  resolvePlatformToolApprovalDisplayState,
  resolvePlatformToolApprovalLookupFailure,
} from "./platform-tool-approval-state.ts";

test("approval status reconciliation preserves terminal states", () => {
  assert.equal(resolvePlatformToolApprovalDisplayState("pending"), "pending");
  assert.equal(resolvePlatformToolApprovalDisplayState("approved"), "approved");
  assert.equal(resolvePlatformToolApprovalDisplayState("rejected"), "rejected");
  assert.equal(resolvePlatformToolApprovalDisplayState("failed"), "failed");
  assert.equal(resolvePlatformToolApprovalDisplayState("expired"), "expired");
  assert.equal(resolvePlatformToolApprovalDisplayState(undefined), "pending");
});

test("only pending trace approvals require a runtime lookup", () => {
  const approvals = [
    { approval_id: "pending", status: "pending" },
    { approval_id: "approved", status: "approved" },
    { approval_id: "rejected", status: "rejected" },
    { approval_id: "failed", status: "failed" },
  ];
  assert.deepEqual(collectPendingPlatformApprovalIDs(approvals), ["pending"]);
  assert.deepEqual(initializePlatformApprovalDisplayStates(approvals, {}), {
    pending: "checking",
    approved: "approved",
    rejected: "rejected",
    failed: "failed",
  });
});

test("only a missing approval is treated as expired", () => {
  assert.equal(resolvePlatformToolApprovalLookupFailure(404), "expired");
  assert.equal(resolvePlatformToolApprovalLookupFailure(500), "pending");
  assert.equal(resolvePlatformToolApprovalLookupFailure(undefined), "pending");
});
