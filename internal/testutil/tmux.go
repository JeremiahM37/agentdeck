// Package testutil contains small safety helpers shared by real-process tests.
package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// CleanupTmux removes sessions from one explicitly validated temporary test
// socket. It never invokes tmux kill-server and refuses non-temporary paths.
func CleanupTmux(t *testing.T, tmuxTmpDir string) {
	t.Helper()
	root := filepath.Clean(tmuxTmpDir)
	tmp := filepath.Clean(os.TempDir())
	if root == tmp || !strings.HasPrefix(root, tmp+string(filepath.Separator)) {
		t.Fatalf("refusing non-temporary tmux cleanup path %q", tmuxTmpDir)
	}
	socket := filepath.Join(root, fmt.Sprintf("tmux-%d", os.Getuid()), "default")
	CleanupTmuxSocket(t, socket)
}

// CleanupTmuxSocket is the explicit-socket form for fixtures that pass -S.
func CleanupTmuxSocket(t *testing.T, socket string) {
	t.Helper()
	clean := filepath.Clean(socket)
	tmp := filepath.Clean(os.TempDir())
	if !strings.HasPrefix(clean, tmp+string(filepath.Separator)) {
		t.Fatalf("refusing non-temporary tmux socket %q", socket)
	}
	if out, err := exec.Command("tmux", "-S", clean, "list-sessions", "-F", "#{session_name}").Output(); err == nil {
		for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if name == "" || strings.ContainsAny(name, "\r\n") {
				continue
			}
			if err := exec.Command("tmux", "-S", clean, "kill-session", "-t", "="+name).Run(); err != nil {
				t.Fatalf("cleanup tmux session %q on owned socket: %v", name, err)
			}
		}
	}
}

// RequireIsolated prevents real-process tests from being run against the
// operator's tmux server. The reviewed test runner sets this only inside its
// bwrap namespace.
func RequireIsolated(t *testing.T) {
	t.Helper()
	if os.Getenv("ADK_TEST_ISOLATED") != "1" || os.Getenv("TMUX") != "" || os.Getenv("TMUX_TMPDIR") != "/tmp/adk-test-tmux" {
		t.Fatal("real-process test requires the reviewed isolated runner")
	}
}
