package agents

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexMCPArgsParseWithInstalledCLI(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex is not installed")
	}
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("HOME", home)
	args, err := CodexMCPArgs(map[string]any{"ops_tools": map[string]any{
		"command": "python3", "args": []any{"-m", "ops"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cmdArgs := append(args, "mcp", "list")
	out, err := exec.Command("codex", cmdArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("codex rejected generated overrides: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ops_tools") || !strings.Contains(string(out), "python3") {
		t.Fatalf("Codex did not report configured server: %s", out)
	}
}

func TestMCPPayloadUsesStandardShape(t *testing.T) {
	raw, err := MCPPayload(map[string]any{"demo": map[string]any{"command": "srv"}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["mcpServers"]; !ok {
		t.Fatalf("payload: %s", raw)
	}
}

func TestCodexMCPArgsAreSortedAndAdditive(t *testing.T) {
	args, err := CodexMCPArgs(map[string]any{
		"z_server": map[string]any{"args": []any{"--x", "a b"}, "command": "srv", "env": map[string]any{"TOKEN": "secret"}},
		"alpha":    map[string]any{"url": "https://example.test/mcp", "headers": map[string]any{"Authorization": "Bearer x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, `mcp_servers.alpha.url="https://example.test/mcp"`) ||
		!strings.Contains(joined, `mcp_servers.z_server.args=["--x","a b"]`) {
		t.Fatalf("args: %v", args)
	}
	if strings.Contains(joined, "CODEX_HOME") {
		t.Fatalf("must not replace Codex home: %v", args)
	}
}

func TestCodexMCPArgsRejectsMalformedServer(t *testing.T) {
	if _, err := CodexMCPArgs(map[string]any{"bad": "not-an-object"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCodexMCPArgsRejectsNamesTheCLICannotParse(t *testing.T) {
	if _, err := CodexMCPArgs(map[string]any{"ops.tools": map[string]any{"command": "python3"}}); err == nil {
		t.Fatal("dotted server names must fail explicitly")
	}
}

func TestInteractiveMCPPrepareRejectsSymlinkAndPublishDoesNotClobber(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	os.Symlink(foreign, filepath.Join(root, ".agentdeck"))
	rel := InteractiveMCPRel(7, "nonce")
	if err := exec.Command("bash", "-c", InteractiveMCPPrepareCommand(root, rel)).Run(); err == nil {
		t.Fatal("symlinked .agentdeck parent must be rejected")
	}
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agentdeck", "interactive"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("bash", "-c", InteractiveMCPPrepareCommand(root, rel)).Run(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.WriteFile(dest, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(filepath.Dir(dest), ".mcp.tmp")
	if err := os.WriteFile(tmp, []byte("agentdeck"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("bash", "-c", InteractiveMCPPublishCommand(root, rel)).Run(); err == nil {
		t.Fatal("existing foreign config must make publication fail")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "foreign" {
		t.Fatalf("foreign config was clobbered: %q", got)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("agentdeck"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("bash", "-c", InteractiveMCPPublishCommand(root, rel)).Run(); err == nil {
		t.Fatal("symlink config must make publication fail")
	}
	got, _ = os.ReadFile(outside)
	if string(got) != "outside" {
		t.Fatalf("symlink target was clobbered: %q", got)
	}
	foreignDir := filepath.Join(root, "foreign-dir")
	if err := os.Mkdir(foreignDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreignDir, dest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("agentdeck"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("bash", "-c", InteractiveMCPPublishCommand(root, rel)).Run(); err == nil {
		t.Fatal("symlink-to-directory config must make publication fail")
	}
	entries, _ := os.ReadDir(foreignDir)
	if len(entries) != 0 {
		t.Fatalf("foreign directory changed: %v", entries)
	}
}
