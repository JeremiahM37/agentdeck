package worktree

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
