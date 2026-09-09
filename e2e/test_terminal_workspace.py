"""Real ttyd + tmux + filesystem; no simulated terminal or target executor."""
import base64
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.request

import pytest
from playwright.sync_api import expect
from conftest import _binary, _unused_port

@pytest.fixture()
def real_terminal(tmp_path):
    tmux_dir = tempfile.TemporaryDirectory(prefix='adkt-', dir='/tmp')
    env = {**os.environ, 'TMUX_TMPDIR': tmux_dir.name, 'TMUX': '', 'AGENTDECK_MOCK': '0',
           'AGENTDECK_DB': str(tmp_path/'test.db'), 'AGENTDECK_HOST': '127.0.0.1',
           'AGENTDECK_GRIMOIRE_URL': '', 'AGENTDECK_AUTH_TOKEN': '', 'AGENTDECK_SESSION_POLL': '3600'}
    port = _unused_port(); env['AGENTDECK_PORT'] = str(port)
    url = f'http://127.0.0.1:{port}'
    root = tmp_path/'workspace'; root.mkdir()
    (root/'hello.txt').write_text('A useful artifact\n<script>window.bad=true</script>\n')
    subprocess.run(['git','init','-q',str(root)], check=True)
    subprocess.run(['tmux','new-session','-d','-s','terminal-test','-c',str(root),'bash --norc'],env=env,check=True)
    log = (tmp_path/'server.log').open('w')
    proc = subprocess.Popen([_binary()],cwd=root,env=env,stdout=log,stderr=log)
    def api(path, data=None):
        req = urllib.request.Request(url+'/api'+path, data=json.dumps(data).encode() if data is not None else None,
                                     headers={'Content-Type':'application/json'})
        return json.load(urllib.request.urlopen(req,timeout=20))
    try:
        for _ in range(100):
            try: api('/health'); break
            except Exception: time.sleep(.1)
        else: raise RuntimeError('isolated terminal server did not start')
        target = api('/targets',{'name':'terminal-local','kind':'local'})
        sess = api('/sessions/adopt',{'target_id':target['id'],'tmux_session':'terminal-test','workdir':str(root),'name':'Real terminal','agent':'claude'})
        yield dict(url=url,root=root,env=env,id=sess['id'],api=api,proc=proc,port=port)
    finally:
        proc.terminate();proc.wait(timeout=15);log.close()
        subprocess.run(['tmux','kill-server'],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
        tmux_dir.cleanup()

def open_terminal(page, t):
    page.goto(f"{t['url']}/terminal/session/{t['id']}")
    expect(page.locator('#connection')).to_have_text('Connected',timeout=20000)
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('$',timeout=10000)
    page.locator('#agent-terminal').click()

def capture(t,session='terminal-test'):
    return subprocess.check_output(['tmux','capture-pane','-p','-J','-S','-100000','-t','='+session+':'],env=t['env']).decode()

def type_command(page,text):
    page.keyboard.type(text,delay=1);page.keyboard.press('Enter')

def test_real_terminal_drop_paste_files_and_shell(page,real_terminal):
    t=real_terminal;errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
    open_terminal(page,t)
    page.keyboard.type('sha256sum ')
    content=b'original bytes\x00\xff\n';name="Résumé's $(touch owned).pdf"
    with page.expect_response(lambda r:r.request.method=='POST' and r.url.endswith('/attachments')) as response:
        page.locator('#agent-terminal').evaluate('''(el, p) => {const d=new DataTransfer();d.items.add(new File([new Uint8Array(p.bytes)],p.name));el.dispatchEvent(new DragEvent('drop',{bubbles:true,cancelable:true,dataTransfer:d}));}''',{'bytes':list(content),'name':name})
    a=response.value.json();expect(page.locator('#notice')).to_contain_text('Path inserted')
    assert Path(a['path']).read_bytes()==content
    assert hashlib.sha256(content).hexdigest() not in capture(t)
    assert not (t['root']/'owned').exists()
    page.keyboard.press('Enter')
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text(hashlib.sha256(content).hexdigest(),timeout=10000)
    # A real clipboard image follows the upload path, never textual escape input.
    png=base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aF9sAAAAASUVORK5CYII=')
    page.keyboard.type('sha256sum ')
    with page.expect_response(lambda r:r.request.method=='POST' and r.url.endswith('/attachments')) as response:
        page.locator('#agent-terminal textarea').evaluate('''(el, bytes)=>{const d=new DataTransfer();d.items.add(new File([new Uint8Array(bytes)],'screenshot.png',{type:'image/png'}));el.dispatchEvent(new ClipboardEvent('paste',{clipboardData:d,bubbles:true,cancelable:true}));}''',list(png))
    a=response.value.json();expect(page.locator('#notice')).to_contain_text('screenshot.png uploaded')
    assert Path(a['path']).read_bytes()==png
    assert hashlib.sha256(png).hexdigest() not in capture(t)
    page.keyboard.press('Enter')
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text(hashlib.sha256(png).hexdigest(),timeout=10000)
    # File drawer renders HTML-looking output as text and downloads exact bytes.
    page.locator('#files').click();page.get_by_role('button',name='hello.txt',exact=True).click()
    expect(page.locator('#preview-body')).to_contain_text('<script>window.bad=true</script>')
    assert page.evaluate('window.bad') is None
    with page.expect_download() as download:page.locator('#preview-download').click()
    assert Path(download.value.path()).read_bytes()==(t['root']/'hello.txt').read_bytes()
    page.locator('#preview-dialog [data-close]').click();page.locator('#files-dialog [data-close]').click()
    page.locator('#shell').click()
    expect(page.locator('#workspace .pane').nth(1).locator('.pane-status')).to_have_text('Connected',timeout=20000)
    page.locator('#workspace .pane').nth(1).locator('.terminal-host').click()
    type_command(page,"printf companion-proof > companion.txt")
    for _ in range(50):
        if (t['root']/'companion.txt').exists():break
        time.sleep(.1)
    assert (t['root']/'companion.txt').read_text()=='companion-proof'
    assert 'companion-proof' not in capture(t)
    page.locator('#shell').click()
    subprocess.run(['tmux','has-session','-t',f"=adk-companion-session-{t['id']}"],env=t['env'],check=True)
    page.locator('#shell').click()
    expect(page.locator('#workspace .pane').nth(1).locator('.pane-status')).to_have_text('Connected',timeout=20000)
    page.screenshot(path='/tmp/agentdeck-terminal-workspace-desktop.png')
    assert errors==[]

def test_real_terminal_history_preferences_pause_and_two_clients(page,browser,real_terminal):
    t=real_terminal;open_terminal(page,t)
    type_command(page,"for i in $(seq 1 180); do echo HISTORY-PROOF-$i; done")
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('HISTORY-PROOF-180')
    page.locator('#history').click();page.locator('#history-query').fill('HISTORY-PROOF-12')
    expect(page.locator('#history-count')).to_contain_text(' / 11',timeout=10000)
    expect(page.locator('#history-text mark.current')).to_have_text('HISTORY-PROOF-12')
    page.locator('#history-next').click();expect(page.locator('#history-count')).to_have_text('2 / 11')
    page.locator('#history-dialog [data-close]').click()
    page.locator('#preferences').click();page.locator('#font-size').fill('19');page.locator('#line-height').select_option('1.3');page.locator('#theme').select_option('black');page.locator('#settings-dialog [data-close]').click()
    page.reload();expect(page.locator('#connection')).to_have_text('Connected',timeout=20000)
    page.locator('#preferences').click();expect(page.locator('#font-size')).to_have_value('19');expect(page.locator('#theme')).to_have_value('black');page.locator('#settings-dialog [data-close]').click()
    page.locator('#pause').click();frozen=page.locator('#agent-pane .frozen');expect(frozen).to_be_visible();before=frozen.text_content()
    second=page.context.new_page();open_terminal(second,t);type_command(second,'echo AFTER-PAUSE-PROOF')
    expect(second.locator('#agent-terminal .xterm-screen')).to_contain_text('AFTER-PAUSE-PROOF')
    assert frozen.text_content()==before
    page.locator('#bottom').click();expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('AFTER-PAUSE-PROOF')
    second.close()
    page.set_viewport_size({'width':390,'height':480})
    expect(page.locator('#agent-terminal')).to_be_visible()
    assert page.locator('#agent-terminal').bounding_box()['height']>150
    assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
    page.screenshot(path='/tmp/agentdeck-terminal-workspace-mobile.png')
    # The desktop launcher CLI joins the same session over a real PTY.
    import pty,select
    master,slave=pty.openpty()
    child=subprocess.Popen([_binary(),'attach','session',str(t['id'])],stdin=slave,stdout=slave,stderr=slave,env={**t['env'],'TERM':'xterm-256color'})
    os.close(slave)
    try:
        output=b'';deadline=time.time()+15
        while time.time()<deadline and b'AFTER-PAUSE-PROOF' not in output:
            if select.select([master],[],[],.2)[0]:output+=os.read(master,65536)
        assert b'AFTER-PAUSE-PROOF' in output
        os.write(master,b'echo DESKTOP-PTY-PROOF\r')
        expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('DESKTOP-PTY-PROOF',timeout=10000)
        os.write(master,b'\x02d');child.wait(timeout=10)
        assert child.returncode==0
    finally:
        if child.poll() is None:child.terminate();child.wait(timeout=10)
        os.close(master)
    subprocess.run(['tmux','has-session','-t','=terminal-test'],env=t['env'],check=True)


def test_real_terminal_previews_failure_recovery_and_reconnect(page,real_terminal):
    t=real_terminal;open_terminal(page,t)
    png=base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aF9sAAAAASUVORK5CYII=')
    (t['root']/'image.png').write_bytes(png)
    # A small, valid PDF, including xref offsets, rendered by the real PDF.js worker.
    objects=[b'<< /Type /Catalog /Pages 2 0 R >>',b'<< /Type /Pages /Kids [3 0 R] /Count 1 >>',b'<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>']
    stream=b'BT /F1 24 Tf 30 100 Td (PDF preview proof) Tj ET'
    objects += [b'<< /Length '+str(len(stream)).encode()+b' >>\nstream\n'+stream+b'\nendstream', b'<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>']
    pdf=b'%PDF-1.4\n'; offsets=[0]
    for i,obj in enumerate(objects,1):
        offsets.append(len(pdf));pdf+=f'{i} 0 obj\n'.encode()+obj+b'\nendobj\n'
    xref=len(pdf);pdf+=b'xref\n0 6\n0000000000 65535 f \n'+b''.join(f'{v:010d} 00000 n \n'.encode() for v in offsets[1:])+f'trailer\n<< /Root 1 0 R /Size 6 >>\nstartxref\n{xref}\n%%EOF\n'.encode()
    (t['root']/'proof.pdf').write_bytes(pdf)
    page.locator('#files').click();page.get_by_role('button',name='image.png',exact=True).click()
    expect(page.locator('#preview-body img')).to_be_visible()
    page.wait_for_function("document.querySelector('#preview-body img').naturalWidth===1")
    page.locator('#preview-dialog [data-close]').click()
    page.get_by_role('button',name='proof.pdf',exact=True).click()
    expect(page.locator('#pdf-page')).to_have_text('Page 1 of 1',timeout=20000)
    assert page.locator('#preview-body canvas').evaluate('(c)=>c.width>250 && c.getContext("2d").getImageData(0,0,c.width,c.height).data.some((v,i)=>i%4!==3 && v<100)')
    page.screenshot(path='/tmp/agentdeck-terminal-pdf.png')
    page.locator('#preview-dialog [data-close]').click();page.locator('#files-dialog [data-close]').click()
    # A rejected upload is visible, does not type, and permits a successful retry.
    page.route('**/attachments',lambda route:route.fulfill(status=502,content_type='application/json',body='{"detail":"Target unavailable"}'))
    before=capture(t)
    page.locator('#file-input').set_input_files({'name':'retry.txt','mimeType':'text/plain','buffer':b'retry'})
    expect(page.locator('#notice')).to_have_text('Target unavailable')
    assert capture(t).rstrip()==before.rstrip()
    page.unroute('**/attachments')
    page.locator('#file-input').set_input_files({'name':'retry.txt','mimeType':'text/plain','buffer':b'retry'})
    expect(page.locator('#notice')).to_contain_text('Path inserted')
    page.keyboard.press('Control+C')
    # Stop only this isolated server's ttyd. The named URL must respawn it.
    children=subprocess.check_output(['ps','--ppid',str(t['proc'].pid),'-o','pid=,comm=']).decode().splitlines()
    ttyds=[int(line.split()[0]) for line in children if line.split()[1]=='ttyd']
    assert ttyds
    for pid in ttyds:os.kill(pid,15)
    expect(page.locator('#connection')).to_have_text('Reconnecting…',timeout=10000)
    expect(page.locator('#connection')).to_have_text('Connected',timeout=20000)
    page.locator('#agent-terminal').click();type_command(page,'echo RECONNECTED-PROOF')
    expect(page.locator('#agent-terminal .xterm-screen')).to_contain_text('RECONNECTED-PROOF')
