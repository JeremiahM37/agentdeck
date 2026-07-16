"""Playwright e2e — real browser, real server (mock executor), both viewports."""
import os
import socket
import subprocess
import sys
import time
from pathlib import Path

import pytest
from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[2]
PORT = 9199
BASE = f"http://127.0.0.1:{PORT}"

PHONE = {"width": 390, "height": 844}
DESKTOP = {"width": 1440, "height": 900}


def _port_open() -> bool:
    with socket.socket() as s:
        return s.connect_ex(("127.0.0.1", PORT)) == 0


@pytest.fixture(scope="session")
def server(tmp_path_factory):
    env = {**os.environ,
           "AGENTDECK_MOCK": "1", "AGENTDECK_TICK": "0.1",
           "AGENTDECK_MOCK_DELAY": "0.25", "AGENTDECK_PORT": str(PORT),
           "AGENTDECK_DB": str(tmp_path_factory.mktemp("e2e") / "e2e.db"),
           "AGENTDECK_BASE_URL": BASE}
    proc = subprocess.Popen([sys.executable, "-m", "server"], cwd=ROOT, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    for _ in range(100):
        if _port_open():
            break
        time.sleep(0.1)
    else:
        proc.kill()
        raise RuntimeError("server did not start")
    yield BASE
    proc.terminate()
    proc.wait(timeout=10)


AUTH_PORT = 9198
AUTH_BASE = f"http://127.0.0.1:{AUTH_PORT}"


@pytest.fixture(scope="session")
def auth_server(tmp_path_factory):
    """A second server with AGENTDECK_AUTH_TOKEN set, to prove the PWA works in
    token-auth mode (fetch + EventSource both need the token threaded through)."""
    env = {**os.environ, "AGENTDECK_MOCK": "1", "AGENTDECK_TICK": "0.1",
           "AGENTDECK_PORT": str(AUTH_PORT), "AGENTDECK_AUTH_TOKEN": "secret123",
           "AGENTDECK_DB": str(tmp_path_factory.mktemp("auth") / "a.db"),
           "AGENTDECK_BASE_URL": AUTH_BASE}
    proc = subprocess.Popen([sys.executable, "-m", "server"], cwd=ROOT, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    for _ in range(100):
        with socket.socket() as s:
            if s.connect_ex(("127.0.0.1", AUTH_PORT)) == 0:
                break
        time.sleep(0.1)
    else:
        proc.kill(); raise RuntimeError("auth server did not start")
    yield AUTH_BASE
    proc.terminate(); proc.wait(timeout=10)


@pytest.fixture(scope="session")
def browser():
    with sync_playwright() as p:
        b = p.chromium.launch()
        yield b
        b.close()


@pytest.fixture()
def page(browser, server, request):
    viewport = getattr(request, "param", DESKTOP)
    ctx = browser.new_context(viewport=viewport)
    pg = ctx.new_page()
    yield pg
    ctx.close()


def _new_task(page, title, prompt="fix it", perm=None):
    page.click("#fab")
    page.fill("#f-title", title)
    page.fill("#f-prompt", prompt)
    if perm:
        page.select_option("#f-perm", perm)
    page.click("#f-go")


@pytest.mark.parametrize("page", [DESKTOP, PHONE], indirect=True,
                         ids=["desktop", "phone"])
def test_board_renders(page, server):
    page.goto(server)
    expect(page.locator(".brand h1")).to_contain_text("AGENT")
    cols = page.locator(".col-head")
    expect(cols).to_have_count(6)
    for name in ["backlog", "queued", "running", "review", "done", "failed"]:
        expect(page.locator(f".col.s-{name}")).to_be_visible()
    expect(page.locator("#fab")).to_be_visible()
    expect(page.locator("#conn-label")).to_have_text("LIVE", timeout=10000)


def test_full_flow_dispatch_review_diff_done(page, server):
    page.goto(server)
    _new_task(page, "E2E ship it", "add health endpoint")
    card = page.locator(".card", has_text="E2E ship it")
    # card lands on the board and travels to review as the mock agent works
    expect(card).to_be_visible(timeout=10000)
    expect(page.locator(".col.s-review .card", has_text="E2E ship it")) \
        .to_be_visible(timeout=20000)

    # open detail: live timeline captured the agent events
    page.locator(".col.s-review .card", has_text="E2E ship it").click()
    expect(page.locator("#sheet .statpill")).to_have_text("review")
    expect(page.locator("#sheet .ev.e-init")).to_be_visible()
    expect(page.locator("#sheet .ev.e-tool_use")).to_be_visible()
    expect(page.locator("#sheet .ev.e-result")).to_be_visible()

    # diff viewer
    page.click("#actions button:has-text('Diff')")
    expect(page.locator(".dfile summary", has_text="app.py")).to_be_visible()
    expect(page.locator(".dl-add", has_text="hello, agentdeck").first).to_be_visible()

    # mark done → card moves to done column
    page.click("#actions button:has-text('Mark done')")
    expect(page.locator(".col.s-done .card", has_text="E2E ship it")) \
        .to_be_visible(timeout=10000)


def test_approval_flow_from_phone(page, server):
    page.goto(server)
    _new_task(page, "E2E gated deploy", "deploy [mock:approval]", perm="default")
    # badge lights up
    expect(page.locator("#appr-badge")).to_be_visible(timeout=15000)
    page.click(".tab[data-tab='approvals']")
    row = page.locator(".rowcard", has_text="Bash")
    expect(row.first).to_be_visible()
    expect(row.first.locator("pre")).to_contain_text("rm -rf build/")
    row.first.locator("button:has-text('Approve')").click()
    # agent continues and finishes
    page.click(".tab[data-tab='board']")
    expect(page.locator(".col.s-review .card", has_text="E2E gated deploy")) \
        .to_be_visible(timeout=20000)


test_approval_flow_from_phone = pytest.mark.parametrize(
    "page", [PHONE], indirect=True, ids=["phone"])(test_approval_flow_from_phone)


def test_targets_tab_probe(page, server):
    page.goto(server)
    page.click(".tab[data-tab='targets']")
    row = page.locator(".rowcard", has_text="lxc-101-project-env")
    expect(row).to_be_visible()
    row.locator("button:has-text('Probe')").click()
    expect(row.locator(".sub", has_text="claude")).to_be_visible(timeout=10000)


def test_quickbar_instant_dispatch(page, server):
    page.goto(server)
    page.fill("#qb-input", "quick: bump the version")
    page.press("#qb-input", "Enter")
    expect(page.locator(".card", has_text="quick: bump the version")) \
        .to_be_visible(timeout=10000)
    expect(page.locator(".col.s-review .card", has_text="quick: bump")) \
        .to_be_visible(timeout=20000)


def test_drag_card_to_queued_dispatches(page, server):
    page.goto(server)
    page.click("#fab")
    page.fill("#f-title", "Drag me")
    page.fill("#f-prompt", "dragged task")
    page.click("#f-save")     # backlog, not dispatched
    card = page.locator(".col.s-backlog .card", has_text="Drag me")
    expect(card).to_be_visible(timeout=10000)
    card.drag_to(page.locator(".col.s-queued .col-body"))
    # dispatch happened → travels to review
    expect(page.locator(".col.s-review .card", has_text="Drag me")) \
        .to_be_visible(timeout=20000)


def test_verify_badge_shows_on_card(page, server):
    page.goto(server)
    page.evaluate("""async () => {
      const projects = await fetch('/api/projects').then(r => r.json());
      await fetch(`/api/projects/${projects[0].id}`, {
        method: 'PATCH', headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({verify_cmd: 'mockverify-pass'})});
    }""")
    page.fill("#qb-input", "verified change please")
    page.press("#qb-input", "Enter")
    card = page.locator(".col.s-review .card", has_text="verified change")
    expect(card).to_be_visible(timeout=20000)
    expect(card.locator(".chip", has_text="verified")).to_be_visible(timeout=10000)


def test_approval_card_has_always_allow(page, server):
    page.goto(server)
    _new_task(page, "E2E always allow", "risky [mock:approval]", perm="default")
    expect(page.locator("#appr-badge")).to_be_visible(timeout=15000)
    page.click(".tab[data-tab='approvals']")
    row = page.locator(".rowcard", has_text="Bash").first
    expect(row.locator("button", has_text="∞ Always")).to_be_visible()
    row.locator("button:has-text('Approve')").first.click()
    page.click(".tab[data-tab='board']")
    expect(page.locator(".col.s-review .card", has_text="E2E always allow")) \
        .to_be_visible(timeout=20000)


def test_board_filter_narrows_cards(page, server):
    page.goto(server)
    page.fill("#qb-input", "alpha unique thing")
    page.press("#qb-input", "Enter")
    page.fill("#qb-input", "beta other thing")
    page.press("#qb-input", "Enter")
    expect(page.locator(".card", has_text="alpha unique")).to_be_visible(timeout=10000)
    expect(page.locator(".card", has_text="beta other")).to_be_visible(timeout=10000)
    page.fill("#qb-filter", "alpha")
    expect(page.locator(".card", has_text="beta other")).to_have_count(0)
    expect(page.locator(".card", has_text="alpha unique")).to_be_visible()
    page.fill("#qb-filter", "")
    expect(page.locator(".card", has_text="beta other")).to_be_visible()


def test_deck_view_streams_panes(page, server):
    page.goto(server)
    page.fill("#qb-input", "deck watch me work")
    page.press("#qb-input", "Enter")
    page.click(".tab[data-tab='deck']")
    pane = page.locator(".pane", has_text="deck watch me")
    expect(pane).to_be_visible(timeout=15000)
    expect(pane.locator(".pane-line").first).to_be_visible(timeout=15000)


def test_settings_ui_saves_sinks(page, server):
    page.goto(server)
    page.click(".tab[data-tab='targets']")
    page.fill("#s-ntfy-server", "https://ntfy.sh")
    page.fill("#s-ntfy-topic", "adk-e2e")
    page.click("#s-save")
    expect(page.locator(".toast", has_text="Sinks saved")).to_be_visible()
    page.reload()
    page.click(".tab[data-tab='targets']")
    expect(page.locator("#s-ntfy-topic")).to_have_value("adk-e2e", timeout=5000)


def test_multi_attempt_badge_not_mislabeled_ab(page, server):
    """A retry produces 2 attempts but is NOT a parallel A/B run — the card must
    not claim 'A/B'. (Regression: the badge said 'A/B xN' for any multi-attempt.)"""
    page.goto(server)
    _new_task(page, "Retry me", "do it")
    # first attempt → review
    expect(page.locator(".col.s-review .card", has_text="Retry me")) \
        .to_be_visible(timeout=20000)
    # retry via API (sequential second attempt, not A/B)
    tid = page.evaluate("""async () => {
      const ts = await (await fetch('/api/tasks')).json();
      const t = ts.find(x => x.title === 'Retry me');
      await fetch(`/api/tasks/${t.id}/dispatch`, {method:'POST',
        headers:{'Content-Type':'application/json'}, body:'{}'});
      return t.id;
    }""")
    card = page.locator(".card", has_text="Retry me")
    expect(card.locator(".chip", has_text="×2")).to_be_visible(timeout=20000)
    # the false 'A/B' claim must be gone
    assert page.locator(".card", has_text="Retry me").locator(
        "text=A/B").count() == 0


def test_foreground_resync_refetches(page, server):
    """Returning to foreground (or SSE reconnect, shared path) must resync the
    board — otherwise a phone that missed events while backgrounded shows stale
    state. (Regression: onopen/visibility did nothing.)"""
    page.goto(server)
    page.wait_for_selector("#board")
    page.wait_for_timeout(500)
    hits = []
    page.on("request", lambda r: hits.append(r.url)
            if r.url.rstrip("/").endswith("/api/tasks")
            else None)
    # faithfully simulate a phone returning to foreground: visible + the event.
    # (headless Chromium reports 'hidden' by default, which the handler correctly
    # ignores — so force 'visible' as a real unlock would.)
    page.evaluate("""() => {
      Object.defineProperty(document, 'visibilityState',
        { get: () => 'visible', configurable: true });
      document.dispatchEvent(new Event('visibilitychange'));
    }""")
    page.wait_for_timeout(700)
    assert len(hits) >= 1, "foreground/reconnect did not resync /api/tasks"


def test_token_auth_ui_works(browser, auth_server):
    """With AGENTDECK_AUTH_TOKEN set: no token → API 401 (UI can't load data);
    token in localStorage → board renders and SSE goes LIVE. (Regression: the PWA
    never sent the token via fetch or the SSE query param, so the UI was dead.)"""
    ctx = browser.new_context(viewport=DESKTOP)
    pg = ctx.new_page()
    pg.goto(auth_server)
    # same-origin API call with no token → rejected by the middleware
    status = pg.evaluate("async () => (await fetch('/api/tasks')).status")
    assert status == 401, f"expected 401 without token, got {status}"
    # store the token as the 401-prompt flow does, then load
    pg.evaluate("localStorage.setItem('adk-token','secret123')")
    pg.reload()
    expect(pg.locator(".col-head")).to_have_count(6, timeout=10000)
    expect(pg.locator("#conn-label")).to_have_text("LIVE", timeout=10000)  # SSE authed via query token
    ctx.close()


def test_delete_task_from_ui(page, server):
    """The delete button removes the card from the board (via the task_deleted
    SSE event) and closes the sheet. (Regression: there was no way to delete a
    task; mistaken/test cards accumulated forever.)"""
    page.goto(server)
    _new_task(page, "UI delete target", "noop")
    card = page.locator(".card", has_text="UI delete target")
    expect(card).to_be_visible(timeout=15000)
    card.first.click()
    page.on("dialog", lambda d: d.accept())
    page.click("#actions button:has-text('Delete')")
    expect(page.locator(".card", has_text="UI delete target")).to_have_count(0,
                                                                             timeout=10000)
    expect(page.locator("#sheet")).to_be_hidden()


def test_running_task_card_no_console_crash(page, server):
    """A running/queued task has no diff yet (diff_stat is {} server-side). The
    card must render without throwing — a JS error here aborted the whole column
    render. (Found via the browser console during live testing.)"""
    errors = []
    page.on("console", lambda m: errors.append(m.text) if m.type == "error" else None)
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.goto(server)
    # a slow task stays in 'running' long enough for its no-diff card to render
    _new_task(page, "Long runner", "work [mock:slow]")
    expect(page.locator(".col.s-running .card", has_text="Long runner")) \
        .to_be_visible(timeout=10000)
    # the running card rendered AND the board is intact (6 columns still there)
    expect(page.locator(".col-head")).to_have_count(6)
    assert not [e for e in errors if "reduce" in e or "is not a function" in e], errors


def test_deck_persists_streams_across_updates(page, server):
    """The Deck must reconcile panes incrementally — a status change of one task
    must not tear down and reopen every pane's SSE. We assert a persisting pane's
    DOM node is the SAME element before and after another task changes status."""
    page.goto(server)
    # two tasks: one slow (stays running), one fast (will change status)
    _new_task(page, "Deck persist A", "slow one [mock:slow]")
    _new_task(page, "Deck persist B", "quick one")
    page.click(".tab[data-tab='deck']")
    paneA = page.locator(".pane", has_text="Deck persist A")
    expect(paneA).to_be_visible(timeout=15000)
    # tag pane A's DOM node so we can detect if it gets recreated
    page.evaluate("""() => {
      const p = [...document.querySelectorAll('.pane')]
        .find(e => e.textContent.includes('Deck persist A'));
      if (p) p.dataset.adkMark = 'orig';
    }""")
    # B finishes → a board task event fires → renderDeck runs again
    expect(page.locator(".pane", has_text="Deck persist B")
           .locator(".statpill", has_text="review")).to_be_visible(timeout=20000)
    # pane A must be the SAME element (stream not torn down)
    still = page.evaluate("""() => {
      const p = [...document.querySelectorAll('.pane')]
        .find(e => e.textContent.includes('Deck persist A'));
      return p ? p.dataset.adkMark : null;
    }""")
    assert still == "orig", "deck pane A was recreated on another task's update (SSE thrash)"


def test_pwa_assets(page, server):
    page.goto(server)
    assert page.evaluate("fetch('/manifest.webmanifest').then(r=>r.ok)")
    assert page.evaluate("fetch('/sw.js').then(r=>r.ok)")
    assert page.evaluate("fetch('/icon.svg').then(r=>r.ok)")
