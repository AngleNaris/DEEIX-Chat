import assert from "node:assert/strict";
import test from "node:test";

import { syncPackageVersionContent } from "./sync-package-version.mjs";

test("matching package versions preserve CRLF bytes", () => {
  const current = '{\r\n  "name": "example",\r\n  "version": "0.3.6"\r\n}\r\n';

  assert.equal(syncPackageVersionContent(current, "0.3.6"), current);
});

test("updated package versions preserve the existing newline style", () => {
  const current = '{\r\n  "name": "example",\r\n  "version": "0.3.5"\r\n}\r\n';
  const updated = syncPackageVersionContent(current, "0.3.6");

  assert.equal(JSON.parse(updated).version, "0.3.6");
  assert.equal(updated.includes("\n") && !updated.includes("\r\n"), false);
  assert.equal(updated.endsWith("\r\n"), true);
});
