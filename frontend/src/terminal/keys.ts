// Modifier handling for the on-screen key bar, kept free of the DOM so it can be
// tested as plain functions.
export interface Mods {
  ctrl: boolean;
  alt: boolean;
}
// modified applies armed modifiers to what is about to be sent. Ctrl maps a
// character to its control code the way a hardware keyboard does; for a cursor
// key both modifiers become the xterm parameter form (CSI 1;5D is Ctrl-Left).
export function modified(text: string, mods: Mods) {
  if (!mods.ctrl && !mods.alt) return text;
  const cursor = /^\x1b[[O]([ABCDHF])$/.exec(text);
  if (cursor)
    return `\x1b[1;${1 + (mods.alt ? 2 : 0) + (mods.ctrl ? 4 : 0)}${cursor[1]}`;
  let out = text;
  if (mods.ctrl && out.length === 1) {
    const code = out.toUpperCase().charCodeAt(0);
    if (out === " ") out = "\x00";
    else if (out === "?") out = "\x7f";
    else if (code >= 64 && code <= 95) out = String.fromCharCode(code - 64);
  }
  return mods.alt ? "\x1b" + out : out;
}
