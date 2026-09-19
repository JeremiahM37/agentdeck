import assert from "node:assert/strict";
import { test } from "node:test";
import { modified } from "./keys";

const none = { ctrl: false, alt: false };

test("an unarmed key is sent as typed", () => {
  assert.equal(modified("c", none), "c");
  assert.equal(modified("\x1b[D", none), "\x1b[D");
});

test("Ctrl maps a letter to its control code, as a hardware keyboard does", () => {
  const ctrl = { ctrl: true, alt: false };
  assert.equal(modified("c", ctrl), "\x03");
  assert.equal(modified("C", ctrl), "\x03");
  assert.equal(modified("d", ctrl), "\x04");
  assert.equal(modified("[", ctrl), "\x1b");
  assert.equal(modified(" ", ctrl), "\x00");
  assert.equal(modified("?", ctrl), "\x7f");
  // Nothing sensible to map: the character goes through rather than vanishing.
  assert.equal(modified("1", ctrl), "1");
});

test("Alt prefixes escape, and combines with Ctrl", () => {
  assert.equal(modified("b", { ctrl: false, alt: true }), "\x1bb");
  assert.equal(modified("c", { ctrl: true, alt: true }), "\x1b\x03");
});

test("a modified cursor key uses the xterm parameter form in either cursor mode", () => {
  assert.equal(modified("\x1b[D", { ctrl: true, alt: false }), "\x1b[1;5D");
  assert.equal(modified("\x1bOD", { ctrl: true, alt: false }), "\x1b[1;5D");
  assert.equal(modified("\x1b[C", { ctrl: false, alt: true }), "\x1b[1;3C");
  assert.equal(modified("\x1b[H", { ctrl: true, alt: true }), "\x1b[1;7H");
});
