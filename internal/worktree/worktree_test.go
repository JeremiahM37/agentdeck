package worktree

import (
	"strings"
	"testing"

	"github.com/JeremiahM37/agentdeck/internal/executor"
)

func TestNaming(t *testing.T) {
	if got := BranchName(7, 2); got != "adk/task7-a2" {
		t.Errorf("branch: %q", got)
	}
	if got := Path("/data/.wt/", 7, 2); got != "/data/.wt/task7-a2" {
		t.Errorf("path: %q", got)
	}
	if got := DefaultWorkroot("/opt/docker/librarr-go/"); got != "/opt/docker/.agentdeck-worktrees" {
		t.Errorf("workroot with trailing slash: %q", got)
	}
	if got := DefaultWorkroot("/srv/app"); got != "/srv/.agentdeck-worktrees" {
		t.Errorf("workroot: %q", got)
	}
}

func TestSplitPatchSingleFile(t *testing.T) {
	files := SplitPatch(executor.MockDiff)
	if len(files) != 1 || files[0].Path != "app.py" {
		t.Fatalf("files: %+v", files)
	}
	if !strings.Contains(files[0].Patch, `+    print("hello, agentdeck")`) {
		t.Errorf("patch body: %q", files[0].Patch)
	}
}

func TestSplitPatchMultiFile(t *testing.T) {
	patch := "diff --git a/a.py b/a.py\n--- a/a.py\n+++ b/a.py\n@@ -1 +1 @@\n-x\n+y\n" +
		"diff --git a/dir/b.txt b/dir/b.txt\n--- a/dir/b.txt\n+++ b/dir/b.txt\n@@ -0,0 +1 @@\n+new\n"
	files := SplitPatch(patch)
	if len(files) != 2 || files[0].Path != "a.py" || files[1].Path != "dir/b.txt" {
		t.Fatalf("files: %+v", files)
	}
	if !strings.HasSuffix(files[1].Patch, "+new\n") {
		t.Errorf("second file body: %q", files[1].Patch)
	}
}

func TestSplitPatchEmpty(t *testing.T) {
	if got := SplitPatch(""); len(got) != 0 {
		t.Fatalf("expected no files, got %+v", got)
	}
}
