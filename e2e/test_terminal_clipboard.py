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


def _body_eval(view, page, expression):
    return view.locator("body").evaluate(expression) if view else page.evaluate(expression)


def test_terminal_copy_falls_back_when_browser_clipboard_is_unavailable(
    page, terminal_view
):
    """Ctrl+Shift+C still copies from an HTTP terminal without Clipboard API."""
    t, view, host = terminal_view
    _body_eval(
        view,
        page,
        """() => {
            Object.defineProperty(Navigator.prototype, 'clipboard', {
                configurable: true, get() { return undefined; }
            });
            window.__execText = '';
            const original = Document.prototype.execCommand;
            Document.prototype.execCommand = function(command) {
                if (command === 'copy') {
                    const el = document.activeElement;
                    window.__execText = el?.value?.slice(el.selectionStart, el.selectionEnd) || '';
                }
                return original.apply(this, arguments);
            };
        }""",
    )
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
    page.keyboard.press("Control+Shift+C")
    copied = _body_eval(view, page, "() => window.__execText")
    assert "COPY_FALLBACK_MARKER" in copied
    assert not host.locator("#notice").is_visible()

    # Ctrl+C remains input to the shell and interrupts a running command.
    host.click()
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
