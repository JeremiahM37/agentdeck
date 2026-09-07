package sessions

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every coding CLI asks "do you trust this folder?" the first time it opens one.
// Launching an agent there is the answer, so being asked again — in a terminal
// you then have to go and find — is pure friction. It is worst on scratch
// sessions, where the directory is new every time and the prompt is guaranteed.

func specFor(t *testing.T, name string) Spec {
	t.Helper()
	s, ok := Find(Builtins(), name)
	if !ok {
		t.Fatalf("no builtin %q", name)
	}
	return s
}

// The command has to survive a directory with a space or a quote in it, because
// a scratch directory is named after whatever the session was called.
func TestTrustProbeQuotesTheDirectory(t *testing.T) {
	for _, agent := range []string{"claude", "codex"} {
		probe := specFor(t, agent).TrustProbe(`/tmp/a dir/with'quote`)
		if probe == "" {
			t.Fatalf("%s has no trust command", agent)
		}
		if strings.Contains(probe, "{dir}") {
			t.Errorf("%s: placeholder left unsubstituted: %s", agent, probe)
		}
	}
	// an agent with no such notion gets no command rather than a broken one
	if got := (Spec{Name: "custom", Command: "x"}).TrustProbe("/tmp/x"); got != "" {
		t.Errorf("expected no probe, got %q", got)
	}
	// and no directory means nothing to trust
	if got := specFor(t, "claude").TrustProbe(""); got != "" {
		t.Errorf("expected no probe for an empty dir, got %q", got)
	}
}

// runProbe executes a trust command against a temp HOME, the way the executor
// does on a target.
func runProbe(t *testing.T, probe, home string) {
	t.Helper()
	cmd := exec.Command("bash", "-c", probe)
	cmd.Env = append(os.Environ(), "HOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("probe failed: %v\n%s", err, out)
	}
}

// The claude side writes JSON that Claude Code itself will read, so it has to be
// the real key in the real shape — and it must not destroy the rest of a config
// that holds credentials and every other project's state.
func TestClaudeTrustAddsTheKeyWithoutLosingTheConfig(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	original := map[string]any{
		"oauthAccount": map[string]any{"emailAddress": "someone@example.com"},
		"projects": map[string]any{
			"/existing": map[string]any{"hasTrustDialogAccepted": true, "lastCost": 1.25},
		},
		"numStartups": 42,
	}
	raw, _ := json.Marshal(original)
	os.WriteFile(path, raw, 0o600)

	dir := "/home/admin/agentdeck-scratch/a room-20260907-abc123"
	runProbe(t, specFor(t, "claude").TrustProbe(dir), home)

	var got map[string]any
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &got); err != nil {
		t.Fatalf("the config is no longer valid JSON: %v", err)
	}
	projects := got["projects"].(map[string]any)
	entry, ok := projects[dir].(map[string]any)
	if !ok {
		t.Fatalf("the directory was not recorded: %v", projects)
	}
	if entry["hasTrustDialogAccepted"] != true {
		t.Errorf("wrong key or value: %v", entry)
	}
	// nothing else may be disturbed — this file holds live credentials
	if got["numStartups"] != float64(42) {
		t.Errorf("unrelated state was lost: numStartups=%v", got["numStartups"])
	}
	if got["oauthAccount"] == nil {
		t.Error("the account block was lost")
	}
	existing := projects["/existing"].(map[string]any)
	if existing["lastCost"] != 1.25 || existing["hasTrustDialogAccepted"] != true {
		t.Errorf("another project's state was disturbed: %v", existing)
	}
}

// Running it twice must not append a second entry or rewrite what is there.
func TestClaudeTrustIsIdempotent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude.json")
	os.WriteFile(path, []byte(`{"projects":{}}`), 0o600)
	dir := "/srv/repo"
	probe := specFor(t, "claude").TrustProbe(dir)
	runProbe(t, probe, home)
	first, _ := os.ReadFile(path)
	runProbe(t, probe, home)
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Error("a second run rewrote the config")
	}
}

// A missing or corrupt config must not stop a session launching.
func TestClaudeTrustCopesWithNoConfigAtAll(t *testing.T) {
	home := t.TempDir()
	runProbe(t, specFor(t, "claude").TrustProbe("/srv/repo"), home)
	raw, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		t.Fatalf("it should have created one: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
}

// The codex side appends TOML rather than round-tripping through a parser
// agentdeck does not own — that file holds the operator's MCP servers.
func TestCodexTrustAppendsWithoutRewritingTheConfig(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	path := filepath.Join(home, ".codex", "config.toml")
	existing := "# my settings\n[mcp_servers.grimoire]\ncommand = \"/usr/local/bin/grimoire-mcp\"\n"
	os.WriteFile(path, []byte(existing), 0o600)

	dir := "/home/admin/projects/inference-research"
	probe := specFor(t, "codex").TrustProbe(dir)
	runProbe(t, probe, home)

	after, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(after), existing) {
		t.Errorf("the existing config was not preserved verbatim:\n%s", after)
	}
	if !strings.Contains(string(after), `[projects."`+dir+`"]`) {
		t.Errorf("the project was not trusted:\n%s", after)
	}
	if !strings.Contains(string(after), `trust_level = "trusted"`) {
		t.Errorf("no trust level:\n%s", after)
	}
	// twice must not duplicate the block
	runProbe(t, probe, home)
	twice, _ := os.ReadFile(path)
	if strings.Count(string(twice), `[projects."`+dir+`"]`) != 1 {
		t.Errorf("a second run duplicated the entry:\n%s", twice)
	}
}

func TestCodexTrustCreatesTheConfigWhenThereIsNone(t *testing.T) {
	home := t.TempDir()
	runProbe(t, specFor(t, "codex").TrustProbe("/srv/repo"), home)
	raw, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("it should have created one: %v", err)
	}
	if !strings.Contains(string(raw), "trusted") {
		t.Errorf("got %q", raw)
	}
}
