package worktree

import (
	"context"
	"github.com/JeremiahM37/agentdeck/internal/executor"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInteractiveIsolationOwnershipAndSafeRemoval(t *testing.T) {
	for _, bin := range []string{"git", "python3", "tmux"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip(bin + " unavailable")
		}
	}
	socketRoot, err := os.MkdirTemp("", "adkw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", socketRoot)
	t.Setenv("TMUX", "")
	t.Cleanup(func() { exec.Command("tmux", "kill-server").Run(); os.RemoveAll(socketRoot) })
	root := t.TempDir()
	repo := filepath.Join(root, "source with spaces")
	os.Mkdir(repo, 0700)
	git := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "file"), []byte("original\n"), 0600)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("cache/\n"), 0600)
	git(repo, "add", ".")
	git(repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "base")
	original := git(repo, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(repo, "file"), []byte("uncommitted source\n"), 0600)
	plan := PlanInteractive(repo, 1, InteractiveOptions{Branch: "feature/isolated"})
	ex := executor.NewLocal()
	ctx := context.Background()
	if err := RunInteractive(ctx, ex, "create", plan); err != nil {
		t.Fatal(err)
	}
	if plan.Commit != original {
		t.Fatal("incorrect base")
	}
	body, _ := os.ReadFile(filepath.Join(plan.Path, "file"))
	if string(body) != "original\n" {
		t.Fatal("source edits copied")
	}
	if got := git(repo, "status", "--porcelain"); got != "M file" {
		t.Fatal("source index changed", got)
	}
	collision := PlanInteractive(repo, 2, InteractiveOptions{Branch: plan.Branch})
	if err := RunInteractive(ctx, ex, "create", collision); err == nil {
		t.Fatal("branch collision accepted")
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", "external-proof", "-c", plan.Path, "sleep 600").CombinedOutput(); err != nil {
		t.Fatalf("tmux: %s", out)
	}
	if err := RunInteractive(ctx, ex, "remove", plan); err == nil {
		t.Fatal("worktree with external terminal removed")
	}
	exec.Command("tmux", "kill-session", "-t", "external-proof").Run()
	changed := *plan
	changed.Token = "someone-else"
	if err := RunInteractive(ctx, ex, "remove", &changed); err == nil {
		t.Fatal("foreign ownership accepted")
	}
	os.WriteFile(filepath.Join(plan.Path, "new"), []byte("keep me"), 0600)
	if err := RunInteractive(ctx, ex, "remove", plan); err == nil {
		t.Fatal("untracked file removed")
	}
	os.Remove(filepath.Join(plan.Path, "new"))
	os.Mkdir(filepath.Join(plan.Path, "cache"), 0700)
	os.WriteFile(filepath.Join(plan.Path, "cache", "important"), []byte("ignored but valuable"), 0600)
	if err := RunInteractive(ctx, ex, "remove", plan); err == nil {
		t.Fatal("ignored files removed")
	}
	os.RemoveAll(filepath.Join(plan.Path, "cache"))
	os.WriteFile(filepath.Join(plan.Path, "file"), []byte("branch change\n"), 0600)
	git(plan.Path, "add", ".")
	git(plan.Path, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "work")
	head := git(plan.Path, "rev-parse", "HEAD")
	if err := RunInteractive(ctx, ex, "remove", plan); err != nil {
		t.Fatal(err)
	}
	if plan.State != "removed" {
		t.Fatal(plan.State)
	}
	if _, err := os.Stat(plan.Path); !os.IsNotExist(err) {
		t.Fatal("directory still exists")
	}
	if git(repo, "rev-parse", plan.Branch) != head {
		t.Fatal("committed branch lost")
	}
	body, _ = os.ReadFile(filepath.Join(repo, "file"))
	if string(body) != "uncommitted source\n" {
		t.Fatal("source modified")
	}
}
