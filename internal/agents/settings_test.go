package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func settingsJSON(t *testing.T, in SettingsInput) map[string]any {
	t.Helper()
	s, err := BuildSettings(in)
	if err != nil {
		t.Fatalf("BuildSettings: %v", err)
	}
	raw, _ := json.Marshal(s)
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func perms(t *testing.T, s map[string]any) map[string]any {
	t.Helper()
	p, ok := s["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("no permissions block in %v", s)
	}
	return p
}

func strList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, i.(string))
	}
	return out
}

func has(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestSettingsCarryPermissionsWithoutHooks(t *testing.T) {
	p, err := ParsePermissions(`{"allow":["Bash(pytest*)"]}`)
	if err != nil {
		t.Fatal(err)
	}
	s := settingsJSON(t, SettingsInput{Permissions: p})
	if got := strList(perms(t, s)["allow"]); len(got) != 1 || got[0] != "Bash(pytest*)" {
		t.Fatalf("allow: %v", got)
	}
	// rules apply to ungated runs too — that is the whole point of writing
	// settings.json unconditionally
	if _, ok := s["hooks"]; ok {
		t.Fatal("an ungated run must not get the approval hook")
	}
}

func TestSettingsMergePermissionsAndGate(t *testing.T) {
	p, _ := ParsePermissions(`{"deny":["Bash(rm *)"]}`)
	s := settingsJSON(t, SettingsInput{BaseURL: "http://cp:9110", Token: "tok",
		Gated: true, Permissions: p})
	if got := strList(perms(t, s)["deny"]); len(got) != 1 || got[0] != "Bash(rm *)" {
		t.Fatalf("deny: %v", got)
	}
	hooks := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	if hooks[0].(map[string]any)["matcher"] != DefaultGateMatcher {
		t.Fatalf("matcher: %v", hooks[0])
	}
}

func TestSettingsRejectTypoPermissionKeys(t *testing.T) {
	_, err := ParsePermissions(`{"allowed":["Bash"]}`)
	if err == nil {
		t.Fatal("a typo'd key must fail at config time, not as a mystery denial mid-run")
	}
	if !strings.Contains(err.Error(), "allowed") {
		t.Fatalf("the error must name the offending key: %v", err)
	}
}

func TestEmptySettingsIsEmpty(t *testing.T) {
	s := settingsJSON(t, SettingsInput{})
	if len(s) != 0 {
		t.Fatalf("expected {}, got %v", s)
	}
}

func TestUnknownProfileRejected(t *testing.T) {
	if _, err := BuildSettings(SettingsInput{Profile: "godmode"}); err == nil {
		t.Fatal("an unknown capability profile must be rejected")
	}
}

func TestParityGrantsBashAndEnumeratedMCPServers(t *testing.T) {
	s := settingsJSON(t, SettingsInput{Profile: "parity",
		MCPServers: []string{"grimoire", "homelab"}})
	allow := strList(perms(t, s)["allow"])
	// bare Bash, not Bash(...): a prefix rule makes the CLI split compound
	// commands and refuse the parts it cannot match
	if !has(allow, "Bash") {
		t.Errorf("parity must grant bare Bash: %v", allow)
	}
	// `mcp__*` is accepted into settings.json and still denies every call, so
	// each server needs its own rule
	if !has(allow, "mcp__grimoire") || !has(allow, "mcp__homelab") {
		t.Errorf("MCP servers must be enumerated: %v", allow)
	}
	if has(allow, "mcp__*") {
		t.Errorf("the wildcard does not work and must not be emitted: %v", allow)
	}
}

func TestRestrictedProfileStaysEmpty(t *testing.T) {
	// the default must not silently widen an existing install's permissions
	s := settingsJSON(t, SettingsInput{Profile: "restricted",
		MCPServers: []string{"grimoire"}})
	if len(s) != 0 {
		t.Fatalf("restricted must grant nothing implicitly: %v", s)
	}
}

func TestExplicitDenyBeatsTheProfile(t *testing.T) {
	p, _ := ParsePermissions(`{"deny":["Bash"]}`)
	s := settingsJSON(t, SettingsInput{Profile: "parity", Permissions: p,
		MCPServers: []string{"grimoire"}})
	pm := perms(t, s)
	if has(strList(pm["allow"]), "Bash") {
		t.Error("the profile re-granted something the operator denied")
	}
	if !has(strList(pm["deny"]), "Bash") {
		t.Error("the explicit deny was dropped")
	}
}

func TestMemoryDirIsReachableNotJustLinked(t *testing.T) {
	// linking a store outside the worktree is useless unless the filesystem
	// sandbox is opened for it too
	for _, profile := range []string{"restricted", "parity"} {
		s := settingsJSON(t, SettingsInput{Profile: profile, MemoryDir: "/store/memory"})
		dirs := strList(perms(t, s)["additionalDirectories"])
		if !has(dirs, "/store/memory") {
			t.Errorf("%s: memory dir missing from additionalDirectories: %v", profile, dirs)
		}
	}
}

func TestProjectSlugMatchesClaudeLayout(t *testing.T) {
	// verified against the running CLI: every non-alphanumeric collapses to '-'
	if got := ProjectSlug("/srv/repos/x/.wt/task1-a1"); got != "-srv-repos-x--wt-task1-a1" {
		t.Errorf("got %q", got)
	}
	if got := ProjectSlug("/tmp/slug_test.d/a_b"); got != "-tmp-slug-test-d-a-b" {
		t.Errorf("got %q", got)
	}
}

// Memory follows the MAIN worktree, not cwd. Verified against the CLI: a session
// started inside a linked worktree reports the PARENT repo's slug as its memory
// dir, so linking the worktree slug produced a symlink nothing ever opened.
func TestMemoryLinkCommandKeysOnTheGitMainWorktree(t *testing.T) {
	cmd := MemoryLinkCommand("/wt/task1-a1", "/store/memory")
	if !strings.Contains(cmd, "--git-common-dir") {
		t.Error("the main worktree must be resolved on the target, not guessed here")
	}
	if strings.Contains(cmd, "-wt-task1-a1/memory") {
		t.Error("the cwd slug is the wrong key")
	}
	if !strings.Contains(cmd, "ln -sfn /store/memory") {
		t.Errorf("link command: %s", cmd)
	}
	if !strings.Contains(cmd, `$HOME/.claude/projects/$slug`) {
		t.Errorf("link path: %s", cmd)
	}
}

func TestMemoryLinkCommandNeverDestroysRealMemories(t *testing.T) {
	// the link path can be a repo the operator also uses interactively
	cmd := MemoryLinkCommand("/wt/task1-a1", "/store/memory")
	if !strings.Contains(cmd, "refusing to replace non-empty memory dir") {
		t.Error("a non-empty real directory must be left alone")
	}
	if strings.Count(cmd, "rm -rf") != 1 {
		t.Errorf("exactly one guarded deletion expected: %s", cmd)
	}
	if !strings.Contains(cmd, `ls -A "$link"`) {
		t.Error("deletion must be reachable only through the emptiness check")
	}
}

func TestMemoryLinkCommandQuotesHostilePaths(t *testing.T) {
	evil := "/store/'; rm -rf /; '"
	cmd := MemoryLinkCommand("/wt/x", evil)
	// the payload survives as ONE literal argument — never as shell syntax
	tokens := shellSplit(cmd)
	found := false
	for _, tok := range tokens {
		if tok == evil {
			found = true
		}
	}
	if !found {
		t.Fatalf("hostile path did not survive as one literal token: %v", tokens)
	}
	rms := 0
	for _, tok := range tokens {
		if tok == "rm" {
			rms++
		}
	}
	if rms != 1 {
		t.Fatalf("expected only the one guarded rm, found %d in %v", rms, tokens)
	}
}

// shellSplit is a minimal POSIX word splitter, enough to prove that a hostile
// path stays one argument.
func shellSplit(s string) []string {
	var out []string
	var cur strings.Builder
	inSingle, started := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inSingle:
			if c == '\'' {
				inSingle = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			inSingle, started = true, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			started = true
		case c == ' ' || c == '\t' || c == '\n':
			if started || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteByte(c)
			started = true
		}
	}
	if started || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
