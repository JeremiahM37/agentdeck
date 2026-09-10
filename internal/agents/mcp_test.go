package agents

import (
	"encoding/json"
	"os/exec"
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
