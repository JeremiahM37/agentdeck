package executor

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMockScratchPathDoesNotSwallowSessionLaunch(t *testing.T) {
	m := NewMock(time.Millisecond)
	defer m.Close()
	ctx := context.Background()
	created, err := m.Run(ctx, `root="$HOME/agentdeck-scratch"; d=$(mktemp -d "$root/demo-XXXXXX") && cd "$d" && pwd`, RunOpts{})
	if err != nil || !created.OK() {
		t.Fatalf("scratch: %v %+v", err, created)
	}
	path := strings.TrimSpace(created.Stdout)
	if !strings.Contains(path, "agentdeck-scratch") {
		t.Fatal(path)
	}
	launched, err := m.Run(ctx, "tmux new-session -d -s adk-s7 'cd "+path+" && codex'", RunOpts{})
	if err != nil || !launched.OK() {
		t.Fatalf("launch: %v %+v", err, launched)
	}
	pane, exists := m.capture("adk-s7")
	if !exists || !strings.Contains(pane, path) {
		t.Fatalf("session never started: %q", pane)
	}
}
