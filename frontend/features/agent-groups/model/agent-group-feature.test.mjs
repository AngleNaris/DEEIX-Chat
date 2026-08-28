import assert from "node:assert/strict";
import test from "node:test";

import {
  filterAgentGroupNavigationItems,
  resolveAgentGroupFeatureAccess,
} from "./agent-group-feature.ts";

test("only an enabled feature exposes user navigation and list requests", () => {
  assert.deepEqual(resolveAgentGroupFeatureAccess("enabled"), {
    allowListRequests: true,
    redirectDirectRoute: false,
    showNavigation: true,
  });
  assert.deepEqual(resolveAgentGroupFeatureAccess("loading"), {
    allowListRequests: false,
    redirectDirectRoute: false,
    showNavigation: false,
  });
  for (const status of ["disabled", "error"]) {
    assert.deepEqual(resolveAgentGroupFeatureAccess(status), {
      allowListRequests: false,
      redirectDirectRoute: true,
      showNavigation: false,
    });
  }
});

test("navigation filtering removes only the agent group user entry", () => {
  const items = [{ id: "newChat" }, { id: "agentGroups" }, { id: "files" }];
  assert.deepEqual(filterAgentGroupNavigationItems(items, false), [
    { id: "newChat" },
    { id: "files" },
  ]);
  assert.deepEqual(filterAgentGroupNavigationItems(items, true), items);
});
