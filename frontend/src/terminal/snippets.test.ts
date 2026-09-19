import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultSnippets, parseSnippets, snippetBytes } from "./snippets";

test("a phone that has never saved any gets the agent replies", () => {
  assert.deepEqual(parseSnippets(null), defaultSnippets);
  assert.ok(defaultSnippets.some((s) => s.text === "continue" && s.enter));
});

test("an emptied list stays empty instead of growing the defaults back", () => {
  assert.deepEqual(parseSnippets("[]"), []);
});

test("junk in storage falls back rather than breaking the key bar", () => {
  assert.deepEqual(parseSnippets("{not json"), defaultSnippets);
  assert.deepEqual(parseSnippets('{"a":1}'), defaultSnippets);
  assert.deepEqual(parseSnippets('[{"text":""},{"nope":1},null,{"text":"ok"}]'), [
    { text: "ok", enter: true },
  ]);
});

test("Enter is a carriage return, and only when asked for", () => {
  assert.equal(snippetBytes({ text: "y", enter: true }), "y\r");
  assert.equal(snippetBytes({ text: "git commit -m ", enter: false }), "git commit -m ");
});
