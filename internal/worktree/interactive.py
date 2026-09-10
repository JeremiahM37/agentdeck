"""Owned interactive Git worktrees. Never reset, force-remove, or delete branches."""
import json,os,pathlib,subprocess,sys
operation,raw=sys.argv[1:3]
p=json.loads(raw)
def git(repo,*args):
 r=subprocess.run(['git','-C',repo,*args],capture_output=True,text=True,timeout=90)
 if r.returncode:raise ValueError(r.stderr.strip() or r.stdout.strip() or 'Git command failed')
 return r.stdout.strip()
def within(path,root):
 return os.path.commonpath([os.path.realpath(path),root])==root
def claim_created_worktree(repo,dest,common,commit):
 # A post-checkout hook can fail after Git has allocated a complete worktree.
 # Claim only that exact new allocation, never an existing or substituted tree.
 if git(dest,'rev-parse','--show-toplevel')!=dest:raise ValueError('Created directory is not the worktree root')
 if git(dest,'rev-parse','--path-format=absolute','--git-common-dir')!=common:raise ValueError('Created worktree belongs to another repository')
 if git(dest,'symbolic-ref','--quiet','--short','HEAD')!=p['branch']:raise ValueError('Created worktree branch does not match')
 if git(dest,'rev-parse','HEAD')!=commit:raise ValueError('Created worktree revision does not match')
 owner=pathlib.Path(git(dest,'rev-parse','--absolute-git-dir'))/'agentdeck-owner'
 with owner.open('x') as file:file.write(p['token'])
try:
 repo=os.path.realpath(p['repo']);dest=os.path.realpath(p['path'])
 if not os.path.isabs(p['repo']) or not os.path.isabs(p['path']):raise ValueError('Worktree paths must be absolute')
 common=git(repo,'rev-parse','--path-format=absolute','--git-common-dir')
 if operation=='create':
  if p['base'].startswith('-') or not p['base']:raise ValueError('Choose a branch, tag or commit as the base')
  git(repo,'check-ref-format','--branch',p['branch'])
  commit=git(repo,'rev-parse','--verify','--end-of-options',p['base']+'^{commit}')
  if os.path.lexists(p['path']):raise ValueError('Worktree path already exists; nothing was changed')
  pathlib.Path(dest).parent.mkdir(parents=True,exist_ok=True)
  try:
   git(repo,'worktree','add','-b',p['branch'],'--',dest,commit)
  except ValueError as failure:
   if os.path.isdir(dest):
    try:
     claim_created_worktree(repo,dest,common,commit)
     p.update(repo=repo,path=dest,commit=commit,state='failed',error=str(failure))
    except (OSError,ValueError,subprocess.TimeoutExpired):
     # Keep the original Git failure. An allocation whose identity cannot be
     # proven stays unclaimed and must not be removed automatically.
     pass
   raise
  owner=pathlib.Path(git(dest,'rev-parse','--absolute-git-dir'))/'agentdeck-owner'
  with owner.open('x') as file:file.write(p['token'])
  p.update(repo=repo,path=dest,commit=commit,state='ready')
 elif operation in ('remove','check-remove'):
  if not os.path.isdir(dest):
   registrations=git(repo,'worktree','list','--porcelain','-z').split('\0')
   if any(entry=='worktree '+dest for entry in registrations):raise ValueError('Worktree directory is missing but still registered with Git; inspect it before cleanup')
   p['state']='removed';print(json.dumps({'workspace':p}));sys.exit(0)
  if git(dest,'rev-parse','--show-toplevel')!=dest:raise ValueError('Directory is not the recorded worktree root')
  if git(dest,'rev-parse','--path-format=absolute','--git-common-dir')!=common:raise ValueError('Worktree belongs to another repository')
  owner=pathlib.Path(git(dest,'rev-parse','--absolute-git-dir'))/'agentdeck-owner'
  if not owner.is_file() or owner.read_text()!=p['token']:raise ValueError('Worktree ownership does not match; nothing was removed')
  if git(dest,'symbolic-ref','--quiet','--short','HEAD')!=p['branch']:raise ValueError('Worktree branch changed; nothing was removed')
  panes=subprocess.run(['tmux','list-panes','-a','-F','#{pane_current_path}'],capture_output=True,text=True,timeout=10)
  if panes.returncode and not any(x in panes.stderr.lower() for x in ['no server running','no such file or directory']):raise ValueError('Could not check active terminals; nothing was removed')
  if any(within(cwd,dest) for cwd in panes.stdout.splitlines() if cwd):raise ValueError('A terminal is still using this worktree; end or leave it first')
  if git(dest,'--no-optional-locks','status','--porcelain','--untracked-files=all','--ignored=matching'):raise ValueError('Worktree contains changed, untracked or ignored files; commit or move them before removal')
  if operation=='remove':
   git(repo,'worktree','remove','--',dest)
   p['state']='removed'
 else:raise ValueError('Unknown worktree operation')
 print(json.dumps({'workspace':p}))
except (OSError,ValueError,subprocess.TimeoutExpired) as e:
 out={'error':str(e)}
 if p.get('state')=='failed' and p.get('error'):out['workspace']=p
 print(json.dumps(out));sys.exit(1)
