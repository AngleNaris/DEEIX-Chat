import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const badge = readFileSync(new URL("./badge.tsx", import.meta.url), "utf8");

test("badges follow the active theme radius", () => {
  assert.match(badge, /rounded-\[var\(--radius\)\]/);
  assert.doesNotMatch(badge, /rounded-full/);
});
