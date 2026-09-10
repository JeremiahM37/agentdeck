package api_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/config"
	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

// This exercises Manager.Launch through the real SSH executor. The remote is
// localhost only to make the wrapper and captured filesystem deterministic.
func TestInteractiveMCPManagerLaunchOverSSHForwardsFreshResumeFork(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Mock = false })
	root := t.TempDir()
	log := filepath.Join(root, "argv")
	wrapper := filepath.Join(root, "agent-wrapper.sh")
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+shellQuoteForTest(log)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".agentdeck"), 0700); err != nil {
		t.Fatal(err)
	}
	foreign := []byte(`{"foreign":true}`)
	if err := os.WriteFile(filepath.Join(repo, ".agentdeck", "mcp.json"), foreign, 0600); err != nil {
		t.Fatal(err)
	}
	target, err := h.App.DB.InsertTarget(&store.Target{Name: "localhost ssh", Kind: "ssh", Host: "127.0.0.1", User: "root", Port: 22, KeyPath: "/home/admin/.ssh/id_ed25519"})
	if err != nil {
		t.Fatal(err)
	}
	h.decode("PUT", "/api/agents", []obj{{"name": "codex", "command": wrapper,
		"resume_args": []string{"resume", "--last"}, "resume_id_args": []string{"resume", "{id}"}, "fork_args": []string{"fork", "{id}"}}}, 200, nil)
	p := h.project("ssh-mcp", obj{"target_id": target.ID, "repo_path": repo,
		"mcp": obj{"ops_tools": obj{"command": "python3", "args": []string{"-m", "ops"}}}})
	for _, tc := range []struct {
		name string
		body obj
		want []string
	}{
		{"fresh", obj{}, []string{"-c", `mcp_servers.ops_tools.args=["-m","ops"]`, "-c", `mcp_servers.ops_tools.command="python3"`}},
		{"resume", obj{"resume": true}, []string{"-c", `mcp_servers.ops_tools.args=["-m","ops"]`, "-c", `mcp_servers.ops_tools.command="python3"`, "resume", "--last"}},
	} {
		_ = os.Remove(log)
		body := obj{"agent": "codex", "project_id": p.id(), "name": tc.name}
		for k, v := range tc.body {
			body[k] = v
		}
		row := h.session(body)
		tmux := row.str("tmux_session")
		t.Cleanup(func() {
			_, _ = h.App.Reg.Any().Run(context.Background(), "tmux kill-session -t "+tmux, executor.RunOpts{Timeout: 10})
		})
		var raw []byte
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			raw, err = os.ReadFile(log)
			if err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if err != nil {
			t.Fatalf("%s wrapper log: %v session=%v", tc.name, err, row)
		}
		got := strings.Fields(string(raw))
		if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
			t.Fatalf("%s argv: got %q want %q", tc.name, got, tc.want)
		}
		if gotForeign, readErr := os.ReadFile(filepath.Join(repo, ".agentdeck", "mcp.json")); readErr != nil || string(gotForeign) != string(foreign) {
			t.Fatalf("foreign MCP config changed: %s %v", gotForeign, readErr)
		}
	}
}

func shellQuoteForTest(s string) string {
	return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "'\\''"))
}
