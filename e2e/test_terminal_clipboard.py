"""Browser terminal clipboard flows, including HTTP fallback and in-app frames."""
import time

import pytest
from playwright.sync_api import expect

from test_terminal_tabs import attach, frame, ready
from test_terminal_workspace import capture, open_terminal, real_terminal


@pytest.fixture(params=[False, True], ids=["standalone", "in-app-frame"])
def terminal_view(request, page, real_terminal):
    t = real_terminal
    if request.param:
        page.goto(t["url"] + "/#sessions")
        attach(page, "Real terminal")
        view = frame(page, t["id"])
        ready(view)
    else:
        open_terminal(page, t)
        view = None
    host = view.locator("#agent-terminal") if view else page.locator("#agent-terminal")
    return t, view, host


def _body_eval(view, page, expression, arg=None):
    if view:
        return view.locator("body").evaluate(expression, arg)
    return page.evaluate(expression, arg)


@pytest.mark.parametrize("clipboard_mode", ["missing", "rejected"], ids=["missing-api", "rejected-api"])
def test_terminal_copy_falls_back_when_browser_clipboard_is_unavailable(
    page, terminal_view, clipboard_mode
):
    """Selection copy works without Clipboard API; bare Ctrl+C still interrupts."""
    t, view, host = terminal_view
    script = """(el, mode) => {
        if (mode === 'missing') {
            Object.defineProperty(Navigator.prototype, 'clipboard', {
                configurable: true, get() { return undefined; }
            });
        } else {
            navigator.clipboard.writeText = () => Promise.reject(
                new DOMException('denied', 'NotAllowedError'));
        }
        window.__execText = '';
        const original = Document.prototype.execCommand;
        Document.prototype.execCommand = function(command) {
            if (command === 'copy') {
                const target = document.activeElement;
                window.__execText = target?.value?.slice(target.selectionStart, target.selectionEnd) || '';
            }
            return original.apply(this, arguments);
        };
    }"""
    if view:
        view.locator("body").evaluate(script, clipboard_mode)
    else:
        page.evaluate(script.replace("(el, mode) =>", "mode =>"), clipboard_mode)
    host.click()
    page.keyboard.type("COPY_FALLBACK_MARKER")
    page.wait_for_timeout(250)
    rows = host.locator(".xterm-rows>div").filter(has_text="COPY_FALLBACK_MARKER")
    expect(rows).to_have_count(1)
    box = rows.first.bounding_box()
    # Select the rendered xterm row as a user would, then use the product shortcut.
    page.mouse.move(box["x"] + box["width"] - 2, box["y"] + box["height"] / 2)
    page.mouse.down()
    page.mouse.move(box["x"] + 2, box["y"] + box["height"] / 2, steps=20)
    page.mouse.up()
    page.keyboard.press("Control+C")
    copied = _body_eval(view, page, "() => window.__execText")
    assert "COPY_FALLBACK_MARKER" in copied
    # Verify the bytes through a normal browser paste target, rather than only
    # observing that execCommand was called.
    body = view.locator("body") if view else page.locator("body")
    body.evaluate("() => document.body.insertAdjacentHTML('beforeend', '<textarea id=clipboard-probe></textarea>')")
    body.locator("#clipboard-probe").focus()
    page.keyboard.press("Control+V")
    assert "COPY_FALLBACK_MARKER" in body.locator("#clipboard-probe").input_value()
    assert not host.locator("#notice").is_visible()

    # Clear the line, then Ctrl+C remains input to the shell and interrupts a
    # running command rather than being treated as a copy shortcut.
    host.click()
    page.keyboard.press("Control+U")
    page.keyboard.type("sleep 5")
    page.keyboard.press("Enter")
    page.wait_for_timeout(300)
    page.keyboard.press("Control+C")
    expect(host.locator(".xterm-screen")).to_contain_text("^C", timeout=3000)


def test_terminal_paste_uses_real_browser_clipboard_in_both_views(page, terminal_view):
    t, view, host = terminal_view
    page.context.grant_permissions(["clipboard-read", "clipboard-write"], origin=t["url"])
    command = "printf PASTE_CLIPBOARD_PROOF > paste-clipboard.txt"
    host.click()
    if view:
        view.locator("body").evaluate("(el, command) => navigator.clipboard.writeText(command)", command)
    else:
        page.evaluate("command => navigator.clipboard.writeText(command)", command)
    page.keyboard.press("Control+Shift+V")
    page.keyboard.press("Enter")
    for _ in range(50):
        if (t["root"] / "paste-clipboard.txt").exists():
            break
        time.sleep(0.1)
    assert (t["root"] / "paste-clipboard.txt").read_text() == "PASTE_CLIPBOARD_PROOF"
    assert "PASTE_CLIPBOARD_PROOF" in capture(t)


def test_terminal_copy_uses_selected_retained_history(page, terminal_view):
    t, view, host = terminal_view
    page.context.grant_permissions(["clipboard-read", "clipboard-write"], origin=t["url"])
    host.click()
    page.keyboard.type("printf RETAINED_COPY_MARKER")
    page.keyboard.press("Enter")
    expect(host.locator(".xterm-screen")).to_contain_text("RETAINED_COPY_MARKER", timeout=5000)
    pause = view.locator("#pause") if view else page.locator("#pause")
    frozen = view.locator("#agent-pane .frozen") if view else page.locator("#agent-pane .frozen")
    if not pause.is_visible():
        (view.locator("#terminal-tools > summary") if view else page.locator("#terminal-tools > summary")).click()
    pause.click()
    expect(frozen).to_be_visible()
    expect(frozen).to_contain_text("RETAINED_COPY_MARKER")
    frozen.select_text()
    page.keyboard.press("Control+Shift+C")
    page.wait_for_timeout(300)
    body = view.locator("body") if view else page.locator("body")
    body.evaluate("() => document.body.insertAdjacentHTML('beforeend', '<textarea id=clipboard-probe></textarea>')")
    body.locator("#clipboard-probe").focus()
    page.keyboard.press("Control+V")
    assert "RETAINED_COPY_MARKER" in body.locator("#clipboard-probe").input_value()


def test_terminal_command_copies_selected_text(page, terminal_view):
    """Command+C is the selection copy shortcut on macOS-style keyboards."""
    t, view, host = terminal_view
    page.context.grant_permissions(["clipboard-read", "clipboard-write"], origin=t["url"])
    host.click()
    page.keyboard.type("META_COPY_MARKER")
    page.wait_for_timeout(250)
    row = host.locator(".xterm-rows>div").filter(has_text="META_COPY_MARKER").first
    expect(row).to_be_visible()
    box = row.bounding_box()
    page.mouse.move(box["x"] + box["width"] - 2, box["y"] + box["height"] / 2)
    page.mouse.down()
    page.mouse.move(box["x"] + 2, box["y"] + box["height"] / 2, steps=20)
    page.mouse.up()
    page.keyboard.press("Meta+C")
    page.wait_for_timeout(250)
    body = view.locator("body") if view else page.locator("body")
    body.evaluate("() => document.body.insertAdjacentHTML('beforeend', '<textarea id=command-probe></textarea>')")
    body.locator("#command-probe").focus()
    page.keyboard.press("Control+V")
    assert "META_COPY_MARKER" in body.locator("#command-probe").input_value()
