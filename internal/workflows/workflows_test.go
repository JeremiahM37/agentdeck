package workflows

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBundledSourcesHaveWrappersAndPinnedSupport(t *testing.T) {
	for _, definition := range Definitions() {
		files, err := Files(definition.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 || SourceDigest(files) == "" {
			t.Fatalf("workflow %s has no digestable source", definition.ID)
		}
		foundWrapper := false
		for _, file := range files {
			if strings.Join(file.Path, "/") == "SKILL.md" {
				foundWrapper = true
			}
		}
		if !foundWrapper {
			t.Fatalf("workflow %s has no SKILL.md wrapper", definition.ID)
		}
	}
}

// The adapter tests exercise the real pinned scripts in temporary projects.
// Keep this in the Go suite so CI and verify cannot silently skip adapter
// behavior while still avoiding tmux, agents, or a live target.
func TestPythonWorkflowAdapters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", filepath.Join("..", "..", "tests", "test_workflow_adapters.py"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("workflow adapter tests failed: %v\n%s", err, output)
	}
}
