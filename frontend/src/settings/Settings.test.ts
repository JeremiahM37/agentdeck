import test from "node:test";
import assert from "node:assert/strict";
import { syncRetained } from "./Settings";
test("MCP conflict merge preserves draft and refreshes retained secrets", () => {
  const draft = {
    remote: {
      url: "https://new",
      headers: { Authorization: { __agentdeck_retained: "old" } },
      custom: "mine",
    },
  };
  const latest = {
    remote: {
      url: "https://other",
      headers: { Authorization: { __agentdeck_retained: "fresh" } },
      custom: "theirs",
    },
  };
  assert.deepEqual(syncRetained(draft, latest), {
    remote: {
      url: "https://new",
      headers: { Authorization: { __agentdeck_retained: "fresh" } },
      custom: "mine",
    },
  });
});
