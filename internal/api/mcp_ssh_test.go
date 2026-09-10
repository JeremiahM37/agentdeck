package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/config"
	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

var lifecycleRigSeq atomic.Int64

// TestInteractiveMCPManagerLaunchLifecycle proves the API and Manager launch
// the same project MCP configuration through both real executors. It exercises
// all native continuation forms for both supported agents, while the wrapper
// process records the argv and environment that actually reached the target.
// The native histories are fixtures: no model or provider authentication is
// involved.
func TestInteractiveMCPManagerLaunchLifecycle(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		for _, targetKind := range []string{"local", "ssh"} {
			t.Run(agent+"-"+targetKind, func(t *testing.T) {
				requireRealTools(t)
				h := newHarness(t, func(c *config.Config) { c.Mock = false })
				root := t.TempDir()
				captureDir := filepath.Join(root, "captures")
				if err := os.Mkdir(captureDir, 0700); err != nil {
					t.Fatal(err)
				}
				wrapper := writeLifecycleWrapper(t, root)
				repo := filepath.Join(root, "repo")
				if err := os.MkdirAll(filepath.Join(repo, ".agentdeck"), 0700); err != nil {
					t.Fatal(err)
				}
				foreign := []byte(`{"foreign":true,"keep":"exact"}`)
				foreignPath := filepath.Join(repo, ".agentdeck", "mcp.json")
				writeMode(t, foreignPath, foreign, 0600)
				instructionPath := filepath.Join(repo, "AGENTS.md")
				if agent == "claude" {
					instructionPath = filepath.Join(repo, "CLAUDE.md")
				}
				instruction := []byte("operator instructions must survive every continuation\n")
				writeMode(t, instructionPath, instruction, 0640)

				home := filepath.Join(root, agent+"-home")
				if err := os.Mkdir(home, 0700); err != nil {
					t.Fatal(err)
				}
				configPath, _ := privateConfigFixture(t, agent, home)
				cid := "11111111-1111-4111-8111-111111111111"
				writeNativeLifecycleHistory(t, agent, home, repo, cid)
				before := snapshotFiles(t, []string{foreignPath, instructionPath, configPath,
					filepath.Join(home, "auth.json"),
					filepath.Join(home, "sessions", cid+".jsonl"),
					filepath.Join(home, "projects", claudeProjectSlug(repo), cid+".jsonl")})

				var remoteTmuxDir string
				if targetKind == "ssh" {
					var err error
					remoteTmuxDir, err = os.MkdirTemp("/tmp", "agentdeck-ssh-tmux-")
					if err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(remoteTmuxDir, 0700); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.RemoveAll(remoteTmuxDir) })
				}
				target := insertLifecycleTarget(t, h, targetKind, remoteTmuxDir)
				base := 10000 + lifecycleRigSeq.Add(1)*100
				if _, err := h.App.DB.Exec(`INSERT INTO sessions
					(id, target_id, name, agent, workdir, tmux_session, status, origin, created_at, updated_at, ended_at)
					VALUES (?, ?, 'lifecycle-id-range', 'none', '/', 'never-launched', 'dead', 'agentdeck', ?, ?, ?)`,
					base, target.ID, store.Now(), store.Now(), store.Now()); err != nil {
					t.Fatal(err)
				}
				targetExecutor, err := h.App.Reg.For(target)
				if err != nil {
					t.Fatal(err)
				}
				if targetKind == "ssh" {
					// The remote executor runs as root while the test process owns
					// the temp tree. Remove only this test's private runtime before
					// t.TempDir attempts its local cleanup.
					t.Cleanup(func() {
						ex, err := h.App.Reg.For(target)
						if err == nil {
							_, _ = ex.Run(context.Background(), "rm -rf -- "+shellQuoteForTest(filepath.Join(repo, ".agentdeck", "interactive"))+" "+shellQuoteForTest(filepath.Join(home, ".local")), executor.RunOpts{Timeout: 20})
						}
					})
				}
				t.Cleanup(func() {
					for id := base + 1; id <= base+10; id++ {
						name := fmt.Sprintf("adk-s%d", id)
						check, checkErr := targetExecutor.Run(context.Background(), "tmux has-session -t "+shellQuoteForTest(name), executor.RunOpts{Timeout: 20})
						if checkErr == nil && check.OK() {
							_, _ = targetExecutor.Run(context.Background(), "tmux kill-session -t "+shellQuoteForTest(name), executor.RunOpts{Timeout: 20})
						}
					}
				})
				envName := "CODEX_HOME"
				if agent == "claude" {
					envName = "CLAUDE_CONFIG_DIR"
				}
				extra := obj{
					"name": agent, "command": wrapper,
					"env":            obj{"CAPTURE_DIR": captureDir, "HOME": home, envName: home},
					"resume_args":    []string{"resume", "--last"},
					"resume_id_args": resumeIDArgs(agent),
					"fork_args":      forkArgs(agent),
				}
				h.decode("PUT", "/api/agents", []obj{extra}, 200, nil)
				project, err := h.App.DB.InsertProject(&store.Project{
					Name: "lifecycle-" + agent + "-" + targetKind, TargetID: target.ID,
					RepoPath: repo, DefaultAgent: agent,
					MCPJSON: store.J(obj{"ops_tools": obj{"command": "python3", "args": []string{"-m", "ops"}}}),
				})
				if err != nil {
					t.Fatal(err)
				}

				fresh := h.session(obj{"project_id": project.ID, "agent": agent, "name": "fresh"})
				waitCapture(t, captureDir, 0)
				freshCapture := readCapture(t, filepath.Join(captureDir, "0.log"))
				assertLifecycleEnvironment(t, freshCapture, agent, home)
				assertFreshArgs(t, agent, freshCapture.args)
				assertProjectFiles(t, agent, repo, home, before, foreign, instruction, targetExecutor)

				conversations := fmt.Sprintf("/api/sessions/%d/conversations", fresh.id())
				var listing obj
				h.decode("GET", conversations, nil, 200, &listing)
				if !strings.Contains(fmt.Sprint(listing), cid) {
					t.Fatalf("native fixture ID missing from API listing: %v", listing)
				}
				fork := h.post(fmt.Sprintf("/api/sessions/%d/fork", fresh.id()),
					obj{"conversation_id": cid, "name": "forked"}, 201)
				waitCapture(t, captureDir, 1)
				forkCapture := readCapture(t, filepath.Join(captureDir, "1.log"))
				assertLifecycleEnvironment(t, forkCapture, agent, home)
				assertContinuationArgs(t, agent, forkCapture.args, cid, true)
				killLifecycleSession(t, h, fork)

				killLifecycleSession(t, h, fresh)
				resumed := h.post(fmt.Sprintf("/api/sessions/%d/resume", fresh.id()),
					obj{"conversation_id": cid, "name": "resumed"}, 201)
				if resumed.str("resume_id") != cid {
					t.Fatalf("resume ID was not persisted: %v", resumed)
				}
				waitCapture(t, captureDir, 2)
				resumeCapture := readCapture(t, filepath.Join(captureDir, "2.log"))
				assertLifecycleEnvironment(t, resumeCapture, agent, home)
				assertContinuationArgs(t, agent, resumeCapture.args, cid, false)
				killLifecycleSession(t, h, resumed)

				assertProjectFiles(t, agent, repo, home, before, foreign, instruction, targetExecutor)
			})
		}
	}
}

