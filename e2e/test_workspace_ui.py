"""Feature access and state retention across the cleaned-up navigation."""
import pytest
from playwright.sync_api import expect
from conftest import PHONE, DESKTOP
from test_terminal_workspace import real_terminal, open_terminal, terminal_tool

@pytest.mark.parametrize('page',[PHONE,DESKTOP],indirect=True)
def test_settings_sections_keep_drafts_and_keyboard_navigation(page,server):
    page.goto(server+'/#targets')
    projects=page.locator('[data-settings=projects]');projects.click()
    page.locator('#imp-root').fill('/tmp/unsubmitted-project-draft')
    page.locator('[data-settings=notifications]').click()
    expect(page.locator('#imp-root')).not_to_be_visible()
    projects.click();expect(page.locator('#imp-root')).to_have_value('/tmp/unsubmitted-project-draft')
    projects.focus();page.keyboard.press('ArrowRight')
    expect(page.locator('[data-settings=notifications]')).to_be_focused()
    expect(page.locator('[data-settings=notifications]')).to_have_attribute('aria-selected','true')
    expect(page.locator('#fab')).not_to_be_visible()
    assert not page.evaluate('document.documentElement.scrollWidth>innerWidth')

@pytest.mark.parametrize('page',[PHONE,DESKTOP],indirect=True)
def test_session_search_and_secondary_actions_survive_refresh(page,real_terminal):
    t=real_terminal;page.goto(t['url']+'/#sessions')
    search=page.locator('#sess-search');search.fill('Real terminal')
    expect(page.locator('.scard')).to_have_count(1)
    page.locator('.scard .action-menu>summary').click()
    expect(page.get_by_role('button',name='Stop tracking',exact=True)).to_be_visible()
    page.wait_for_timeout(5500)
    expect(page.get_by_role('button',name='Stop tracking',exact=True)).to_be_visible()
    page.keyboard.press('Escape');expect(page.locator('.scard .action-menu')).not_to_have_attribute('open','')
    search.fill('no match');expect(page.locator('.scard')).to_have_count(0)
    search.fill('Real terminal');expect(page.locator('.scard')).to_have_count(1)

@pytest.mark.parametrize('page',[PHONE,DESKTOP],indirect=True)
def test_desktop_platform_choices_and_terminal_tools(page,real_terminal):
    open_terminal(page,real_terminal)
    expect(page.locator('#desktop')).to_have_text('Open in terminal')
    expect(page.locator('#desktop')).to_have_attribute('href',f"agentdeck://attach/session/{real_terminal['id']}")
    terminal_tool(page,'#desktop-setup')
    expect(page.locator('#desktop-open')).to_have_text('Open in terminal')
    expect(page.locator('#desktop-dialog')).not_to_contain_text('Kitty')
    expect(page.locator('#desktop-dialog')).not_to_contain_text('WezTerm')
    expect(page.locator('#desktop-dialog')).to_contain_text('Linux')
    expect(page.locator('#desktop-dialog')).to_contain_text('Windows')
    expect(page.locator('a[href="/desktop/setup-agentdeck-terminal.sh"]')).to_be_visible()
    expect(page.locator('a[href="/desktop/setup-agentdeck.ps1"]')).to_be_visible()
    page.locator('#desktop-dialog [data-close]').click()
    expect(page.locator('#pause')).not_to_be_visible()
    page.locator('#terminal-tools>summary').click()
    for selector in ['#pause','#find','#history','#preferences','#shell','#reconnect']:
        expect(page.locator(selector)).to_be_visible()
    page.keyboard.press('Escape')
    expect(page.locator('#pause')).not_to_be_visible()
    assert not page.evaluate('document.documentElement.scrollWidth>innerWidth')

@pytest.mark.parametrize('link',['#desktop','#desktop-open'])
def test_open_in_terminal_keeps_browser_terminal_alive(page,real_terminal,link):
    t=real_terminal
    open_terminal(page,t)
    page.evaluate('''() => {
      window.departures = [];
      for (const type of ['beforeunload', 'pagehide'])
        window.addEventListener(type, () => window.departures.push(type));
    }''')
    if link == '#desktop-open':
        terminal_tool(page,'#desktop-setup')
    page.locator(link).click()
    page.wait_for_timeout(300)
    # Chromium attempts an external-protocol navigation even when the headless
    # machine has no handler. The document stays; its live terminal must too.
    assert 'beforeunload' in page.evaluate('window.departures')
    assert 'pagehide' not in page.evaluate('window.departures')
    if link == '#desktop-open':
        page.locator('#desktop-dialog [data-close]').click()
    expect(page.locator('#agent-terminal .xterm-screen')).to_be_visible()
    page.locator('#agent-terminal').click()
    page.keyboard.type('printf click-survived > click-proof.txt')
    page.keyboard.press('Enter')
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('click-proof.txt')
    import time
    for _ in range(50):
        if (t['root']/'click-proof.txt').exists():break
        time.sleep(.1)
    assert (t['root']/'click-proof.txt').read_text()=='click-survived'
