import assert from "node:assert/strict";
import test from "node:test";

import React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { RoleIcon } from "./role-icon.ts";

function render(value) {
  return renderToStaticMarkup(React.createElement(RoleIcon, { value, className: "size-4" }));
}

test("role icon names render vectors instead of visible slug text", () => {
  const known = render("shield-check");
  assert.match(known, /<svg/);
  assert.doesNotMatch(known, />shield-check</);

  const unknown = render("unknown-icon-name");
  assert.match(unknown, /<svg/);
  assert.doesNotMatch(unknown, />unknown-icon-name</);
});

test("existing custom glyph values remain visible", () => {
  assert.match(render("🎵"), />🎵</);
});
