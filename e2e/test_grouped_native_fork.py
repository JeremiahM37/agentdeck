"""Web and PTY forks pass Claude an exact target-side history path."""
import json
import shlex
from pathlib import Path
import re
import time
import urllib.request
import uuid

import pytest
from playwright.sync_api import expect
from test_terminal_workspace import real_terminal
from test_terminal_dashboard import Dashboard
from test_multi_workspace import grouped
from test_native_search import open_search


def prepare(t):
    parent = grouped(t)
    home = t['root'].parent / 'native profile'
    folder = home / 'projects' / re.sub(r'[^a-zA-Z0-9]', '-', parent['workdir'])
    folder.mkdir(parents=True)
    cid = str(uuid.uuid4())
    source = folder / (cid + '.jsonl')
    source.write_text(json.dumps({'sessionId': cid, 'cwd': parent['workdir'], 'type': 'user',
                                 'message': {'role': 'user', 'content': 'Grouped history sentinel'}}) + '\n')
    assets = folder / cid / 'subagents'
    assets.mkdir(parents=True)
    (assets / 'worker.jsonl').write_text('Nested history remains native-owned\n')
    proof = t['root'].parent / 'native-fork-proof.json'
    stub = t['root'].parent / 'native-fork-agent'
    stub.write_text('#!/usr/bin/env python3\nimport os,sys,json,time\nfrom pathlib import Path\n'
                    'Path(os.environ["FORK_PROOF"]).write_text(json.dumps({"argv":sys.argv[1:],"cwd":os.getcwd()}))\n'
                    'print("GROUPED NATIVE FORK READY",flush=True)\nwhile True:time.sleep(1)\n')
    stub.chmod(0o700)
    specs = [{'name': 'claude', 'command': str(stub), 'fork_args': ['--resume', '{id}', '--fork-session'],
              'env': {'CLAUDE_CONFIG_DIR': str(home), 'FORK_PROOF': str(proof)}},
             {'name': 'codex', 'command': 'codex', 'env': {'CODEX_HOME': str(home / 'empty-codex')}}]
    request = urllib.request.Request(t['url'] + '/api/agents', method='PUT', data=json.dumps(specs).encode(),
                                     headers={'Content-Type': 'application/json'})
    with urllib.request.urlopen(request):
        pass
    return parent, source, proof, {str(p): p.read_bytes() for p in home.rglob('*') if p.is_file()}


def check(t, parent, source, proof, original):
    deadline = time.monotonic() + 8
    while not proof.exists() and time.monotonic() < deadline:
        time.sleep(.05)
    observed = json.loads(proof.read_text())
    assert observed['argv'] == ['--resume', str(source), '--fork-session']
    assert observed['cwd'] != parent['workdir']
    child = next(r for r in t['api']('/sessions') if r['workdir'] == observed['cwd'])
    assert len(child['workspace']['repositories']) == 2
    home = source.parents[2]
    assert {str(p): p.read_bytes() for p in home.rglob('*') if p.is_file()} == original


@pytest.mark.parametrize('width', [390, 1440])
def test_browser_grouped_claude_fork(page, real_terminal, width):
    t = real_terminal
    parent, source, proof, original = prepare(t)
    errors = []
    page.on('pageerror', lambda e: errors.append(str(e)))
    page.set_viewport_size({'width': width, 'height': 900})
    page.goto(t['url'])
    dialog = open_search(page)
    dialog.get_by_label('Conversation text').fill('Grouped history sentinel')
    dialog.get_by_label('Agent', exact=True).select_option('claude')
    dialog.get_by_role('button', name='Search', exact=True).click()
    expect(dialog.locator('.ns-result')).to_have_count(1, timeout=15000)
    dialog.locator('.ns-result').click()
    dialog.get_by_role('button', name='Fork conversation', exact=True).click()
    dialog.get_by_label('Fork workspace').select_option('isolated')
    assert dialog.evaluate('(el)=>el.scrollWidth<=el.clientWidth')
    release=t['root'].parent/'release-native-fork'
    hook=Path(parent['workspace']['repositories'][1]['worktree']['repo'])/'.git/hooks/post-checkout'
    hook.write_text('#!/bin/sh\nwhile [ ! -f '+shlex.quote(str(release))+' ]; do sleep 0.05; done\n')
    hook.chmod(0o700)
    try:
        with page.expect_response(lambda r:r.request.method=='POST' and r.url.endswith('/fork')) as response:
            dialog.get_by_role('button', name='Create fork', exact=True).click()
        assert response.value.status==202
        expect(dialog).not_to_be_visible(timeout=20000)
        card=page.locator('.scard',has_text='Conversation fork')
        expect(card.get_by_role('button',name='Setting up',exact=True)).to_be_disabled()
        expect(card.locator('.spane')).to_contain_text('Second repository: creating',timeout=15000)
        assert not proof.exists()
        page.reload()
        expect(page.locator('.scard',has_text='Conversation fork').get_by_role('button',name='Setting up',exact=True)).to_be_disabled()
        release.touch()
        check(t, parent, source, proof, original)
    finally:
        release.touch()
    assert not errors


def test_terminal_grouped_claude_fork(real_terminal):
    t = real_terminal
    parent, source, proof, original = prepare(t)
    dashboard = Dashboard(t)
    try:
        dashboard.wait('Grouped review')
        dashboard.send('F'); dashboard.wait('Search saved conversations')
        dashboard.send('history sentinel\t\t\x1b[C\x13')
        dashboard.wait('1 conversation'); dashboard.send('\r')
        dashboard.wait('MATCHING MESSAGE'); dashboard.send('f')
        dashboard.wait('Fork whole saved conversation')
        dashboard.send('\t\t\x1b[C\x13')
        dashboard.wait('Fork workspace setup started.', timeout=20)
        check(t, parent, source, proof, original)
        dashboard.quit()
    finally:
        dashboard.close()