func insertLifecycleTarget(t *testing.T, h *harness, kind, tmuxDir string) *store.Target {
	t.Helper()
	if kind == "local" {
		target, err := h.App.DB.InsertTarget(&store.Target{Name: "lifecycle local", Kind: "local"})
		if err != nil {
			t.Fatal(err)
		}
		return target
	}
	prefix := ""
	if tmuxDir != "" {
		prefix = "mkdir -m 700 -p " + shellQuoteForTest(tmuxDir) + " && env TMUX_TMPDIR=" + shellQuoteForTest(tmuxDir) + " sh -c"
	}
	target, err := h.App.DB.InsertTarget(&store.Target{
		Name: "lifecycle ssh", Kind: "ssh", Host: "127.0.0.1", User: "root", Port: 22,
		KeyPath: "/home/admin/.ssh/id_ed25519", CommandPrefix: prefix,
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func writeLifecycleWrapper(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "agent-wrapper.sh")
	body := `#!/bin/sh
set -eu
n=$(find "$CAPTURE_DIR" -maxdepth 1 -name '*.log' -type f | wc -l)
out="$CAPTURE_DIR/$n.log"
{
  printf 'CLAUDE_CONFIG_DIR=%s\n' "${CLAUDE_CONFIG_DIR-}"
  printf 'CODEX_HOME=%s\n' "${CODEX_HOME-}"
  for arg in "$@"; do printf 'ARG=%s\n' "$arg"; done
} > "$out"
exec sleep 600
`
	if err := os.WriteFile(path, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func privateConfigFixture(t *testing.T, agent, home string) (string, []byte) {
	t.Helper()
	name := "auth.json"
	body := []byte(`{"private":"claude-auth","keep":true}`)
	if agent == "codex" {
		name = "config.toml"
		body = []byte("model = \"fixture\"\n")
		writeMode(t, filepath.Join(home, "auth.json"), []byte(`{"token":"fixture"}`), 0600)
	}
	path := filepath.Join(home, name)
	writeMode(t, path, body, 0600)
	return path, body
}

func writeNativeLifecycleHistory(t *testing.T, agent, home, repo, cid string) {
	t.Helper()
	dir := filepath.Join(home, "sessions")
	if agent == "claude" {
		dir = filepath.Join(home, "projects", claudeProjectSlug(repo))
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if agent == "codex" {
		rows = append(rows, map[string]any{"type": "session_meta", "payload": obj{"id": cid, "cwd": repo, "source": "cli"}})
		rows = append(rows, map[string]any{"type": "response_item", "payload": obj{"type": "message", "role": "user", "content": []obj{{"type": "input_text", "text": "lifecycle fixture"}}}})
	} else {
		rows = append(rows, map[string]any{"type": "user", "sessionId": cid, "cwd": repo, "message": obj{"role": "user", "content": "lifecycle fixture"}})
	}
	var body strings.Builder
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(data)
		body.WriteByte('\n')
	}
	writeMode(t, filepath.Join(dir, cid+".jsonl"), []byte(body.String()), 0600)
}

func claudeProjectSlug(workdir string) string {
	var b strings.Builder
	for _, c := range workdir {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func resumeIDArgs(agent string) []string {
	if agent == "claude" {
		return []string{"--resume", "{id}"}
	}
	return []string{"resume", "{id}"}
}

func forkArgs(agent string) []string {
	if agent == "claude" {
		return []string{"--resume", "{id}", "--fork-session"}
	}
	return []string{"fork", "{id}"}
}

type lifecycleCapture struct {
	env  map[string]string
	args []string
}

func waitCapture(t *testing.T, dir string, index int) {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("%d.log", index))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for wrapper capture %s", path)
}

func readCapture(t *testing.T, path string) lifecycleCapture {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := lifecycleCapture{env: map[string]string{}}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "ARG="):
			out.args = append(out.args, strings.TrimPrefix(line, "ARG="))
		case strings.Contains(line, "="):
			parts := strings.SplitN(line, "=", 2)
			out.env[parts[0]] = parts[1]
		}
	}
	return out
}

func assertLifecycleEnvironment(t *testing.T, got lifecycleCapture, agent, home string) {
	t.Helper()
	key := "CODEX_HOME"
	if agent == "claude" {
		key = "CLAUDE_CONFIG_DIR"
	}
	if got.env[key] != home {
		t.Fatalf("%s was not preserved: got %q want %q", key, got.env[key], home)
	}
}

func assertFreshArgs(t *testing.T, agent string, args []string) {
	t.Helper()
	if agent == "claude" {
		if len(args) < 2 || args[0] != "--mcp-config" || !strings.Contains(args[1], "/agentdeck/mcp/") {
			t.Fatalf("fresh Claude MCP argv: %q", args)
		}
		for _, arg := range args {
			if arg == "--resume" || arg == "--fork-session" || arg == "resume" || arg == "fork" {
				t.Fatalf("fresh Claude launch unexpectedly had continuation arg %q: %q", arg, args)
			}
		}
		return
	}
	want := []string{"-c", `mcp_servers.ops_tools.args=["-m","ops"]`, "-c", `mcp_servers.ops_tools.command="python3"`}
	assertArgsContainOrdered(t, args, want)
}

func assertContinuationArgs(t *testing.T, agent string, args []string, cid string, fork bool) {
	t.Helper()
	if agent == "claude" {
		if len(args) < 4 || args[0] != "--mcp-config" || !strings.Contains(args[1], "/agentdeck/mcp/") {
			t.Fatalf("Claude MCP argv: %q", args)
		}
		want := []string{"--resume", cid}
		if fork {
			want = append(want, "--fork-session")
		}
		assertArgsContainOrdered(t, args[2:], want)
		return
	}
	want := []string{"-c", `mcp_servers.ops_tools.args=["-m","ops"]`, "-c", `mcp_servers.ops_tools.command="python3"`}
	if fork {
		want = append(want, "fork", cid)
	} else {
		want = append(want, "resume", cid)
	}
	assertArgsContainOrdered(t, args, want)
}

func assertArgsContainOrdered(t *testing.T, got, want []string) {
	t.Helper()
	start := 0
	for _, item := range want {
		found := -1
		for i := start; i < len(got); i++ {
			if got[i] == item {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("argv %q missing ordered item %q after %d; want %q", got, item, start, want)
		}
		start = found + 1
	}
}

func killLifecycleSession(t *testing.T, h *harness, session obj) {
	t.Helper()
	if session.str("tmux_session") != "" {
		h.decode("DELETE", fmt.Sprintf("/api/sessions/%d", session.id()), nil, 200, nil)
	}
}

type fileSnapshot struct {
	path string
	body []byte
	perm os.FileMode
}

func snapshotFiles(t *testing.T, paths []string) []fileSnapshot {
	t.Helper()
	out := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, fileSnapshot{path: path, body: body, perm: info.Mode().Perm()})
	}
	return out
}

func assertProjectFiles(t *testing.T, agent, repo, home string, before []fileSnapshot, foreign, instruction []byte, ex executor.Executor) {
	t.Helper()
	if got, err := os.ReadFile(filepath.Join(repo, ".agentdeck", "mcp.json")); err != nil || string(got) != string(foreign) {
		t.Fatalf("foreign MCP config changed: %q %v", got, err)
	}
	name := "AGENTS.md"
	if agent == "claude" {
		name = "CLAUDE.md"
	}
	if got, err := os.ReadFile(filepath.Join(repo, name)); err != nil || string(got) != string(instruction) {
		t.Fatalf("%s instructions changed: %q %v", name, got, err)
	}
	for _, want := range before {
		got, err := os.ReadFile(want.path)
		if err != nil {
			t.Fatalf("protected file %s disappeared: %v", want.path, err)
		}
		info, err := os.Stat(want.path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want.body) || info.Mode().Perm() != want.perm {
			t.Fatalf("protected file %s changed (mode %o -> %o)", want.path, want.perm, info.Mode().Perm())
		}
	}
	if agent == "claude" {
		stateRoot := filepath.Join(home, ".local", "state", "agentdeck", "mcp")
		result, err := ex.Run(context.Background(), "find "+shellQuoteForTest(stateRoot)+" -type f -name mcp.json -printf '%m %p\\n'", executor.RunOpts{Timeout: 20})
		if err != nil || !result.OK() || strings.TrimSpace(result.Stdout) == "" {
			t.Fatal("Claude launch did not publish a private MCP runtime")
		}
		for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) != 2 || parts[0] != "600" {
				t.Fatalf("Claude MCP runtime is not private: %q", line)
			}
			var payload map[string]any
			data, readErr := ex.Run(context.Background(), "cat "+shellQuoteForTest(parts[1]), executor.RunOpts{Timeout: 20})
			want := `{"mcpServers":{"ops_tools":{"args":["-m","ops"],"command":"python3"}}}`
			if readErr != nil || !data.OK() || strings.TrimSpace(data.Stdout) != want || json.Unmarshal([]byte(data.Stdout), &payload) != nil || payload["mcpServers"] == nil {
				t.Fatalf("Claude MCP runtime %s is invalid: %v", parts[1], readErr)
			}
			parentResult, parentErr := ex.Run(context.Background(), "stat -c %a "+shellQuoteForTest(filepath.Dir(parts[1])), executor.RunOpts{Timeout: 20})
			if parentErr != nil || !parentResult.OK() || strings.TrimSpace(parentResult.Stdout) != "700" {
				t.Fatalf("Claude MCP runtime parent %s is not private: %q (%v)", filepath.Dir(parts[1]), strings.TrimSpace(parentResult.Stdout), parentErr)
			}
		}
	}
	_ = home
}

func writeMode(t *testing.T, path string, body []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, body, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func shellQuoteForTest(s string) string {
	return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "'\\''"))
}
