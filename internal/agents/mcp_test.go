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

func TestInteractiveMCPInstallUsesPrivateExclusiveRuntime(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required on agent targets")
	}
	payload := []byte(`{"mcpServers":{"ops":{"command":"python3"}}}`)
	root := t.TempDir()
	foreign := t.TempDir()
	os.Symlink(foreign, filepath.Join(root, ".agentdeck"))
	rel := InteractiveMCPRel(7, "nonce")
	if err := exec.Command("bash", "-c", InteractiveMCPInstallCommand(root, rel, payload)).Run(); err == nil {
		t.Fatal("symlinked .agentdeck parent must be rejected")
	}
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agentdeck"), 0700); err != nil {
		t.Fatal(err)
	}
	interactive := filepath.Join(root, ".agentdeck", "interactive")
	if err := os.Symlink(foreign, interactive); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("bash", "-c", InteractiveMCPInstallCommand(root, rel, payload)).Run(); err == nil {
		t.Fatal("symlinked interactive parent must be rejected")
	}
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agentdeck", "interactive"), 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(rel, "/mcp.json")))
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	foreignDest := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(foreignDest, []byte("foreign"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("bash", "-c", InteractiveMCPInstallCommand(root, rel, payload)).Run(); err == nil {
		t.Fatal("existing runtime leaf must make installation fail")
	}
	got, _ := os.ReadFile(foreignDest)
	info, _ := os.Stat(foreignDest)
	if string(got) != "foreign" || info.Mode().Perm() != 0640 {
		t.Fatalf("foreign config changed: body=%q mode=%o", got, info.Mode().Perm())
	}
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agentdeck"), 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", "-c", InteractiveMCPInstallCommand(root, rel, payload)).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	dest := filepath.Join(root, filepath.FromSlash(rel))
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("published MCP payload: %q (%v)", got, err)
	}
	info, err = os.Stat(dest)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("published MCP mode: %o (%v)", info.Mode().Perm(), err)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(dest), ".mcp.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary file was not removed: %v", err)
	}
	if info, err := os.Stat(filepath.Dir(dest)); err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("runtime leaf mode: %o (%v)", info.Mode().Perm(), err)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agentdeck"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".agentdeck", "interactive")); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("bash", "-c", InteractiveMCPInstallCommand(root, rel, payload)).Run(); err == nil {
		t.Fatal("symlinked interactive parent must not be traversed")
	}
	got, _ = os.ReadFile(outside)
	if string(got) != "outside" {
		t.Fatalf("symlink target changed: %q", got)
	}
}

func TestInteractiveMCPInstallRejectsParentReplacementBeforeTraversal(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required on agent targets")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agentdeck"), 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	bin := t.TempDir()
	python := filepath.Join(bin, "python3")
	body := "#!/bin/sh\nset -eu\nmv -- \"$ADK_WORK/.agentdeck\" \"$ADK_WORK/original-agentdeck\"\nln -s -- \"$ADK_OUTSIDE\" \"$ADK_WORK/.agentdeck\"\nexec \"$ADK_REAL_PYTHON\" \"$@\"\n"
	if err := os.WriteFile(python, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	realPython, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	rel := InteractiveMCPRel(8, "replacement")
	cmd := exec.Command("bash", "-c", InteractiveMCPInstallCommand(root, rel, []byte(`{"mcpServers":{}}`)))
	cmd.Env = append(os.Environ(),
		"PATH="+bin+":"+os.Getenv("PATH"), "ADK_WORK="+root,
		"ADK_OUTSIDE="+outside, "ADK_REAL_PYTHON="+realPython)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatal("parent replacement must be rejected")
	} else if len(out) == 0 {
		t.Fatal("replacement rejection did not report an error")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("foreign replacement target changed: %v", err)
	}
}
