"""Native history follows the source session's resolved launch configuration."""
import json,time
import pytest
from test_terminal_workspace import real_terminal
from test_native_history import prepare
from test_session_restore import request


def read_runtime(t):
    file=t['root']/'runtime.json'
    end=time.monotonic()+8
    while time.monotonic()<end and not file.exists():time.sleep(.05)
    return json.loads(file.read_text())


@pytest.mark.parametrize('agent',['claude','codex'])
def test_project_history_and_launch_configuration_survive_settings_edits(real_terminal,agent):
    t=real_terminal;cid,history,_=prepare(t,agent);before=history.read_bytes()
    key='CLAUDE_CONFIG_DIR' if agent=='claude' else 'CODEX_HOME'
    home=str(t['root']/'native-home');wrong=str(t['root']/'wrong-home')
    stub=t['root']/'configured-agent.py'
    stub.write_text('import os,sys,json,time\nfrom pathlib import Path\nPath("runtime.json").write_text(json.dumps({"argv":sys.argv[1:],"home":os.getenv('+repr(key)+'),"marker":os.getenv("CONFIG_MARKER"),"introduced":os.getenv("NEW_SETTING")}))\nprint("CONFIG READY",flush=True)\nwhile True:time.sleep(1)\n')
    spec={'name':agent,'command':'python3 '+str(stub),'env':{key:wrong,'CONFIG_MARKER':'global'},'fork_args':['--resume','{id}','--fork-session'] if agent=='claude' else ['fork','{id}'],'resume_id_args':['--resume','{id}'] if agent=='claude' else ['resume','{id}'],'trust_command':'printf "%s" "$CONFIG_MARKER" > '+str(t['root']/'trust-marker')}
    assert request(t,'PUT','/agents',[spec])[0]==200
    project=t['api']('/projects',{'name':'Configured project','target_id':t['target_id'],'repo_path':str(t['root']),'env':{key:home,'CONFIG_MARKER':'project','FAKE_CONFIG_SECRET':'private-fixture-value'}})
    assert request(t,'PATCH',f"/sessions/{t['id']}",{'project_id':project['id']})[0]==200
    source=f"/sessions/{t['id']}"
    listing=t['api'](source+'/conversations')
    assert [c['id'] for c in listing['conversations']]==[cid]
    first=t['api'](source+'/fork',{'conversation_id':cid,'name':'Original configuration'})
    runtime=read_runtime(t);assert runtime['home']==home and runtime['marker']=='project'
    assert (t['root']/'trust-marker').read_text()=='project'
    assert 'private-fixture-value' not in json.dumps(first)
    assert 'launch_config_json' not in first
    # Changing both global and project settings must not retarget an existing
    # launched session's history store or executable.
    changed={**spec,'command':'false','env':{key:wrong,'CONFIG_MARKER':'changed','NEW_SETTING':'should not leak in'}}
    assert request(t,'PUT','/agents',[changed])[0]==200
    assert request(t,'PATCH',f"/projects/{project['id']}",{'env':{key:wrong,'CONFIG_MARKER':'changed-project'}})[0]==200
    child=f"/sessions/{first['id']}"
    assert [c['id'] for c in t['api'](child+'/conversations')['conversations']]==[cid]
    assert request(t,'DELETE',child)[0]==200
    (t['root']/'runtime.json').unlink()
    resumed=t['api'](child+'/resume',{'conversation_id':cid})
    runtime=read_runtime(t)
    assert runtime['home']==home and runtime['marker']=='project' and runtime['introduced'] is None
    assert runtime['argv']==(['--resume',cid] if agent=='claude' else ['resume',cid])
    (t['root']/'runtime.json').unlink()
    forked=t['api'](f"/sessions/{resumed['id']}/fork",{'conversation_id':cid})
    runtime=read_runtime(t)
    assert runtime['home']==home and runtime['marker']=='project' and runtime['introduced'] is None
    assert ('--fork-session' in runtime['argv']) if agent=='claude' else runtime['argv'][0]=='fork'
    assert history.read_bytes()==before
