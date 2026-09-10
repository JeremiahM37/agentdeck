// Package skills manages project skill intent and target-local materialization.
// A skill source is always read on the target; AgentDeck stores only the
// attachment record and never copies a user's home, credentials, or config.
package skills

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/shellq"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	SourcePath  string `json:"source_path"`
	EntryName   string `json:"entry_name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Digest      string `json:"digest,omitempty"`
}

const script = `
import hashlib,json,os,sys,stat
def safe_parent(dst,base,create=True):
 cur=os.path.abspath(base); parent=os.path.dirname(os.path.abspath(dst))
 if os.path.islink(cur) or not os.path.isdir(cur): raise OSError('worktree root is unsafe')
 rel=os.path.relpath(parent,cur)
 if rel.startswith('..'+os.sep) or rel=='..': raise OSError('destination escapes worktree')
 fd=os.open(cur,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
 try:
  for part in rel.split(os.sep) if rel!='.' else []:
   try: nfd=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=fd)
   except FileNotFoundError:
    if not create: os.close(fd); return None,os.path.basename(dst)
    os.mkdir(part,dir_fd=fd); nfd=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=fd)
   os.close(fd); fd=nfd
  return fd,os.path.basename(dst)
 except:
  os.close(fd); raise
def gitpat(line):
 return '/'+line.replace('\\','\\\\').replace('*','\\*').replace('?','\\?').replace('[','\\[').replace(']','\\]')
def edit_exclude(repo,marker,line,remove):
 p=os.popen('git -C '+__import__('shlex').quote(repo)+' rev-parse --git-path info/exclude').read().strip()
 if not p: raise OSError('Git info/exclude unavailable')
 if not os.path.isabs(p): p=os.path.join(repo,p)
 lockp=p+'.agentdeck.lock'; import fcntl,tempfile
 with open(lockp,'a+') as lock:
  fcntl.flock(lock,fcntl.LOCK_EX)
  try: old=open(p,encoding='utf8').read()
  except OSError: old=''
  pair=marker+'\n'+gitpat(line)+'\n'
  new=old.replace(pair,'') if remove else (old if pair in old else old+('' if not old or old.endswith('\n') else '\n')+pair)
  if new!=old:
   fd,tmp=tempfile.mkstemp(prefix='.agentdeck-exclude-',dir=os.path.dirname(p)); os.write(fd,new.encode()); os.close(fd); os.replace(tmp,p)
 return p
def roots(a):
 repo=os.path.abspath(a.get('repo')) if a.get('repo') else ''; agent=a.get('agent','claude'); out=[]
 def add(kind,p):
  if p and os.path.isdir(p): out.append((kind,os.path.realpath(p)))
 cur=repo
 if repo:
  import subprocess
  try: gr=subprocess.check_output(['git','-C',repo,'rev-parse','--show-toplevel'],stderr=subprocess.DEVNULL,text=True).strip()
  except Exception: gr=repo
  while cur and os.path.commonpath([os.path.realpath(cur),os.path.realpath(gr)])==os.path.realpath(gr):
   add('repo:'+os.path.realpath(cur),os.path.join(cur,'.claude' if agent=='claude' else '.agents','skills'))
   if os.path.realpath(cur)==os.path.realpath(gr): break
   cur=os.path.dirname(cur)
 if agent=='claude': add('claude-user',os.path.join(os.environ.get('CLAUDE_CONFIG_DIR',os.path.expanduser('~/.claude')),'skills'))
 elif agent=='codex':
  add('codex-user',os.path.expanduser('~/.agents/skills')); add('codex-system','/etc/codex/skills')
 import hashlib
 for p in a.get('configured',[]): add('configured:'+hashlib.sha256(os.path.realpath(p).encode()).hexdigest()[:12],p)
 return out
def discover(a):
 out=[]; seen=set()
 for kind,p in roots(a):
  try: names=sorted(os.listdir(p))
  except OSError: continue
  for n in names:
   q=os.path.join(p,n); md=os.path.join(q,'SKILL.md')
   if not os.path.isdir(q) or not os.path.isfile(md): continue
   key=(os.path.realpath(p),n)
   if key in seen: continue
   seen.add(key); name=n; desc=''
   try:
    lines=open(md,encoding='utf8').read(8192).splitlines()
    for line in lines:
     if line.lower().startswith('name:'): name=line.split(':',1)[1].strip() or n
     if line.lower().startswith('description:'): desc=line.split(':',1)[1].strip()
   except OSError: pass
   out.append({'id':kind+'/'+n,'name':name,'source':kind,'source_path':q,'entry_name':n,'kind':'dir','description':desc})
 return out
