import assert from "node:assert/strict";
import test from "node:test";

import { parsePlatformToolApprovalFromOutput } from "./platform-tool-approval.ts";

test("approval tool output parser maps pending and preserves terminal states", () => {
  assert.deepEqual(
    parsePlatformToolApprovalFromOutput(
      JSON.stringify({ status: "pending_approval", approval_id: "pending-1", tool: "save_memory" }),
    ),
    { approval_id: "pending-1", tool: "save_memory", arguments: {}, status: "pending" },
  );
  for (const status of ["approved", "rejected", "failed", "expired"]) {
    assert.equal(
      parsePlatformToolApprovalFromOutput(
        JSON.stringify({ status, approval_id: `${status}-1`, tool: "save_memory", arguments: { key: "k" } }),
      )?.status,
      status,
    );
  }
});

test("approval tool output parser rejects malformed or unrelated payloads", () => {
  assert.equal(parsePlatformToolApprovalFromOutput(undefined), null);
  assert.equal(parsePlatformToolApprovalFromOutput("not-json"), null);
  assert.equal(parsePlatformToolApprovalFromOutput(JSON.stringify({ status: "approved" })), null);
  assert.equal(parsePlatformToolApprovalFromOutput(JSON.stringify({ status: "success", approval_id: "other" })), null);
});
