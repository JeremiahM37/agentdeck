"""Grouped review against real owned worktrees and a live tmux session."""
from pathlib import Path
import subprocess
import pytest
from playwright.sync_api import expect
from test_terminal_workspace import real_terminal, terminal_tool
from test_interactive_worktree import setup
from test_terminal_dashboard import Dashboard


def grouped(t):
    primary,_=setup(t)
    other=t['root'].parent/'other';other.mkdir()
    subprocess.run(['git','init','-q',str(other)],check=True)
    (other/'second.txt').write_text('original\n')
    subprocess.run(['git','-C',str(other),'add','.'],check=True)
    subprocess.run(['git','-C',str(other),'-c','user.name=Test','-c','user.email=test@example.invalid','commit','-qm','base'],check=True)
    extra=t['api']('/projects',{'name':'Second repository','target_id':t['target_id'],'repo_path':str(other)})
    row=t['api']('/sessions',{'name':'Grouped review','project_id':primary['id'],'worktree':{'extra_repositories':[{'project_id':extra['id']}]}})
    repos=row['workspace']['repositories']
    (Path(repos[0]['worktree']['path'])/'first.txt').write_text('PRIMARY DIFF SENTINEL\n')
    (Path(repos[1]['worktree']['path'])/'second.txt').write_text('SECOND DIFF SENTINEL\n')
    return row


@pytest.mark.parametrize('width',[390,1440])
def test_grouped_browser_review_switches_repository(page,real_terminal,width):
    t=real_terminal;row=grouped(t);errors=[]
    page.on('pageerror',lambda e:errors.append(str(e)))
    page.set_viewport_size({'width':width,'height':900})
    page.goto(f"{t['url']}/terminal/session/{row['id']}")
    expect(page.locator('#connection')).to_have_text('Connected',timeout=20000)
    terminal_tool(page,'#review')
    dialog=page.get_by_role('dialog',name='Review changes')
    expect(dialog.locator('.review-patch')).to_contain_text('PRIMARY DIFF SENTINEL')
    dialog.get_by_label('Repository',exact=True).select_option('1')
    expect(dialog.locator('.review-patch')).to_contain_text('SECOND DIFF SENTINEL')
    expect(dialog.locator('.review-patch')).not_to_contain_text('PRIMARY DIFF SENTINEL')
    dialog.get_by_label('Repository',exact=True).select_option('0')
    expect(dialog.locator('.review-patch')).to_contain_text('PRIMARY DIFF SENTINEL')
    assert dialog.evaluate('(e)=>e.scrollWidth<=e.clientWidth+1')
    dialog.get_by_role('button',name='Close review').click()
    expect(page.locator('#connection')).to_have_text('Connected')
    assert not errors


def test_grouped_terminal_review_switches_repository(real_terminal):
    t=real_terminal;grouped(t);d=Dashboard(t)
    try:
        d.wait('Grouped review');d.send('/Grouped review\r');d.send('v')
        d.wait('PRIMARY DIFF SENTINEL');d.wait('Tab repo')
        d.send('\t');d.wait('Second repository');d.wait('SECOND DIFF SENTINEL')
        d.send('\t');d.wait('PRIMARY DIFF SENTINEL')
        d.send('\x1b');d.wait('Grouped review');d.quit()
    finally:d.close()
