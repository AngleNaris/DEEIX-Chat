import assert from "node:assert/strict";
import test from "node:test";

import {
  resolveAgentGroupSubmissionTarget,
  resolveConversationRequestModel,
  resolveConversationSubmissionModels,
} from "./conversation-request-model.ts";

test("new agent group conversations are recognized before a conversation DTO exists", () => {
  assert.equal(resolveAgentGroupSubmissionTarget(null, true), true);
});

test("persisted and queued conversations use their own agent group binding", () => {
  assert.equal(resolveAgentGroupSubmissionTarget({ agentGroupID: " group-1 " }, false), true);
  assert.equal(resolveAgentGroupSubmissionTarget({ agentGroupID: "" }, true), false);
  assert.equal(resolveAgentGroupSubmissionTarget({ agentGroupID: null }, true), false);
});

test("first agent group message clears every model-bearing submission phase", () => {
  assert.deepEqual(
    resolveConversationSubmissionModels("openai/gpt-5", true),
    {
      optimisticMessageModel: "",
      createConversationModel: "",
      streamRequestModel: "",
    },
  );
});

test("existing and queued agent group messages also clear stale selected models", () => {
  for (const selectedModel of ["openai/gpt-5", "  anthropic/claude  ", ""]) {
    const models = resolveConversationSubmissionModels(selectedModel, true);
    assert.deepEqual(Object.values(models), ["", "", ""]);
  }
});

test("ordinary messages preserve one normalized model across all phases", () => {
  assert.deepEqual(
    resolveConversationSubmissionModels("  openai/gpt-5  ", false),
    {
      optimisticMessageModel: "openai/gpt-5",
      createConversationModel: "openai/gpt-5",
      streamRequestModel: "openai/gpt-5",
    },
  );
});

test("request model resolution remains compatible with empty ordinary selections", () => {
  assert.equal(resolveConversationRequestModel("   ", false), "");
});
