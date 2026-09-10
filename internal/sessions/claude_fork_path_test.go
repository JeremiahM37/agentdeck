package sessions

import (
	"os/exec"
	"testing"
)

func TestClaudeForkPathResolution(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	if output, err := exec.Command(python, "claude_fork_path_test.py").CombinedOutput(); err != nil {
		t.Fatalf("Claude native fork path: %v\n%s", err, output)
	}
}

func TestClaudeFileForkRequiresNativeResumeAndFork(t *testing.T) {
	for _, args := range [][]string{{"--resume", "{id}", "--fork-session"}, {"--fork-session", "-r", "{id}"}} {
		if !claudeFileFork(args) {
			t.Fatalf("native fork not recognized: %v", args)
		}
	}
	for _, args := range [][]string{nil, {"fork", "{id}"}, {"--resume", "{id}"}, {"--resume", "fixed", "--fork-session", "{id}"}} {
		if claudeFileFork(args) {
			t.Fatalf("custom command rewritten: %v", args)
		}
	}
}
