package sessions

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/JeremiahM37/agentdeck/internal/shellq"
)

// Spec describes how to start one interactive coding CLI.
//
// Three agents ship built in, but the set is deliberately open: the terminal
// does not care which binary is in it, and neither should the board. A spec is
// the smallest description that lets agentdeck launch, resume and prime an
// arbitrary CLI — anything beyond that is the CLI's business.
type Spec struct {
	Name string `json:"name"`
	// Command is the binary (or a full shell command) that starts it.
	Command string `json:"command"`
	// Args are fixed arguments appended on every launch.
	Args []string `json:"args,omitempty"`
	// ModelFlag passes a model, e.g. "--model" or "-m". Empty means this CLI has
	// no model switch and any model set on the session is ignored rather than
	// guessed at.
	ModelFlag string `json:"model_flag,omitempty"`
	// ResumeArgs reopen the CLI's own previous conversation, e.g. ["--continue"].
	// Empty means resume is not offered for this agent.
	ResumeArgs []string `json:"resume_args,omitempty"`
	// PromptArg says the opening message can be a positional argument. When it
	// cannot, agentdeck falls back to typing the message once the pane settles.
	PromptArg bool `json:"prompt_arg,omitempty"`
	// Env is agent-wide environment, layered under the project's own. This is
	// the local-model door for a CLI that wants its endpoint in the environment.
	Env map[string]string `json:"env,omitempty"`
	// Builtin marks the three that ship with agentdeck, so the UI can show which
	// are yours.
	Builtin bool `json:"builtin,omitempty"`
}

// Builtins are the agents agentdeck knows without being told.
func Builtins() []Spec {
	return []Spec{
		{Name: "claude", Command: "claude", ModelFlag: "--model",
			ResumeArgs: []string{"--continue"}, PromptArg: true, Builtin: true},
		{Name: "codex", Command: "codex", ModelFlag: "-m",
			ResumeArgs: []string{"resume", "--last"}, PromptArg: true, Builtin: true},
		// gemini's interactive mode takes no opening message on the command
		// line, so its prime is typed in once the pane settles
		{Name: "gemini", Command: "gemini", ModelFlag: "-m", Builtin: true},
	}
}

// ParseSpecs decodes the operator's custom agents and merges them over the
// built-ins. Same name overrides, so a built-in whose CLI has drifted can be
// corrected without a new release.
func ParseSpecs(raw string) []Spec {
	out := Builtins()
	var custom []Spec
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &custom); err != nil {
			return out
		}
	}
	for _, c := range custom {
		c.Name = strings.TrimSpace(c.Name)
		if c.Name == "" || c.Command == "" {
			continue
		}
		c.Builtin = false
		replaced := false
		for i := range out {
			if out[i].Name == c.Name {
				out[i], replaced = c, true
				break
			}
		}
		if !replaced {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ValidateSpecs rejects definitions that could not launch, so a bad one fails at
// config time rather than as a session that dies on start.
func ValidateSpecs(raw string) error {
	var custom []Spec
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), &custom); err != nil {
		return fmt.Errorf("agents must be a list of objects: %w", err)
	}
	seen := map[string]bool{}
	for i, c := range custom {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			return fmt.Errorf("agent %d has no name", i)
		}
		if seen[name] {
			return fmt.Errorf("duplicate agent %q", name)
		}
		seen[name] = true
		if strings.TrimSpace(c.Command) == "" {
			return fmt.Errorf("agent %q has no command", name)
		}
		for k := range c.Env {
			if !validEnvName(k) {
				return fmt.Errorf("agent %q: invalid env var name %q", name, k)
			}
		}
	}
	return nil
}

// Find returns the spec for a name.
func Find(specs []Spec, name string) (Spec, bool) {
	if name == "" {
		name = "claude"
	}
	for _, s := range specs {
		if s.Name == name {
			return s, true
		}
	}
	return Spec{}, false
}

// LaunchCommand renders the tmux invocation that starts this agent.
func (s Spec) LaunchCommand(workdir, tmuxName, model string, resume bool, prompt, envPrefix string) string {
	parts := []string{s.Command}
	parts = append(parts, s.Args...)
	if resume && len(s.ResumeArgs) > 0 {
		parts = append(parts, s.ResumeArgs...)
	}
	if model != "" && s.ModelFlag != "" {
		parts = append(parts, s.ModelFlag, model)
	}
	if prompt != "" && s.PromptArg {
		parts = append(parts, shellq.Quote(prompt))
	}
	inner := fmt.Sprintf("cd %s && %s%s; exec bash",
		shellq.Quote(workdir), envPrefix, strings.Join(parts, " "))
	return fmt.Sprintf("tmux new-session -d -s %s %s",
		shellq.Quote(tmuxName), shellq.Quote(inner))
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
