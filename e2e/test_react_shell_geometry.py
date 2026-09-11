"""Real mobile layout must not zoom out to accommodate overflowing controls."""
from test_terminal_workspace import real_terminal
from playwright.sync_api import expect

def test_mobile_chrome_geometry(page,real_terminal):
 with page.context.browser.new_context(viewport={'width':390,'height':900},is_mobile=True,has_touch=True) as context:
  phone=context.new_page();phone.goto(real_terminal['url']+'/#sessions')
  expect(phone.locator('.scard')).to_be_visible()
  geometry=phone.evaluate('''()=>({width:innerWidth,scroll:document.documentElement.scrollWidth,nav:document.querySelector('#tabbar').getBoundingClientRect().toJSON()})''')
  assert geometry['width']==390,geometry
  assert geometry['scroll']<=390,geometry
  assert geometry['nav']['bottom']<=900,geometry
  phone.locator('.tab[data-tab="sessions"]').tap()
  expect(phone.locator('.tab[data-tab="sessions"]')).to_have_class('tab on')
