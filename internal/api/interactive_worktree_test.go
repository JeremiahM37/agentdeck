package api_test

import (
	"fmt"
	"github.com/JeremiahM37/agentdeck/internal/config"
	"github.com/JeremiahM37/agentdeck/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInteractiveSessionWorktreeLifecycle(t *testing.T) {
	requireRealTools(t)
	isolateTmux(t)
	h := newHarness(t, func(c *config.Config) { c.Mock = false })
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	os.Mkdir(repo, 0700)
	git := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v", out, err)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(repo, "proof"), []byte("base\n"), 0600)
	git("add", ".")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "base")
	target, _ := h.App.DB.InsertTarget(&store.Target{Name: "local worktree", Kind: "local"})
	h.decode("PUT", "/api/agents", []obj{{"name": "test-worktree", "command": "sleep 600"}}, 200, nil)
	var row obj
	input := obj{"group_path": "Work/Worktrees", "name": "isolated session", "target_id": target.ID, "workdir": repo, "agent": "test-worktree", "worktree": obj{"branch": "feature/api"}, "yolo": false}
	h.decode("POST", "/api/sessions", input, 201, &row)
	ws := row["workspace"].(map[string]any)
	dir := ws["path"].(string)
	if row["group_path"] != "Work/Worktrees" || row["workdir"] != dir || dir == repo || ws["token"] != nil {
		t.Fatal(row)
	}
	base := fmt.Sprintf("/api/sessions/%d", int64(row.num("id")))
	h.decode("DELETE", base+"/worktree", nil, 409, nil)
	h.decode("DELETE", base, nil, 200, nil)
	os.WriteFile(filepath.Join(dir, "keep"), []byte("uncommitted"), 0600)
	h.decode("DELETE", base+"/worktree", nil, 409, nil)
	os.Remove(filepath.Join(dir, "keep"))
	h.decode("DELETE", base+"/worktree", nil, 200, &row)
	h.decode("DELETE", base+"/worktree", nil, 200, nil)
	if row["workspace"].(map[string]any)["state"] != "removed" {
		t.Fatal(row)
	}
	// The kept branch cannot silently be reused for a new worktree. Failed
	// allocation remains inspectable and can be cleared without deleting it.
	h.decode("POST", "/api/sessions", input, 409, nil)
	var failed []obj
	h.decode("GET", "/api/sessions?all=true", nil, 200, &failed)
	if len(failed) != 2 || failed[0]["workspace"].(map[string]any)["state"] != "failed" {
		t.Fatal(failed)
	}
	h.decode("DELETE", fmt.Sprintf("/api/sessions/%d/worktree", int64(failed[0].num("id"))), nil, 200, nil)
	input["resume"] = true
	h.decode("POST", "/api/sessions", input, 409, nil)
	var all []obj
	h.decode("GET", "/api/sessions?all=true", nil, 200, &all)
	if len(all) != 2 {
		t.Fatalf("allocation lost or invalid request created a session: %v", all)
	}
}
