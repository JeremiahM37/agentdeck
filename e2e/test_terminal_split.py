"""An unclipped tools menu, on real ttyd and tmux."""
from playwright.sync_api import expect
from test_terminal_workspace import real_terminal, capture


def attach(page, name):
    page.locator('.tab[data-tab="sessions"]').click()
    page.locator('.scard',has_text=name).get_by_role('button',name='⌨ Attach',exact=True).click()
    expect(page.locator('.tab[data-tab="terminals"]')).to_have_class('tab on')


def frame(page, id):
    return page.frame_locator(f'iframe[src="/terminal/session/{id}?embed=1"]')


def ready(f):
    expect(f.owner).to_be_visible(timeout=15000)
    expect(f.locator('#connection')).to_have_text('Connected',timeout=15000)
    expect(f.locator('#agent-terminal .xterm-screen')).to_contain_text('$',timeout=10000)


def test_tools_menu_opens_over_the_terminal(page,real_terminal):
    t=real_terminal;errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
    page.goto(t['url']+'/#sessions')
    attach(page,'Real terminal');f=frame(page,t['id']);ready(f)
    f.locator('#terminal-tools-summary').click()
    panel=f.locator('#terminal-tools .action-menu-panel')
    expect(panel).to_be_visible()
    # The toolbar scrolls horizontally, which clips vertically too, so a menu
    # laid out inside it is invisible however "visible" its box looks. Hit-test
    # a real point instead: the terminal must not be painted over the menu.
    hit=panel.evaluate('''(panel)=>{const r=panel.getBoundingClientRect();
        const el=document.elementFromPoint(r.left+r.width/2,r.top+20);
        return {inside:!!el&&panel.contains(el),tag:el?el.tagName+'.'+el.className:null,
                bottom:r.bottom,height:r.height,viewport:innerHeight};}''')
    assert hit['inside'],f'tools menu is covered or clipped; point hit {hit["tag"]}'
    assert hit['height']>100,hit
    assert hit['bottom']<=hit['viewport']+1,f'menu runs past the viewport: {hit}'
    # Painted is not the same as usable, so drive one entry through to its effect.
    f.get_by_role('button',name='Find in terminal').click()
    expect(f.locator('#search-input')).to_be_visible()
    assert not errors,errors
