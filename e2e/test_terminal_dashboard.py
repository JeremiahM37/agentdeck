"""A real dashboard process in a controlling PTY, parsed as a terminal screen.

No keystroke/model mocks: requests hit the real server; attach hits real tmux.
"""
import codecs
import fcntl
import os
import pty
import select
import signal
import struct
import subprocess
import termios
import time

import pyte
import pytest
from conftest import _binary
from test_terminal_workspace import real_terminal


class Dashboard:
    def __init__(self,t,args=(),outer_tmux=False):
        self.master,self.slave=pty.openpty()
        self.original=termios.tcgetattr(self.slave)
        fcntl.ioctl(self.slave,termios.TIOCSWINSZ,struct.pack('HHHH',35,120,0,0))
        self.screen=pyte.Screen(120,35);self.stream=pyte.Stream(self.screen)
        self.decoder=codecs.getincrementaldecoder('utf-8')('replace')
        def controlling_terminal():
            os.setsid();fcntl.ioctl(0,termios.TIOCSCTTY,0)
        command=[_binary(),*args]
        if outer_tmux:command=["tmux","new-session","-s","dashboard-outer",*command]
        self.proc=subprocess.Popen(command,stdin=self.slave,stdout=self.slave,stderr=self.slave,
          env={**t['env'],'AGENTDECK_API':t['url'],'TERM':'xterm-256color','AGENTDECK_ATTACH_HOST':''},
          preexec_fn=controlling_terminal)
    def pump(self,duration=.1):
        end=time.monotonic()+duration
        while time.monotonic()<end:
            if select.select([self.master],[],[],.03)[0]:
                try:data=os.read(self.master,65536)
                except OSError:return
                self.stream.feed(self.decoder.decode(data))
    @property
    def text(self):return '\n'.join(self.screen.display)
    def wait(self,text,timeout=12):
        end=time.monotonic()+timeout
        while time.monotonic()<end:
            self.pump()
            if text in self.text:return
        raise AssertionError(f'Missing {text!r}:\n{self.text}')
    def send(self,data):os.write(self.master,data.encode());self.pump(.15)
    def resize(self,cols,rows):
        self.screen.resize(rows,cols)
        fcntl.ioctl(self.slave,termios.TIOCSWINSZ,struct.pack('HHHH',rows,cols,0,0))
        os.kill(self.proc.pid,signal.SIGWINCH);self.pump(.3)
    def quit(self):
        self.send('q');self.proc.wait(timeout=10)
        assert self.proc.returncode==0
        assert termios.tcgetattr(self.slave)==self.original
    def close(self):
        if self.proc.poll() is None:self.proc.terminate();self.proc.wait(timeout=10)
        os.close(self.master);os.close(self.slave)


def test_dashboard_search_rename_live_refresh_and_resize(real_terminal):
    t=real_terminal;d=Dashboard(t)
    try:
        d.wait('Real terminal');d.wait('LIVE')
        d.send('/no-matches');d.wait('No matching items')
        d.send('\x01\x0bReal terminal\r');d.wait('Real terminal')
        d.send('e');d.wait('Rename')
        d.send('\x01\x0bRenamed in dashboard\x13')
        d.wait('Rename completed')
        assert t['api'](f"/sessions/{t['id']}")['name']=='Renamed in dashboard'
        d.send('\x1b');d.pump(.3)
        # A new server-side record appears automatically; no manual refresh.
        subprocess.run(['tmux','new-session','-d','-s','second-dashboard-agent','bash --norc'],env=t['env'],check=True)
        t['api']('/sessions/adopt',{'target_id':t['target_id'],'tmux_session':'second-dashboard-agent','name':'Arrived while open','agent':'claude','workdir':str(t['root'])})
        d.wait('Arrived while open',timeout=8)
        for cols,rows in [(80,24),(45,16),(140,40)]:
            d.resize(cols,rows);d.wait('AgentDeck');d.wait('q quit')
        d.send('?');d.wait('Keyboard shortcuts');d.send('?')
        d.quit()
        subprocess.run(['tmux','has-session','-t','=terminal-test'],env=t['env'],check=True)
    finally:d.close()


@pytest.mark.parametrize("outer_tmux",[False,True])
def test_dashboard_native_attach_detach_returns_to_selection(real_terminal,outer_tmux):
    t=real_terminal;d=Dashboard(t,['console'],outer_tmux=outer_tmux)
    try:
        d.wait('Real terminal');d.send('\r')
        # The preview already contains a shell prompt. Wait for a real tmux
        # client before typing, especially while SSH is still connecting.
        deadline=time.monotonic()+12
        while time.monotonic()<deadline:
            d.pump()
            clients=subprocess.check_output(['tmux','list-clients','-t','terminal-test','-F','#{client_name}'],env=t['env'],text=True).strip()
            if clients:break
        assert clients, d.text
        d.wait('$')
        d.send('printf dashboard-native-proof > dashboard-proof.txt\r')
        end=time.monotonic()+5
        while time.monotonic()<end and not (t['root']/'dashboard-proof.txt').exists():d.pump()
        assert (t['root']/'dashboard-proof.txt').read_text()=='dashboard-native-proof'
        d.send('\x02d');d.wait('Detached. Session keeps running.');d.wait('Real terminal')
        d.send('\r');d.wait('$');d.send('\x02d');d.wait('Detached. Session keeps running.')
        d.quit()
    finally:d.close()


def test_dashboard_creates_task_with_named_project_and_multiline_prompt(real_terminal):
    t=real_terminal
    project=t['api']('/projects',{'name':'Dashboard project','target_id':t['target_id'],'repo_path':str(t['root'])})
    d=Dashboard(t)
    try:
        d.wait('Real terminal');d.send('2n');d.wait('New task')
        d.send('Task from keyboard\t');d.wait('Dashboard project')
        d.send('\tFirst line\rSecond line')
        d.send('\t\t\t\t');d.wait('Dispatch now')
        d.send('\x1b[D\x13');d.wait('Create task completed')
        tasks=t['api']('/tasks');task=next(r for r in tasks if r['title']=='Task from keyboard')
        assert task['project_id']==project['id']
        assert task['prompt']=='First line\nSecond line'
        assert task['status']=='backlog'
        d.quit()
    finally:d.close()


def test_dashboard_context_upload_preserves_local_bytes(real_terminal,tmp_path):
    t=real_terminal;pdf=tmp_path/'context.pdf';pdf.write_bytes(b'PDF\x00\xffcontext')
    d=Dashboard(t)
    try:
        d.wait('Real terminal');d.send('u');d.wait('Local file path')
        d.send(str(pdf)+'\x13');d.wait('Uploaded:')
        assert any(p.read_bytes()==pdf.read_bytes() for p in t['root'].rglob('*.pdf'))
        d.quit()
    finally:d.close()


def test_dashboard_edits_notification_settings_without_json(real_terminal):
    t=real_terminal;d=Dashboard(t)
    try:
        d.wait('Real terminal');d.send('7');d.wait('Notification settings')
        d.send('\t\tharmless-test-topic\x13');d.wait('Save settings completed')
        assert t['api']('/settings')['ntfy_topic']=='harmless-test-topic'
        d.quit()
    finally:d.close()