def main(a):
 op=a.get('op')
 if op=='discover': return {'skills':discover(a)}
 if op=='materialize':
  src=os.path.realpath(a['source']); dst=os.path.abspath(a['target']); fd,name=safe_parent(dst,a['base'])
  if not os.path.isdir(src) or not os.path.isfile(os.path.join(src,'SKILL.md')): return {'error':'skill source is unavailable'}
  try: st=os.lstat(name,dir_fd=fd)
  except FileNotFoundError: st=None
  if st:
   if os.path.abspath(src)==dst: os.close(fd); return {'preexisting':True}
   if os.path.islink(dst) and os.path.realpath(dst)==src and a.get('owned'): os.close(fd); return {'already':True}
   os.close(fd); return {'error':'skill destination already exists'}
  try: os.symlink(src,name,dir_fd=fd)
  except (OSError,NotImplementedError) as e: os.close(fd); return {'error':'symlink materialization unavailable: '+str(e)}
  os.close(fd)
  return {'created':True}
 if op=='remove':
  dst=os.path.abspath(a['target']); src=os.path.realpath(a['source']); fd,name=safe_parent(dst,a['base'],False)
  if fd is None: return {'removed':False,'missing':True}
  try: st=os.lstat(name,dir_fd=fd)
  except FileNotFoundError: os.close(fd); return {'removed':False,'missing':True}
  try: linked=os.path.realpath(os.path.join(os.path.dirname(dst),os.readlink(name,dir_fd=fd)))
  except OSError: os.close(fd); return {'error':'owned skill link changed; preserving target'}
  if not stat.S_ISLNK(st.st_mode) or linked!=src: os.close(fd); return {'error':'owned skill link changed; preserving target'}
  os.unlink(name,dir_fd=fd); os.close(fd); return {'removed':True}
 if op=='exclude':
  repo=os.path.abspath(a['repo']); return {'path':edit_exclude(repo,a['marker'],a['line'],False)}
 if op=='unexclude':
  repo=os.path.abspath(a['repo']); p=edit_exclude(repo,a['marker'],a['line'],True); return {'path':p}
 return {'error':'unknown operation'}
