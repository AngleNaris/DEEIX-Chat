import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { updateConversationStreamingOwner } from "./conversation-streaming-state.ts";

const globalStyles = readFileSync(new URL("../../../app/globals.css", import.meta.url), "utf8");
const sidebarConversationItem = readFileSync(
  new URL("../../../features/layouts/components/navigation/sidebar-conversation-item.tsx", import.meta.url),
  "utf8",
);
const recentList = readFileSync(
  new URL("../../../features/recent/components/sections/recent-list.tsx", import.meta.url),
  "utf8",
);

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

test("streaming titles sweep only their text while unread dots remain completion-only", () => {
  assert.match(globalStyles, /background-clip:\s*text/);
  assert.match(globalStyles, /-webkit-text-fill-color:\s*transparent/);
  assert.match(globalStyles, /animation:\s*trace-sweep-move/);
  assert.match(globalStyles, /currentColor\s+0%[\s\S]*currentColor\s+100%/);
  assert.match(globalStyles, /background-position:\s*100%\s+0[\s\S]*background-position:\s*0%\s+0/);
  assert.doesNotMatch(globalStyles, /background-position:\s*(?:130%|-30%)\s+0/);
  assert.doesNotMatch(globalStyles, /\.trace-sweep::after/);

  for (const source of [sidebarConversationItem, recentList]) {
    assert.match(source, /item\.hasUnread\s*&&\s*!streaming/);
    assert.match(source, /streaming\s*&&\s*"trace-sweep"/);
  }
});
