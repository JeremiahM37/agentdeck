"""Scrolling has to reach both ordinary scrollback and mouse-driven TUIs."""
import time
import subprocess
import pytest
from playwright.sync_api import expect
from test_terminal_workspace import real_terminal, open_terminal, type_command, terminal_tool


def test_mouse_application_receives_scroll(page,real_terminal):
    t=real_terminal
    page.add_init_script('''let Constructor;
      Object.defineProperty(window,'Terminal',{configurable:true,
        get(){return Constructor},set(Base){Constructor=class extends Base {
          constructor(...args){super(...args);window.testTerminal=this;}
        };}});''')
    (t['root']/'mouse-app.py').write_text('''import os,tty,sys,re
old=None
tty.setraw(sys.stdin.fileno())
os.write(1,b'\\x1b[?1049h\\x1b[?1000h\\x1b[?1006h\\x1b[2J\\x1b[HMOUSE-APP-READY')
while True:
 data=os.read(0,4096)
 with open('mouse-input.log','ab') as f:f.write(data)
 if b'[<64;' in data:os.write(1,b'\\x1b[2;1HSCROLLED-OLDER-CONTENT')
''')
    open_terminal(page,t);type_command(page,'python3 mouse-app.py')
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('MOUSE-APP-READY')
    box=page.locator('#agent-terminal').bounding_box()
    page.mouse.move(box['x']+box['width']/2,box['y']+box['height']/2)
    page.mouse.wheel(0,-350)
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('SCROLLED-OLDER-CONTENT',timeout=3000)

    # Finger swipes must generate the same negotiated mouse-wheel reports.
    before=(t['root']/'mouse-input.log').read_bytes().count(b'[<64;')
    client=page.context.new_cdp_session(page)
    x=box['x']+box['width']/2; y=box['y']+box['height']/2
    client.send('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[{'x':x,'y':y}]})
    for delta in [15,30,45,60]:
        client.send('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[{'x':x,'y':y+delta}]})
        page.wait_for_timeout(30)
    client.send('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
    deadline=time.time()+3
    while time.time()<deadline:
        if (t['root']/'mouse-input.log').read_bytes().count(b'[<64;')>before:break
        page.wait_for_timeout(50)
    assert (t['root']/'mouse-input.log').read_bytes().count(b'[<64;')>before
    client.detach()


@pytest.mark.parametrize('real_terminal', [{}, {'no_alternate_screen':True}], indirect=True, ids=['tmux-default','tmux-normal-screen'])
@pytest.mark.parametrize('return_gesture', ['wheel', 'touch'])
def test_scroll_fetches_retained_history_from_before_attach(page,real_terminal,return_gesture):
    t=real_terminal
    subprocess.run(['tmux','send-keys','-t','=terminal-test:',"for i in $(seq 1 180); do echo OLD-LINE-$i; done",'Enter'],env=t['env'],check=True)
    time.sleep(.2)
    open_terminal(page,t)
    box=page.locator('#agent-terminal').bounding_box()
    page.mouse.move(box['x']+box['width']/2,box['y']+box['height']/2)
    page.mouse.wheel(0,-350)
    frozen=page.locator('.frozen')
    expect(frozen).to_be_visible(timeout=10000)
    expect(frozen).to_contain_text('OLD-LINE-1')
    assert frozen.evaluate('(el)=>el.scrollTop<el.scrollHeight-el.clientHeight')
    expect(page.locator('#pause')).to_have_text('Pause view')
    # Returning to the bottom resumes naturally, with no button or blocked input.
    if return_gesture == 'wheel':
        page.mouse.wheel(0,10000)
    else:
        client=page.context.new_cdp_session(page)
        x=box['x']+box['width']/2; y=box['y']+box['height']-50
        client.send('Input.dispatchTouchEvent',{'type':'touchStart','touchPoints':[{'x':x,'y':y}]})
        for delta in range(30,540,30):
            client.send('Input.dispatchTouchEvent',{'type':'touchMove','touchPoints':[{'x':x,'y':max(box['y']+10,y-delta)}]})
            page.wait_for_timeout(20)
        client.send('Input.dispatchTouchEvent',{'type':'touchEnd','touchPoints':[]})
        client.detach()
    expect(frozen).not_to_be_visible()
    page.locator('#agent-terminal').click();type_command(page,'echo BACK-AT-LIVE-PROMPT')
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('BACK-AT-LIVE-PROMPT')
    page.wait_for_timeout(1100)
    page.mouse.wheel(0,-10000)
    page.wait_for_timeout(200)
    page.mouse.wheel(0,-10000)
    expect(frozen).to_be_visible()
    page.keyboard.type('echo TYPING-RETURNS-TO-LIVE')
    page.keyboard.press('Enter')
    expect(frozen).not_to_be_visible()
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('TYPING-RETURNS-TO-LIVE')
    # Only explicitly pressing Pause should require Resume.
    terminal_tool(page,'#pause')
    expect(frozen).to_be_visible()
    page.mouse.move(box['x']+box['width']/2,box['y']+box['height']/2)
    page.mouse.wheel(0,10000)
    expect(page.locator('#pause')).to_have_text('Resume view')
    expect(frozen).to_be_visible()
