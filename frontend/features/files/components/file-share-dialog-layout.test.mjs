import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const artifactPage = readFileSync(
  new URL("../../../app/(project)/artifacts/page.tsx", import.meta.url),
  "utf8",
);
const fileShareDialog = readFileSync(new URL("./file-share-dialog.tsx", import.meta.url), "utf8");

test("artifact page owns vertical scrolling inside the fixed application shell", () => {
  assert.match(artifactPage, /min-h-0[^"\n]*flex-1[^"\n]*overflow-y-auto/);
});

test("file share dialog keeps content visible and shows the complete share URL", () => {
  assert.match(fileShareDialog, /DialogContent\s+className="[^"]*flex[^"]*max-h-\[calc\(100svh-2rem\)\][^"]*flex-col/);
  assert.match(fileShareDialog, /min-w-0 flex-1 space-y-4 overflow-y-auto/);
  assert.match(fileShareDialog, /DialogFooter className="shrink-0/);
  assert.match(fileShareDialog, /className="min-w-0 flex-1 break-all text-xs leading-5 text-muted-foreground"/);
  assert.doesNotMatch(fileShareDialog, /flex-1 truncate text-xs text-muted-foreground/);
  assert.match(fileShareDialog, /onOpenAutoFocus=/);
  assert.match(fileShareDialog, /onCloseAutoFocus=/);
  assert.match(fileShareDialog, /restoreTarget\.focus\(\{ preventScroll: true \}\)/);
});
