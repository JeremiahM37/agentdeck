package api_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeremiahM37/agentdeck/internal/config"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

func TestProjectSkillAPIUsesTargetLocalGitAndPreservesCollision(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Mock = false })
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	source := filepath.Join(root, "sources", "review")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("name: review\ndescription: inspect\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git: %v %s", err, out)
	}
	target, _ := h.App.DB.InsertTarget(&store.Target{Name: "skill-local", Kind: "local"})
	var project obj
	h.decode("POST", "/api/projects", obj{"name": "skill project", "target_id": target.ID, "repo_path": repo, "default_agent": "codex", "skill_sources": []string{filepath.Dir(source)}}, 201, &project)
	var listed obj
	h.decode("GET", "/api/skills?project_id="+itoa(project.id())+"&agent=codex", nil, 200, &listed)
	items := listed["skills"].([]any)
	if len(items) == 0 {
		t.Fatal("target-local skill was not discovered")
	}
	skillID := items[0].(map[string]any)["id"].(string)
	for _, item := range items {
		if candidate := item.(map[string]any)["id"].(string); strings.HasPrefix(candidate, "configured:") {
			skillID = candidate
			break
		}
	}
	var attached obj
	h.decode("POST", "/api/projects/"+itoa(project.id())+"/skills", obj{"agent": "codex", "skill_id": skillID}, 201, &attached)
	link := filepath.Join(repo, ".agents", "skills", "review")
	if _, err := os.Lstat(link); err != nil {
		t.Fatal(err)
	} // source is a directory; attach is idempotent
	h.decode("GET", "/api/projects/"+itoa(project.id())+"/skills?agent=codex", nil, 200, &obj{})
	h.decode("DELETE", "/api/projects/"+itoa(project.id())+"/skills/"+itoa(int64(attached.sub("attachment").num("id"))), nil, 200, nil)
	if err := os.WriteFile(link, []byte("foreign"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := h.status("POST", "/api/projects/"+itoa(project.id())+"/skills", obj{"agent": "codex", "skill_id": skillID}); got != 409 {
		t.Fatalf("collision status=%d", got)
	}
	if b, err := os.ReadFile(link); err != nil || string(b) != "foreign" {
		t.Fatalf("foreign collision changed: %q %v", b, err)
	}
}

func itoa(v int64) string { return fmt.Sprintf("%d", v) }
