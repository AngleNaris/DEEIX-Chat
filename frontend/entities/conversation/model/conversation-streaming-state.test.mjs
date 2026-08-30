import assert from "node:assert/strict";
import test from "node:test";

import { updateConversationStreamingOwner } from "./conversation-streaming-state.ts";

test("a conversation remains streaming until its final run owner finishes", () => {
  const owners = new Map();

  assert.equal(updateConversationStreamingOwner(owners, "conversation-1", "run-1", true), true);
  assert.equal(updateConversationStreamingOwner(owners, "conversation-1", "run-2", true), true);
  assert.equal(updateConversationStreamingOwner(owners, "conversation-1", "run-1", false), true);
  assert.equal(updateConversationStreamingOwner(owners, "conversation-1", "run-2", false), false);
});

test("run owners are isolated by conversation", () => {
  const owners = new Map();

  updateConversationStreamingOwner(owners, "conversation-1", "run-1", true);
  updateConversationStreamingOwner(owners, "conversation-2", "run-2", true);

  assert.equal(updateConversationStreamingOwner(owners, "conversation-1", "run-1", false), false);
  assert.equal(owners.has("conversation-2"), true);
});
