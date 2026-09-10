"""Graceful service shutdown with real browser streams and persistent tmux."""
import subprocess
from playwright.sync_api import expect
from test_terminal_workspace import real_terminal
from test_terminal_tabs import attach, frame, ready


def test_shutdown_drains_browser_streams_without_stopping_tmux(page, real_terminal):
    t = real_terminal
    with page.expect_response(lambda r: r.url.endswith('/api/stream')):
        page.goto(t['url'] + '/#sessions')
    attach(page, 'Real terminal')
    terminal = frame(page, t['id'])
    ready(terminal)
    terminal.locator('#agent-terminal').click()
    page.keyboard.type('echo STILL-HERE-AFTER-SHUTDOWN')
    page.keyboard.press('Enter')
    expect(terminal.locator('#agent-terminal .xterm-screen')).to_contain_text('STILL-HERE-AFTER-SHUTDOWN')
    original_tmux = subprocess.check_output(['tmux','display-message','-p','-t','=terminal-test','#{session_id}'],env=t['env'])
    t['proc'].terminate()
    assert t['proc'].wait(timeout=3) == 0
    subprocess.run(['tmux','has-session','-t','=terminal-test'],env=t['env'],check=True)
    expect(terminal.locator('#connection')).not_to_have_text('Connected',timeout=5000)
    restarted = subprocess.Popen(t['proc'].args,cwd=t['root'],env=t['env'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    try:
        expect(terminal.locator('#connection')).to_have_text('Connected',timeout=20000)
        expect(terminal.locator('#agent-terminal .xterm-screen')).to_contain_text('STILL-HERE-AFTER-SHUTDOWN')
        assert subprocess.check_output(['tmux','display-message','-p','-t','=terminal-test','#{session_id}'],env=t['env']) == original_tmux
    finally:
        restarted.terminate()
        restarted.wait(timeout=15)
