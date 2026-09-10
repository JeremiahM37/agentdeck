import json,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
import native_identity as module

class IdentityTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name);self.proc=self.root/'proc';self.home=self.root/'home';self.workspace=str(self.root/'work')
        self.marker='a'*32;self.cid='11111111-1111-4111-8111-111111111111'
        self.process(100,0,'10');self.process(200,100,'20')
        (self.proc/'self').symlink_to(self.proc/'100')
        (self.root/'machine-id').write_text('fixture-machine')
        real_path=Path
        def paths(*parts):
            if parts and parts[0]=='/proc':return real_path(self.proc,*parts[1:])
            if parts and parts[0]=='/etc/machine-id':return real_path(self.root/'machine-id')
            return real_path(*parts)
        self.path_patch=patch.object(module,'Path',side_effect=paths);self.path_patch.start();self.addCleanup(self.path_patch.stop)
        self.pane='\t'.join(['$1','@1','%1','100',self.marker])+'\n'
        self.tmux=patch.object(module.subprocess,'check_output',return_value=self.pane).start();self.addCleanup(patch.stopall)
    def process(self,pid,parent,start):
        folder=self.proc/str(pid);folder.mkdir(parents=True,exist_ok=True)
        fields=['S',str(parent)]+['0']*17+[start]
        (folder/'stat').write_text(f'{pid} (name with ) parentheses) '+' '.join(fields))
    def claude(self,**overrides):
        folder=self.home/'sessions';folder.mkdir(parents=True,exist_ok=True)
        row=dict(pid=200,procStart='20',sessionId=self.cid,cwd=self.workspace,kind='interactive',entrypoint='cli');row.update(overrides)
        (folder/'200.json').write_text(json.dumps(row))
    def codex(self,cid=None,source='cli',outside=False):
        cid=cid or self.cid;folder=(self.root/'outside') if outside else (self.home/'sessions');folder.mkdir(parents=True,exist_ok=True)
        file=folder/(cid+'.jsonl');file.write_text(json.dumps(dict(type='session_meta',payload=dict(id=cid,cwd=self.workspace,source=source)))+'\n')
        fd=self.proc/'200/fd';fd.mkdir(exist_ok=True)
        (fd/str(len(list(fd.iterdir())))).symlink_to(file)
        exe=self.proc/'200/exe'
        if not exe.exists():
            binary=self.root/'codex';binary.touch();exe.symlink_to(binary)
    def read(self,agent):return module.native_identity(agent,self.workspace,str(self.home),'owned',self.marker)
    def test_claude_process_start_and_interactive_identity(self):
        self.claude();self.assertEqual(self.read('claude'),dict(state='identified',id=self.cid))
        for fields in (dict(procStart='19'),dict(pid=201),dict(kind='background'),dict(cwd='/elsewhere')):
            self.claude(**fields);self.assertEqual(self.read('claude')['state'],'unavailable')
    def test_claude_machine_and_pid_namespace(self):
        self.claude(pidDomain='linux:fixture-machine:pid:[123]')
        with patch.object(module.os,'readlink',return_value='pid:[123]'):
            self.assertEqual(self.read('claude')['state'],'identified')
        with patch.object(module.os,'readlink',return_value='pid:[456]'):
            self.assertEqual(self.read('claude')['state'],'unavailable')
    def test_reused_pane_during_observation(self):
        self.claude();self.tmux.side_effect=[self.pane,self.pane.replace('%1','%2')]
        self.assertEqual(self.read('claude')['state'],'changed')
    def test_unmarked_live_pane_still_has_process_identity(self):
        self.claude();self.tmux.return_value=self.pane.replace(self.marker,'')
        result=module.native_identity('claude',self.workspace,str(self.home),'owned','')
        self.assertEqual(result,dict(state='identified',id=self.cid))
        self.tmux.side_effect=[self.tmux.return_value,self.tmux.return_value.replace('%1','%9')]
        result=module.native_identity('claude',self.workspace,str(self.home),'owned','')
        self.assertEqual(result['state'],'changed')
    def test_wrong_tmux_identity(self):
        self.claude();self.tmux.return_value=self.pane.replace(self.marker,'b'*32)
        self.assertEqual(self.read('claude')['state'],'changed')
    def test_codex_live_descriptor_and_subagent(self):
        self.codex();self.codex('22222222-2222-4222-8222-222222222222',source={'subagent':{}})
        self.assertEqual(self.read('codex'),dict(state='identified',id=self.cid))
    def test_codex_ambiguity(self):
        self.codex();self.codex('22222222-2222-4222-8222-222222222222')
        self.assertEqual(self.read('codex')['state'],'ambiguous')
    def test_other_profile_and_non_agent_process(self):
        self.codex(outside=True);self.assertEqual(self.read('codex')['state'],'unavailable')
        self.codex();exe=self.proc/'200/exe';exe.unlink();other=self.root/'less';other.touch();exe.symlink_to(other)
        self.assertEqual(self.read('codex')['state'],'unavailable')
    def test_non_descendant_is_ignored(self):
        self.claude();self.process(200,999,'20');self.assertEqual(self.read('claude')['state'],'unavailable')

if __name__=='__main__':unittest.main()
