package sessions

import (
	"os/exec"
	"testing"
)

func TestClaudeForkSnapshotAssetsAndFailureCleanup(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	output, err := exec.Command(python, "claude_fork_seed_test.py").CombinedOutput()
	if err != nil {
		t.Fatalf("Claude fork snapshot: %v\n%s", err, output)
	}
}
