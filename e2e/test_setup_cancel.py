"""Cancel real held checkouts through desktop, phone, and the terminal dashboard."""
import time
from pathlib import Path
import pytest
from playwright.sync_api import expect
from test_terminal_workspace import real_terminal
from test_terminal_dashboard import Dashboard
from test_background_setup_ui import hold_second_checkout


def prepare(t):
    release=hold_second_checkout(t)
    projects=t['api']('/projects')
    primary=next(p for p in projects if p['name']=='Isolated project')
    extra=next(p for p in projects if p['name']=='Second repository')
    row=t['api']('/sessions',{'name':'Cancel setup proof','agent':'claude','target_id':t['target_id'],
                             'project_id':primary['id'],'background':True,
                             'worktree':{'extra_repositories':[{'project_id':extra['id']}]}})
    return release,row


def outcome(t,row):
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        current=t['api'](f"/sessions/{row['id']}")
        if current['setup_state']=='failed':break
        time.sleep(.05)
    assert current['setup_cancel_requested'] and 'cancelled' in current['setup_error'],current
    assert current['ended_at'] is not None
    first=Path(current['workspace']['repositories'][0]['worktree']['path'])
    assert (first/'hello.txt').exists(), 'cancellation removed the completed first checkout'
    assert not current.get('tracking_identity'), 'cancelled setup launched an agent'


@pytest.mark.parametrize('width',[390,1440])
def test_browser_cancels_setup_and_retains_completed_checkout(page,real_terminal,width):
    t=real_terminal;release,row=prepare(t);errors=[]
    page.on('pageerror',lambda error:errors.append(str(error)))
    try:
        page.set_viewport_size({'width':width,'height':900});page.goto(t['url']+'/#sessions')
        card=page.locator('.scard',has_text='Cancel setup proof')
        expect(card.locator('.spane')).to_contain_text('Isolated project: ready',timeout=15000)
        card.get_by_role('button',name='Cancel setup',exact=True).click()
        expect(card.locator('.spane')).to_contain_text('cancelled',timeout=15000)
        outcome(t,row)
        expect(card.get_by_role('button',name='⌨ Attach',exact=True)).to_have_count(0)
        assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
        assert not errors
    finally:release.touch()


def test_terminal_cancels_selected_setup(real_terminal):
    t=real_terminal;release,row=prepare(t);d=Dashboard(t)
    try:
        d.wait('Cancel setup proof');d.send('/Cancel setup proof\r')
        d.wait('Isolated project: ready',timeout=15)
        d.send('m');d.wait('Cancel setup');d.send('\r')
        d.wait('Cancellation requested.')
        d.wait('cancelled',timeout=15)
        outcome(t,row);d.quit()
    finally:release.touch();d.close()
