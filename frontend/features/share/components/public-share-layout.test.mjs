import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const workspaceShell = readFileSync(
  new URL("../../layouts/components/sections/workspace-shell.tsx", import.meta.url),
  "utf8",
);
const publicSharePage = readFileSync(new URL("./public-share-page.tsx", import.meta.url), "utf8");

test("anonymous shares have one viewport-bounded content scroller", () => {
  assert.match(
    workspaceShell,
    /if \(!accessToken\)[\s\S]*fixed inset-0 overflow-hidden[\s\S]*\{children\}/,
  );
  assert.match(publicSharePage, /<main className="h-full min-h-0 w-full overflow-y-auto/);
});
