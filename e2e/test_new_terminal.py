"""One press from the Terminals view to a blank shell, on real ttyd and tmux."""
import subprocess
from playwright.sync_api import expect
from test_terminal_workspace import real_terminal


def test_new_terminal_button_opens_a_blank_shell(page,real_terminal):
    t=real_terminal;errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
    before={s['id'] for s in t['api']('/sessions')}
    page.goto(t['url']+'/#terminals')
    # With nothing open, the way in is offered right where the terminals will be.
    expect(page.locator('.terminal-empty')).to_be_visible()
    # One machine configured: there is nothing to choose between, so no menu.
    expect(page.locator('.terminal-new-machines')).to_have_count(0)
    page.locator('.terminal-empty').get_by_role('button',name='New terminal').click()
    expect(page.get_by_role('tab')).to_have_count(1,timeout=15000)
    created=[s for s in t['api']('/sessions') if s['id'] not in before]
    assert len(created)==1,created
    shell=created[0]
    f=page.frame_locator(f'iframe[src="/terminal/session/{shell["id"]}?embed=1"]')
    expect(f.locator('#connection')).to_have_text('Connected',timeout=20000)
    # It is a shell you can type into, not an agent waiting on a prompt.
    f.locator('#agent-terminal').click()
    page.keyboard.type('echo BLANK-SHELL-$((6*7))');page.keyboard.press('Enter')
    expect(f.locator('#agent-terminal .xterm-screen')).to_contain_text('BLANK-SHELL-42',timeout=10000)
    out=subprocess.check_output(['tmux','capture-pane','-p','-t','='+shell['tmux_session']+':'],env=t['env']).decode()
    assert 'BLANK-SHELL-42' in out
    # With a terminal open the button moves to the tab bar, and a second press
    # opens a second shell beside the first rather than reusing it.
    page.locator('.terminal-tabbar').get_by_role('button',name='New terminal').click()
    expect(page.get_by_role('tab')).to_have_count(2,timeout=15000)
    assert len(t['api']('/sessions'))==len(before)+2
    assert not errors,errors
