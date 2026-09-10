package worktree

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/executor"
)

func TestMultiWorkspaceTargetPreflightPreservesRepositories(t *testing.T) {
	for _, bin := range []string{"git", "python3"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " unavailable")
		}
	}
	root := t.TempDir()
	git := func(repo string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	var sources []RepositorySource
	indexes := map[string][]byte{}
	for _, name := range []string{"api space", "web"} {
		repo := filepath.Join(root, name)
		if err := os.Mkdir(repo, 0700); err != nil {
			t.Fatal(err)
		}
		git(repo, "init", "-q")
		if err := os.WriteFile(filepath.Join(repo, "file"), []byte("original\n"), 0600); err != nil {
			t.Fatal(err)
		}
		git(repo, "add", ".")
		git(repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
		git(repo, "tag", "base-tag")
		if err := os.WriteFile(filepath.Join(repo, "file"), []byte("source edit\n"), 0600); err != nil {
			t.Fatal(err)
		}
		index, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
		if err != nil {
			t.Fatal(err)
		}
		indexes[repo] = index
		sources = append(sources, RepositorySource{Name: name, Repo: repo, Base: "base-tag"})
	}
	newPlan := func() *Interactive {
		t.Helper()
		plan, err := PlanMultiWorkspace(sources, 19, InteractiveOptions{Branch: "feature/grouped"})
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	ex := executor.NewLocal()
	ctx := context.Background()
	plan := newPlan()
	if err := RunInteractive(ctx, ex, "check-create", plan); err != nil {
		t.Fatal(err)
	}
	for _, entry := range plan.Repositories {
		if entry.Worktree.Commit != git(entry.Worktree.Repo, "rev-parse", "base-tag") {
			t.Fatal("preflight did not resolve repository base")
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(sources[0].Repo, alias); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "existing-linked")
	git(sources[0].Repo, "worktree", "add", "--detach", linked, "HEAD")
	for _, tc := range []struct {
		name   string
		change func(*Interactive)
		want   string
	}{
		{"symlink alias", func(p *Interactive) { p.Repositories[1].Worktree.Repo = alias }, "same Git repository"},
		{"linked worktree alias", func(p *Interactive) { p.Repositories[1].Worktree.Repo = linked }, "same Git repository"},
		{"missing later base", func(p *Interactive) { p.Repositories[1].Worktree.Base = "missing-ref" }, "revision"},
		{"escaping child", func(p *Interactive) { p.Repositories[1].Worktree.Path = filepath.Join(root, "escape") }, "distinct children"},
		{"duplicate token", func(p *Interactive) { p.Repositories[1].Worktree.Token = p.Token }, "separate ownership"},
		{"invalid branch", func(p *Interactive) {
			p.Branch = "bad..branch"
			for _, r := range p.Repositories {
				r.Worktree.Branch = p.Branch
			}
		}, "valid branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newPlan()
			tc.change(p)
			before, _ := json.Marshal(p)
			err := RunInteractive(ctx, ex, "check-create", p)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
			after, _ := json.Marshal(p)
			if !bytes.Equal(before, after) {
				t.Fatal("failed preflight partially replaced plan")
			}
		})
	}
	git(sources[1].Repo, "branch", "feature/grouped")
	if err := RunInteractive(ctx, ex, "check-create", newPlan()); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("later branch collision accepted: %v", err)
	}
	for repo, before := range indexes {
		after, err := os.ReadFile(filepath.Join(repo, ".git", "index"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("preflight changed source index")
		}
		body, err := os.ReadFile(filepath.Join(repo, "file"))
		if err != nil || string(body) != "source edit\n" {
			t.Fatal("preflight changed source edits")
		}
	}
	if _, err := os.Stat(plan.Path); !os.IsNotExist(err) {
		t.Fatal("preflight created allocation")
	}
	if out, _ := exec.Command("git", "-C", sources[0].Repo, "branch", "--list", "feature/grouped").Output(); len(out) != 0 {
		t.Fatal("preflight created a branch in the first repository")
	}
}

func TestMultiWorkspacePlansSeparateOwnedRepositoriesAndBases(t *testing.T) {
	sources := []RepositorySource{{Name: "../API", Repo: "/repos/backend", Base: "main"}, {Name: "API", Repo: "/repos/frontend", Base: "develop"}, {Name: "資料", Repo: "/repos/library"}}
	p, err := PlanMultiWorkspace(sources, 8, InteractiveOptions{Branch: "feature/shared"})
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[string]bool{p.Token: true}
	paths := map[string]bool{}
	for i, r := range p.Repositories {
		w := r.Worktree
		if filepath.Dir(w.Path) != p.Path || w.Path == p.Path || paths[w.Path] || tokens[w.Token] || w.Token == "" || w.Branch != "feature/shared" || w.Repo != sources[i].Repo {
			t.Fatal("workspace allocation boundaries or ownership were lost")
		}
		paths[w.Path], tokens[w.Token] = true, true
	}
	if p.Repositories[1].Worktree.Base != "develop" || p.Repositories[2].Worktree.Base != "HEAD" {
		t.Fatal("per-repository bases were lost")
	}
	raw, _ := json.Marshal(p)
	var public Interactive
	json.Unmarshal(raw, &public)
	public.RedactOwnership()
	encoded, _ := json.Marshal(public)
	for token := range tokens {
		if strings.Contains(string(encoded), token) {
			t.Fatal("nested ownership token exposed")
		}
	}
	if p.Token == "" || p.Repositories[0].Worktree.Token == "" {
		t.Fatal("redaction changed durable ownership")
	}
	for _, invalid := range [][]RepositorySource{nil, {{Repo: "relative"}}, {{Repo: "/same"}, {Repo: "/same/../same"}}} {
		if _, err := PlanMultiWorkspace(invalid, 1, InteractiveOptions{}); err == nil {
			t.Fatal("invalid repository set accepted")
		}
	}
}

func TestMultiWorkspaceCreationFailureAndCleanup(t *testing.T) {
	for _, bin := range []string{"git", "python3", "tmux"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " unavailable")
		}
	}
	socket, err := os.MkdirTemp("", "adk-group-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", socket)
	t.Setenv("TMUX", "")
	t.Cleanup(func() { exec.Command("tmux", "kill-server").Run(); os.RemoveAll(socket) })
	root := t.TempDir()
	git := func(repo string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	var sources []RepositorySource
	for _, name := range []string{"api", "web"} {
		repo := filepath.Join(root, name)
		os.Mkdir(repo, 0700)
		git(repo, "init", "-q")
		os.WriteFile(filepath.Join(repo, "file"), []byte("original\n"), 0600)
		git(repo, "add", ".")
		git(repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
		sources = append(sources, RepositorySource{Name: name, Repo: repo})
	}
	ex := executor.NewLocal()
	ctx := context.Background()
	for _, fails := range []bool{false, true} {
		t.Run(fmt.Sprint(fails), func(t *testing.T) {
			plan, err := PlanMultiWorkspace(sources, 21, InteractiveOptions{})
			if err != nil {
				t.Fatal(err)
			}
			hook := filepath.Join(sources[1].Repo, ".git/hooks/post-checkout")
			if fails {
				os.WriteFile(hook, []byte("#!/bin/sh\nprintf artifact > setup-artifact\necho later-failure >&2\nexit 1\n"), 0700)
				defer os.Remove(hook)
			}
			err = RunInteractive(ctx, ex, "create", plan)
			if fails {
				if err == nil || !strings.Contains(err.Error(), "later-failure") || plan.State != "failed" {
					t.Fatalf("failure lost: %v %#v", err, plan)
				}
			} else if err != nil || plan.State != "ready" {
				t.Fatalf("creation: %v %#v", err, plan)
			}
			for _, r := range plan.Repositories {
				if _, err := os.Stat(filepath.Join(r.Worktree.Path, "file")); err != nil {
					t.Fatal(err)
				}
			}
			// A dirty last repository must prevent any removal of the first one.
			artifact := filepath.Join(plan.Repositories[1].Worktree.Path, "setup-artifact")
			os.WriteFile(artifact, []byte("keep"), 0600)
			if err := RunInteractive(ctx, ex, "remove", plan); err == nil {
				t.Fatal("dirty group removed")
			}
			if _, err := os.Stat(plan.Repositories[0].Worktree.Path); err != nil {
				t.Fatal("first repository removed before later preflight")
			}
			os.Remove(artifact)
			if out, err := exec.Command("tmux", "new-session", "-d", "-s", "group-root", "-c", plan.Path, "sleep 600").CombinedOutput(); err != nil {
				t.Fatalf("tmux: %s", out)
			}
			if err := RunInteractive(ctx, ex, "remove", plan); err == nil || !strings.Contains(err.Error(), "terminal") {
				t.Fatalf("active workspace root was not protected: %v", err)
			}
			exec.Command("tmux", "kill-session", "-t", "group-root").Run()
			if err := RunInteractive(ctx, ex, "remove", plan); err != nil {
				t.Fatal(err)
			}
			if plan.State != "removed" {
				t.Fatal(plan.State)
			}
			for _, r := range plan.Repositories {
				if _, err := os.Stat(r.Worktree.Path); !os.IsNotExist(err) {
					t.Fatal("child still exists")
				}
				git(r.Worktree.Repo, "rev-parse", r.Worktree.Branch)
			}
			if _, err := os.Stat(filepath.Join(plan.Path, ".agentdeck-state.json")); err != nil {
				t.Fatal("durable removal receipt lost")
			}
		})
	}
}

func TestMultiWorkspaceSupervisorDeathKeepsCheckoutGuarded(t *testing.T) {
	for _, bin := range []string{"git", "python3", "tmux"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " unavailable")
		}
	}
	socket, err := os.MkdirTemp("", "adk-orphan-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", socket)
	t.Setenv("TMUX", "")
	t.Cleanup(func() { exec.Command("tmux", "kill-server").Run(); os.RemoveAll(socket) })
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	os.Mkdir(repo, 0700)
	git := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %s", out)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(repo, "file"), []byte("base"), 0600)
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base")
	started, release := filepath.Join(root, "started"), filepath.Join(root, "release")
	hook := "#!/bin/sh\ntouch " + executor.ShellQuote(started) + "\nwhile [ ! -f " + executor.ShellQuote(release) + " ]; do sleep 0.1; done\n"
	os.WriteFile(filepath.Join(repo, ".git/hooks/post-checkout"), []byte(hook), 0700)
	t.Cleanup(func() { os.WriteFile(release, []byte("release"), 0600) })
	plan, err := PlanMultiWorkspace([]RepositorySource{{Name: "repo", Repo: repo}}, 33, InteractiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(plan)
	cmd := exec.Command("python3", "-c", multiWorkerScript, "create", string(raw), multiPreflightScript, interactiveScript)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill() }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("checkout hook did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	ex := executor.NewLocal()
	if err := RunInteractive(context.Background(), ex, "remove", plan); err == nil {
		t.Fatal("cleanup raced an orphaned checkout")
	}
	os.WriteFile(release, []byte("release"), 0600)
	deadline = time.Now().Add(10 * time.Second)
	for {
		err = RunInteractive(context.Background(), ex, "remove", plan)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("orphaned checkout did not become recoverable: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if plan.State != "removed" {
		t.Fatal(plan.State)
	}
}
