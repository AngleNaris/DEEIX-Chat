import assert from "node:assert/strict";
import test from "node:test";

import {
  collectSavedArtifactTraceItems,
  parseRecalledEvidence,
  resolveRecalledEvidenceKind,
} from "./message-process-trace.ts";

test("recalled source types map to user-facing evidence kinds", () => {
  const cases = [
    ["skill", "skill"],
    ["tool_result", "tool"],
    ["native_tool_result", "tool"],
    ["doc_card", "card"],
    ["user_memory", "memory"],
    ["semantic_recall", "recall"],
    ["summary", "summary"],
    ["conversation_summary", "summary"],
    ["image", "image"],
    ["image_analysis", "image"],
  ];

  for (const [sourceType, expected] of cases) {
    assert.equal(resolveRecalledEvidenceKind(sourceType), expected);
  }
  assert.equal(resolveRecalledEvidenceKind("file_rag_chunk"), null);
  assert.equal(resolveRecalledEvidenceKind("file_rag_fallback"), null);
  assert.equal(resolveRecalledEvidenceKind("tool"), null);
  assert.equal(resolveRecalledEvidenceKind("unknown"), null);
});

test("recalled evidence keeps supported sources, trims values, and filters RAG duplicates", () => {
  const items = parseRecalledEvidence({
    blocks: [
      {
        sourceRefs: [
          { sourceType: " skill ", sourceID: " skill-1 ", title: " Image skill ", artifactID: 12 },
          { sourceType: "tool", sourceID: "tool-definition", title: "Browser" },
          { sourceType: "tool_result", sourceID: "tool-1", title: "Browser result", artifactID: 13 },
          { sourceType: "doc_card", sourceID: "card-1", title: "World setting" },
          { sourceType: "user_memory", sourceID: "memory-1", title: "User preference" },
          { sourceType: "semantic_recall", sourceID: "recall-1", title: "Earlier decision" },
          { sourceType: "summary", sourceID: "summary-1", title: "Conversation summary" },
          { sourceType: "image", sourceID: "image-1", title: "Screenshot analysis" },
          { sourceType: "file_rag_chunk", sourceID: "file-1", title: "Already shown as RAG" },
          { sourceType: "unknown", sourceID: "unknown-1", title: "Unknown source" },
          { sourceType: "skill", sourceID: "empty-title", title: " " },
        ],
      },
    ],
  });

  assert.deepEqual(
    items.map(({ kind, sourceID, title, artifactID }) => ({ kind, sourceID, title, artifactID })),
    [
      { kind: "skill", sourceID: "skill-1", title: "Image skill", artifactID: 12 },
      { kind: "tool", sourceID: "tool-1", title: "Browser result", artifactID: 13 },
      { kind: "card", sourceID: "card-1", title: "World setting", artifactID: undefined },
      { kind: "memory", sourceID: "memory-1", title: "User preference", artifactID: undefined },
      { kind: "recall", sourceID: "recall-1", title: "Earlier decision", artifactID: undefined },
      { kind: "summary", sourceID: "summary-1", title: "Conversation summary", artifactID: undefined },
      { kind: "image", sourceID: "image-1", title: "Screenshot analysis", artifactID: undefined },
    ],
  );
});

test("recalled evidence deduplicates aliases after normalization", () => {
  const items = parseRecalledEvidence({
    blocks: [
      {
        sourceRefs: [
          { sourceType: "summary", sourceID: "summary-1", title: "Rolling summary", artifactID: 4 },
          {
            sourceType: "conversation_summary",
            sourceID: "summary-1",
            title: "Rolling summary",
            artifactID: 8,
          },
          { sourceType: "tool_result", sourceID: "tool-1", title: "Search", artifactID: 0 },
          { sourceType: "native_tool_result", sourceID: "tool-1", title: "Search", artifactID: -1 },
        ],
      },
    ],
  });

  assert.equal(items.length, 2);
  assert.deepEqual(
    items.map(({ kind, artifactID }) => ({ kind, artifactID })),
    [
      { kind: "summary", artifactID: 4 },
      { kind: "tool", artifactID: undefined },
    ],
  );
});

test("saved artifact tool results move to the assistant body and deduplicate", () => {
  const artifactCall = {
    name: "platform_save_artifact",
    status: "completed",
    output_detail: JSON.stringify({
      artifact_id: "artifact-1",
      title: "Release page",
      kind: "html",
      share_url: "/share/artifact?artifact_id=share-1",
    }),
  };
  const payload = JSON.stringify({
    tool_calls: [
      artifactCall,
      { name: "search_artifact", status: "completed", output: JSON.stringify({ artifact_id: "ignored" }) },
      { name: "web_search", status: "completed", output: JSON.stringify({ title: "Search result" }) },
    ],
  });

  assert.deepEqual(collectSavedArtifactTraceItems([payload, payload, "invalid json"]), [
    {
      artifactID: "artifact-1",
      title: "Release page",
      kind: "html",
      shareURL: "/share/artifact?artifact_id=share-1",
    },
  ]);
});
