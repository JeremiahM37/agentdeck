"""A rejected real terminal stop must remain visible and retryable."""
import subprocess
import pytest
from playwright.sync_api import expect
from test_terminal_workspace import real_terminal

@pytest.mark.parametrize('real_terminal',[{'controllable_stop':True}],indirect=True)
@pytest.mark.parametrize('width',[390,1440])
def test_failed_stop_keeps_card_and_terminal_until_retry(page,real_terminal,width):
    t=real_terminal
    marker=t['root']/'refuse-stop';marker.touch()
    subprocess.run(['tmux','new-session','-d','-s','terminal-test-neighbor','bash --norc'],env=t['env'],check=True)
    page.set_viewport_size({'width':width,'height':900})
    page.goto(t['url']+'/#sessions')
    card=page.locator('.scard',has_text='Real terminal')
    def stop():
        card.locator('summary').first.click()
        page.once('dialog',lambda dialog:dialog.accept())
        with page.expect_response(lambda r:r.request.method=='DELETE' and f"/sessions/{t['id']}?" in r.url) as response:
            card.get_by_role('button',name='Kill',exact=True).click()
        return response.value
    response=stop()
    assert response.status==500
    expect(page.locator('.toast.err')).to_contain_text('terminal did not stop')
    expect(card).to_be_visible()
    assert t['api'](f"/sessions/{t['id']}")['ended_at'] is None
    subprocess.run(['tmux','has-session','-t','=terminal-test'],env=t['env'],check=True)
    marker.unlink()
    assert stop().status==200
    expect(card).to_have_count(0)
    assert t['api'](f"/sessions/{t['id']}")['ended_at'] is not None
    assert subprocess.run(['tmux','has-session','-t','=terminal-test'],env=t['env'],capture_output=True).returncode!=0
    subprocess.run(['tmux','has-session','-t','=terminal-test-neighbor'],env=t['env'],check=True)
