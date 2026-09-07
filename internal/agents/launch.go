package agents

import (
	"fmt"
	"sort"
	"strings"
)

// Names are the agents agentdeck knows how to launch.
var Names = []string{"claude", "codex", "gemini"}

// GatedCapable are the agents that support the hook-gated 'default' permission
// mode. codex and gemini have no PreToolUse equivalent, so a gated task on them
// is rejected before dispatch rather than silently running ungated.
var GatedCapable = map[string]bool{"claude": true}

// codexSandbox maps agentdeck's permission modes onto codex sandbox policies
// (codex >= 0.140; the older --full-auto was removed upstream).
var codexSandbox = map[string][]string{
	"plan":              {"--sandbox", "read-only"},
	"acceptEdits":       {"--sandbox", "workspace-write"},
	"bypassPermissions": {"--dangerously-bypass-approvals-and-sandbox"},
}

// Launcher builds the shell command that starts an agent in a tmux session.
type Launcher struct {
	ClaudeBin string
	CodexBin  string
	GeminiBin string
}

// LaunchSpec is one attempt's launch parameters.
type LaunchSpec struct {
	Agent          string
	Worktree       string
	TmuxSession    string
	PermissionMode string
	Model          string
	ResumeSession  string
	Sandbox        bool
	Env            map[string]string
	SettingsPath   string
	MCPConfig      string
	StrictMCP      bool
}

// EnvPrefix renders the shell prefix of KEY=VAL pairs injected before the agent
// binary.
//
// This is the any-model door: point a project at any Anthropic-compatible
// endpoint (Ollama >= 0.20 natively, LiteLLM, llama.cpp, vLLM gateways) via
// ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN, or set OPENAI_*/GEMINI_* for the
// other agents. Values are shell-quoted; keys are validated.
func EnvPrefix(env map[string]string, sandbox bool) (string, error) {
	pairs := map[string]string{}
	for k, v := range env {
		pairs[k] = v
	}
	if sandbox {
		// claude refuses bypassPermissions as root; inside a disposable container
		// that refusal is the wrong default
		if _, ok := pairs["IS_SANDBOX"]; !ok {
			pairs["IS_SANDBOX"] = "1"
		}
	}
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if !validEnvName(k) {
			return "", fmt.Errorf("invalid env var name %q", k)
		}
		parts = append(parts, k+"="+shellQuote(pairs[k]))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return strings.Join(parts, " ") + " ", nil
}

func validEnvName(k string) bool {
	if k == "" || (k[0] >= '0' && k[0] <= '9') {
		return false
	}
	for _, r := range k {
		if r != '_' && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// Command renders the full `tmux new-session` invocation for one attempt.
func (l Launcher) Command(s LaunchSpec) (string, error) {
	prefix, err := EnvPrefix(s.Env, s.Sandbox)
	if err != nil {
		return "", err
	}
	if s.Agent == "" || s.Agent == "claude" {
		return l.claudeCommand(s, prefix), nil
	}

	// codex/gemini have no equivalent of --settings/--mcp-config; the staged
	// context bundle still reaches them through the prompt prefix
	rt := RuntimeDir(s.Worktree)
	var parts []string
	switch s.Agent {
	case "codex":
		parts = []string{l.bin(l.CodexBin, "codex"), "exec", "--json"}
		if s.Model != "" {
			parts = append(parts, "-m", s.Model)
		}
		flags, ok := codexSandbox[s.PermissionMode]
		if !ok {
			return "", fmt.Errorf("codex has no sandbox for permission mode %q", s.PermissionMode)
		}
		parts = append(parts, flags...)
		if s.ResumeSession != "" {
			parts = append(parts, "resume", shellQuote(s.ResumeSession))
		}
		parts = append(parts, `"$(cat .agentdeck/prompt.md)"`)
	case "gemini":
		parts = []string{l.bin(l.GeminiBin, "gemini"), "-p", `"$(cat .agentdeck/prompt.md)"`}
		if s.Model != "" {
			parts = append(parts, "-m", s.Model)
		}
		if s.PermissionMode == "acceptEdits" || s.PermissionMode == "bypassPermissions" {
			parts = append(parts, "--yolo")
		}
	default:
		return "", fmt.Errorf("unknown agent %q", s.Agent)
	}
	// Both read stdin even with the prompt passed as an argument, and a tmux
	// pane's stdin never EOFs — without this redirect the agent waits forever on
	// "Reading additional input from stdin" and the attempt merely looks hung.
	inner := fmt.Sprintf("cd %s && %s%s < /dev/null > %s/events.jsonl 2> %s/stderr.log; echo $? > %s/exit_code",
		s.Worktree, prefix, strings.Join(parts, " "), rt, rt, rt)
	return "tmux new-session -d -s " + s.TmuxSession + " " + shellQuote(inner), nil
}

func (l Launcher) claudeCommand(s LaunchSpec, prefix string) string {
	rt := RuntimeDir(s.Worktree)
	settings := s.SettingsPath
	if settings == "" {
		settings = SettingsRel
	}
	parts := []string{l.bin(l.ClaudeBin, "claude"), "-p", `"$(cat .agentdeck/prompt.md)"`,
		"--output-format", "stream-json", "--verbose",
		"--permission-mode", s.PermissionMode}
	parts = append(parts, "--settings", settings)
	if s.MCPConfig != "" {
		parts = append(parts, "--mcp-config", s.MCPConfig)
		if s.StrictMCP {
			parts = append(parts, "--strict-mcp-config")
		}
	}
	if s.Model != "" {
		parts = append(parts, "--model", s.Model)
	}
	if s.ResumeSession != "" {
		parts = append(parts, "--resume", s.ResumeSession)
	}
	inner := fmt.Sprintf("cd %s && %s%s > %s/events.jsonl 2> %s/stderr.log; echo $? > %s/exit_code",
		s.Worktree, prefix, strings.Join(parts, " "), rt, rt, rt)
	return "tmux new-session -d -s " + s.TmuxSession + " " + shellQuote(inner)
}

func (l Launcher) bin(configured, def string) string {
	if configured != "" {
		return configured
	}
	return def
}
