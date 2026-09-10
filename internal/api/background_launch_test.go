package api_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/config"
	"github.com/JeremiahM37/agentdeck/internal/shellq"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

func TestBackgroundWorkspaceSurvivesResponseAndRetainsFailures(t *testing.T) {
	requireRealTools(t)
	h := newHarness(t, func(c *config.Config) { c.Mock = false })
	root := t.TempDir()
	repo := filepath.Join(root, "repository")
	os.Mkdir(repo, 0700)
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(repo, "base"), []byte("original"), 0600)
	git("add", ".")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@localhost", "commit", "-qm", "base")
	target, _ := h.App.DB.InsertTarget(&store.Target{Name: "background fixture", Kind: "local"})
	h.decode("PUT", "/api/agents", []obj{{"name": "background-fixture", "command": "sleep 600"}}, 200, nil)
	started, release := filepath.Join(root, "started"), filepath.Join(root, "release")
	hook := filepath.Join(repo, ".git/hooks/post-checkout")
	os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+shellq.Quote(started)+"\nwhile [ ! -f "+shellq.Quote(release)+" ]; do sleep 0.05; done\n"), 0700)
	t.Cleanup(func() { os.WriteFile(release, []byte("release"), 0600) })
	var row obj
	input := obj{"agent": "background-fixture", "target_id": target.ID, "workdir": repo, "worktree": obj{}, "background": true, "yolo": false}
	if os.Getenv("AGENTDECK_GROUPED_SETUP_PROOF") == "1" {
		extra := filepath.Join(root, "extra")
		git("clone", "-q", repo, extra)
		var primaryProject, extraProject obj
		h.decode("POST", "/api/projects", obj{"name": "primary", "target_id": target.ID, "repo_path": repo}, 201, &primaryProject)
		h.decode("POST", "/api/projects", obj{"name": "extra", "target_id": target.ID, "repo_path": extra}, 201, &extraProject)
		delete(input, "workdir")
		input["project_id"] = primaryProject.id()
		input["worktree"] = obj{"extra_repositories": []obj{{"project_id": extraProject.id()}}}
	}
	h.decode("POST", "/api/sessions", input, 202, &row)
	id := row.id()
	endpoint := fmt.Sprintf("/api/sessions/%d", id)
	if row["setup_state"] != "creating" {
		t.Fatal(row)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("hook did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.App.DB.Update("sessions", id, map[string]any{"created_at": store.Now() - 180})
	h.App.Sessions.Poll(context.Background())
	current, _ := h.App.DB.Session(id)
	if current.EndedAt != nil || current.SetupState != "creating" {
		t.Fatal("poll ended active setup")
	}
	// Opt-in wall-clock proof crosses the former Git (90s), child (105s),
	// and outer executor (120s) deadlines without slowing every unit run.
	if os.Getenv("AGENTDECK_SLOW_SETUP_PROOF") == "1" {
		until := time.Now().Add(125 * time.Second)
		for time.Now().Before(until) {
			current, _ = h.App.DB.Session(id)
			if current.SetupState != "creating" || current.EndedAt != nil {
				t.Fatalf("setup abandoned before release: %+v", current)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	h.decode("DELETE", endpoint, nil, 409, nil)
	h.decode("DELETE", endpoint+"/worktree", nil, 409, nil)
	os.WriteFile(release, []byte("release"), 0600)
	wait := func(id int64) *store.Session {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			session, err := h.App.DB.Session(id)
			if err != nil {
				t.Fatal(err)
			}
			if session.SetupState != "creating" {
				return session
			}
			if time.Now().After(deadline) {
				t.Fatal("background launch did not finish")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	current = wait(id)
	if current.SetupState != "ready" || current.EndedAt != nil || current.Workdir == repo {
		t.Fatalf("background launch failed: %+v", current)
	}
	h.decode("DELETE", endpoint, nil, 200, nil)
	os.WriteFile(hook, []byte("#!/bin/sh\nprintf valuable > setup-artifact\necho background-setup-failure >&2\nexit 1\n"), 0700)
	h.decode("POST", "/api/sessions", input, 202, &row)
	failed := wait(row.id())
	if failed.SetupState != "failed" || failed.EndedAt == nil || !strings.Contains(failed.SetupError, "background-setup-failure") {
		t.Fatalf("failure was hidden: %+v", failed)
	}
	if failed.WorktreeJSON == "" {
		t.Fatal("failed allocation was lost")
	}
}
