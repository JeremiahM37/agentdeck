package api_test

// Importing is what turns an empty board into a view of the work you already
// have. These run against a REAL local executor: the scan is a shell script on
// the target, so a mock would only prove the script string was built.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// projectTree builds a directory that looks like somewhere code lives.
func projectTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// a real git repo
	repo := filepath.Join(root, "with-git")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", "-b", "trunk", repo).CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %s", out)
	}
	writeFile(t, filepath.Join(repo, "go.mod"), "module x\n")

	// a build manifest, no git
	writeFile(t, filepath.Join(root, "just-go", "go.mod"), "module y\n")
	// the shape a long-running research project actually has: no manifest at
	// all, just the document that says where it got to
	writeFile(t, filepath.Join(root, "research", "HANDOFF.md"), "where we got to\n")
	// and something that is not a project
	if err := os.MkdirAll(filepath.Join(root, "screenshots"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "screenshots", "a.png"), "notacode")
	return root
}

func (h *harness) localTarget(t *testing.T) int64 {
	t.Helper()
	return h.post("/api/targets", obj{"name": "importer", "kind": "local"}, 201).id()
}

func TestScanFindsProjectShapedDirectories(t *testing.T) {
	h := newHarness(t, realLocal)
	tid := h.localTarget(t)
	root := projectTree(t)

	found := h.getList("/api/projects/import/scan?root=" + root +
		"&target_id=" + strconv.FormatInt(tid, 10))
	byName := map[string]obj{}
	for _, c := range found {
		byName[c.str("name")] = c
	}
	if len(byName) != 3 {
		t.Fatalf("expected three candidates, got %v", keysOf(byName))
	}
	if byName["with-git"]["git"] != true || byName["with-git"].str("branch") != "trunk" {
		t.Errorf("git repo: %v", byName["with-git"])
	}
	// the heuristic must reach the research directory that never got a go.mod —
	// those are exactly the long-running projects worth tracking
	if byName["research"].str("marker") != "HANDOFF.md" {
		t.Errorf("research dir: %v", byName["research"])
	}
	if _, ok := byName["screenshots"]; ok {
		t.Error("a directory of assets is not a project")
	}
	// a verify command is proposed from the build system, never assumed
	if byName["just-go"].str("verify_cmd") != "go test ./..." {
		t.Errorf("suggestion: %v", byName["just-go"])
	}
}

func TestImportRegistersProjectsAndSkipsDuplicates(t *testing.T) {
	h := newHarness(t, realLocal)
	tid := h.localTarget(t)
	root := projectTree(t)

	res := h.post("/api/projects/import", obj{"target_id": tid, "root": root}, 200)
	if len(res.list("imported")) != 3 {
		t.Fatalf("imported: %v", res["imported"])
	}
	var repo obj
	for _, p := range res.list("imported") {
		if p.str("name") == "with-git" {
			repo = p
		}
	}
	// the repo's real branch becomes the base a dispatched task worktrees from
	if repo.str("default_base_branch") != "trunk" {
		t.Errorf("base branch: %v", repo.str("default_base_branch"))
	}
	if repo.str("verify_cmd") != "" {
		t.Error("a verify command must be opt-in — a wrong one badges every attempt red")
	}

	// importing the same root again is a no-op, not a pile of duplicates
	again := h.post("/api/projects/import", obj{"target_id": tid, "root": root}, 200)
	if len(again.list("imported")) != 0 || len(again.list("skipped")) != 3 {
		t.Fatalf("re-import should skip everything: %v", again)
	}
	// the demo board is a mock-mode fixture, so this real-executor harness has
	// exactly what was imported and nothing else
	if got := len(h.getList("/api/projects")); got != 3 {
		t.Errorf("expected only the three imported projects, got %d", got)
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	h := newHarness(t, realLocal)
	tid := h.localTarget(t)
	before := len(h.getList("/api/projects"))
	res := h.post("/api/projects/import",
		obj{"target_id": tid, "root": projectTree(t), "dry_run": true}, 200)
	if len(res.list("planned")) != 3 {
		t.Fatalf("planned: %v", res["planned"])
	}
	if len(res.list("imported")) != 0 {
		t.Error("a dry run must not register anything")
	}
	if got := len(h.getList("/api/projects")); got != before {
		t.Errorf("projects changed during a dry run: %d → %d", before, got)
	}
}

func TestImportVerifyIsOptIn(t *testing.T) {
	h := newHarness(t, realLocal)
	tid := h.localTarget(t)
	res := h.post("/api/projects/import",
		obj{"target_id": tid, "root": projectTree(t), "verify": true}, 200)
	found := false
	for _, p := range res.list("imported") {
		if p.str("name") == "just-go" && p.str("verify_cmd") == "go test ./..." {
			found = true
		}
	}
	if !found {
		t.Fatalf("verify was requested but not applied: %v", res["imported"])
	}
}

// An explicit path is registered even when the heuristic would not have found
// it: the operator saying "this is a project" is better evidence than a marker
// file.
func TestImportExplicitPathsBypassTheHeuristic(t *testing.T) {
	h := newHarness(t, realLocal)
	tid := h.localTarget(t)
	odd := filepath.Join(t.TempDir(), "no-markers-at-all")
	if err := os.MkdirAll(odd, 0o755); err != nil {
		t.Fatal(err)
	}
	res := h.post("/api/projects/import",
		obj{"target_id": tid, "paths": []string{odd}}, 200)
	imported := res.list("imported")
	if len(imported) != 1 || imported[0].str("repo_path") != odd {
		t.Fatalf("imported: %v", imported)
	}
	if imported[0].str("name") != "no-markers-at-all" {
		t.Errorf("name should come from the directory: %v", imported[0])
	}
}

func TestImportValidation(t *testing.T) {
	h := newHarness(t, realLocal)
	if code := h.status("POST", "/api/projects/import", obj{}); code != 400 {
		t.Errorf("nothing to import: %d", code)
	}
	if code := h.status("GET", "/api/projects/import/scan", nil); code != 400 {
		t.Errorf("scan with no root: %d", code)
	}
	if code := h.status("POST", "/api/projects/import",
		obj{"target_id": 9999, "root": "/tmp"}); code != 400 {
		t.Errorf("unknown target: %d", code)
	}
}

func keysOf(m map[string]obj) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
