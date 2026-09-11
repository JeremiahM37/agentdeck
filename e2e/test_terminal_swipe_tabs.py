"""Real mobile terminal-tab gestures against two live tmux sessions."""
import subprocess

import pytest
from playwright.sync_api import expect

from test_terminal_workspace import real_terminal


def _touch_swipe(page, x1, y1, x2, y2):
    _touch_path(page, [(x1, y1), (x2, y2)])


def _touch_path(page, points):
    cdp = page.context.new_cdp_session(page)
    x1, y1 = points[0]
    cdp.send("Input.dispatchTouchEvent", {"type": "touchStart",
        "touchPoints": [{"x": x1, "y": y1, "id": 1}]})
    for x, y in points[1:]:
        cdp.send("Input.dispatchTouchEvent", {"type": "touchMove",
            "touchPoints": [{"x": x, "y": y, "id": 1}]})
    cdp.send("Input.dispatchTouchEvent", {"type": "touchEnd", "touchPoints": []})


@pytest.mark.parametrize("width", [390])
def test_mobile_swipe_switches_live_terminal_tabs_without_stealing_scroll_or_selection(
    page, real_terminal, width, tmp_path
):
    t = real_terminal
    second_root = tmp_path / "second-workspace"
    second_root.mkdir()
    subprocess.run(["tmux", "new-session", "-d", "-s", "terminal-two",
                    "-c", str(second_root), "bash --norc"], env=t["env"], check=True)
    t["api"]("/sessions/adopt", {
        "target_id": t["target_id"], "tmux_session": "terminal-two",
        "workdir": str(second_root), "name": "Second terminal", "agent": "claude",
    })
    page.set_viewport_size({"width": width, "height": 900})
    page.goto(t["url"] + "/#sessions")

    page.locator(".scard", has_text="Real terminal").get_by_role(
        "button", name="⌨ Attach"
    ).click()
    expect(page.locator(".terminal-tab", has_text="Real terminal")).to_have_attribute(
        "aria-selected", "true", timeout=15000
    )
    # Compact mode folds the primary nav; an explicit sessions route is the
    # same supported escape used by the terminal's Browse action.
    page.goto(t["url"] + "/#sessions")
    page.locator(".scard", has_text="Second terminal").get_by_role(
        "button", name="⌨ Attach"
    ).click()
    expect(page.locator(".terminal-tab", has_text="Second terminal")).to_have_attribute(
        "aria-selected", "true", timeout=15000
    )
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    panel = page.locator(".terminal-tabpanel:not([hidden]) iframe")
    box = panel.bounding_box()
    assert box and box["width"] > 250 and box["height"] > 300
    page.screenshot(path="/tmp/agentdeck-mobile-tabs-before.png", full_page=False)

    # A real CDP touch flick anywhere in the active terminal selects the
    # adjacent tab.  The tab identity and its attached iframe remain mounted.
    _touch_swipe(page, box["x"] + box["width"] * .25, box["y"] + box["height"] * .5,
                 box["x"] + box["width"] * .65, box["y"] + box["height"] * .5)
    expect(page.locator(".terminal-tab", has_text="Real terminal")).to_have_attribute(
        "aria-selected", "true", timeout=3000
    )
    page.screenshot(path="/tmp/agentdeck-mobile-tabs-after-right.png", full_page=False)
    _touch_swipe(page, box["x"] + box["width"] * .65, box["y"] + box["height"] * .5,
                 box["x"] + box["width"] * .25, box["y"] + box["height"] * .5)
    expect(page.locator(".terminal-tab", has_text="Second terminal")).to_have_attribute(
        "aria-selected", "true", timeout=3000
    )
    page.screenshot(path="/tmp/agentdeck-mobile-tabs-after-left.png", full_page=False)

    frame = [f for f in page.frames if "/terminal/session/" in f.url][-1]
    state = frame.evaluate("window.__adkTerminalState?.()")
    assert state and state["mouseTrackingMode"] == "none"
    assert frame.evaluate("window.__adkTerminalSelectAll?.()") is None
    assert frame.evaluate("window.__adkTerminalState?.().hasSelection")
    active_before = page.locator('.terminal-tab[aria-selected="true"]').inner_text()
    # A horizontal text selection is owned by xterm and must not navigate.
    _touch_swipe(page, box["x"] + box["width"] * .25, box["y"] + box["height"] * .5,
                 box["x"] + box["width"] * .65, box["y"] + box["height"] * .5)
    expect(page.locator('.terminal-tab[aria-selected="true"]')).to_have_text(active_before)

    frame.evaluate("window.getSelection()?.removeAllRanges()")
    # Vertical reading gestures that briefly backtrack into a diagonal path
    # also stay in the terminal; endpoint-only checks would misclassify this.
    _touch_path(page, [(box["x"] + box["width"] * .25, box["y"] + box["height"] * .7),
                       (box["x"] + box["width"] * .28, box["y"] + box["height"] * .35),
                       (box["x"] + box["width"] * .65, box["y"] + box["height"] * .5)])
    expect(page.locator('.terminal-tab[aria-selected="true"]')).to_have_text(active_before)

    # The compact toolbar keeps secondary actions reachable through its menu,
    # and a browser keyboard resize does not clip the active terminal.
    page.locator('.terminal-actions>summary').click()
    expect(page.get_by_role("menuitem", name="Close this view")).to_be_visible()
    page.keyboard.press("Escape")
    page.set_viewport_size({"width": width, "height": 640})
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    assert page.locator(".terminal-tab[aria-selected=true]").count() == 1