main_in=json.loads(sys.argv[1]); print(json.dumps(main(main_in),separators=(',',':')))
`

func run(ctx context.Context, ex executor.Executor, in map[string]any) (map[string]any, error) {
	b, _ := json.Marshal(in)
	cmd := "python3 -c " + shellq.Quote(script) + " " + shellq.Quote(string(b))
	r, err := ex.Run(ctx, cmd, executor.RunOpts{Timeout: 30})
	if err != nil {
		return nil, err
	}
	if !r.OK() {
		return nil, fmt.Errorf("skill target command failed: %s", strings.TrimSpace(r.Stderr))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &out); err != nil {
		return nil, fmt.Errorf("invalid skill target response: %w", err)
	}
	if msg, _ := out["error"].(string); msg != "" {
		return out, errors.New(msg)
	}
	return out, nil
}

func supported(agent string) bool { return agent == "claude" || agent == "codex" }
func rel(agent, name string) string {
	if agent == "claude" {
		return filepath.Join(".claude", "skills", name)
	}
	return filepath.Join(".agents", "skills", name)
}
func configured(p *store.Project) []string {
	var x []string
	_ = json.Unmarshal([]byte(p.SkillSourcesJSON), &x)
	if x == nil {
		x = []string{}
	}
	return x
}

func Discover(ctx context.Context, ex executor.Executor, p *store.Project, agent string) ([]Skill, error) {
	if !supported(agent) {
		return nil, fmt.Errorf("skills are unsupported for agent %q", agent)
	}
	repo := ""
	sources := []string{}
	if p != nil {
		repo = p.RepoPath
		sources = configured(p)
	}
	out, err := run(ctx, ex, map[string]any{"op": "discover", "repo": repo, "agent": agent, "configured": sources})
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(out["skills"])
	var x []Skill
	if err := json.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	return x, nil
}

func find(xs []Skill, id string) (Skill, error) {
	var hit *Skill
	for i := range xs {
		if xs[i].ID == id {
			if hit != nil {
				return Skill{}, fmt.Errorf("skill id %q is ambiguous", id)
			}
			v := xs[i]
			hit = &v
		}
	}
	if hit == nil {
		return Skill{}, fmt.Errorf("skill %q was not found on target", id)
	}
	return *hit, nil
}

func Materialize(ctx context.Context, ex executor.Executor, p *store.Project, x Skill, agent, workdir string, attachmentID int64, owned bool) (string, string, error) {
	name := x.EntryName
	source := x.SourcePath
	// The source remains the target-local path discovered from the project
	// roots. Worktrees link back to that source; this avoids silently turning a
	// project skill into a missing path in a fresh worktree.
	dst := filepath.Join(workdir, rel(agent, name))
	out, err := run(ctx, ex, map[string]any{"op": "materialize", "source": source, "target": dst, "base": workdir, "owned": owned})
	if err != nil {
		return "", "", err
	}
	marker := Marker(attachmentID, workdir)
	line := filepath.ToSlash(rel(agent, name))
	if pre, _ := out["preexisting"].(bool); pre {
		return dst, source, nil
	}
	if _, err = run(ctx, ex, map[string]any{"op": "exclude", "repo": workdir, "line": line, "marker": marker}); err != nil {
		// The link is ours even if Git metadata is unavailable. Best effort
		// rollback keeps a failed attach from leaving an untracked orphan.
		if created, _ := out["created"].(bool); created {
			_, _ = run(ctx, ex, map[string]any{"op": "remove", "source": source, "target": dst, "base": workdir})
		}
		return "", "", err
	}
	return dst, source, nil
}

func Remove(ctx context.Context, ex executor.Executor, p *store.Project, x *store.ProjectSkill, workdir string) error {
	source := x.SourcePath
	if _, err := run(ctx, ex, map[string]any{"op": "remove", "source": source, "target": filepath.Join(workdir, x.TargetRel), "base": workdir}); err != nil {
		return err
	}
	_, err := run(ctx, ex, map[string]any{"op": "unexclude", "repo": workdir, "line": filepath.ToSlash(x.TargetRel), "marker": Marker(x.ID, workdir)})
	return err
}

func Marker(id int64, workdir string) string {
	h := sha256.Sum256([]byte(filepath.Clean(workdir)))
	return fmt.Sprintf("# agentdeck-owned-skill:%d:%x", id, h[:6])
}

func Reassert(ctx context.Context, ex executor.Executor, db *store.DB, p *store.Project, agent, workdir string) error {
	rows, err := db.ProjectSkills(p.ID, agent)
	if err != nil {
		return err
	}
	for _, x := range rows {
		s := Skill{ID: x.SkillID, SourcePath: x.SourcePath, EntryName: x.EntryName}
		owned := false
		if mats, me := db.Materializations(x.ID); me == nil {
			for _, m := range mats {
				if m.WorktreePath == workdir {
					owned = true
					break
				}
			}
		}
		dst := filepath.Join(workdir, rel(agent, s.EntryName))
		src := s.SourcePath
		if err := db.UpsertMaterialization(&store.SkillMaterialization{AttachmentID: x.ID, TargetID: x.TargetID, WorktreePath: workdir, TargetPath: dst, SourcePath: src, TargetRel: x.TargetRel}); err != nil {
			return err
		}
		var e error
		dst, src, e = Materialize(ctx, ex, p, s, agent, workdir, x.ID, owned)
		if e != nil {
			return fmt.Errorf("skill %q: %w", x.SkillID, e)
		}
		if err := db.UpsertMaterialization(&store.SkillMaterialization{AttachmentID: x.ID, TargetID: x.TargetID, WorktreePath: workdir, TargetPath: dst, SourcePath: src, TargetRel: x.TargetRel}); err != nil {
			return err
		}
	}
	return nil
}

func Clean(ctx context.Context, ex executor.Executor, db *store.DB, p *store.Project, workdir string) error {
	rows, err := db.MaterializationsAt(workdir)
	if err != nil {
		return err
	}
	for _, m := range rows {
		x, e := db.ProjectSkill(m.AttachmentID)
		if e != nil {
			continue
		}
		if e = Remove(ctx, ex, p, x, workdir); e != nil {
			continue
		}
		_ = db.DeleteMaterialization(m.ID)
	}
	return nil
}
