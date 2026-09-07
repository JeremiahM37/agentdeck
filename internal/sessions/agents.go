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
	// ModelsCommand asks the CLI what models it has. `{bin}` is replaced with the
	// resolved binary. Model line-ups change faster than agentdeck ships, and a
	// list of names written down here is wrong the moment a vendor renames one —
	// so the tool is asked rather than remembered. Empty means no catalog, and
	// the UI falls back to whatever you have already run.
	ModelsCommand string `json:"models_command,omitempty"`
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
			ResumeArgs: []string{"resume", "--last"}, PromptArg: true, Builtin: true,
			ModelsCommand: "{bin} debug models"},
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

// ModelsProbe is the command that asks this agent for its model catalog, with
// the binary resolved. Empty when the agent cannot be asked.
func (s Spec) ModelsProbe() string {
	if s.ModelsCommand == "" {
		return ""
	}
	return strings.ReplaceAll(s.ModelsCommand, "{bin}", s.Command)
}

// ParseModelCatalog pulls model identifiers out of whatever JSON an agent's
// catalog command prints.
//
// The shape is the agent's, not ours, so this reads defensively: a top-level
// array, or the first array of objects found under a key, and from each entry a
// slug/id/name. Entries a CLI marks hidden are skipped — those are internal
// models its own picker does not offer either.
func ParseModelCatalog(raw string) []string {
	var doc any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &doc); err != nil {
		return nil
	}
	list := findList(doc, 0)
	out := []string{}
	seen := map[string]bool{}
	for _, item := range list {
		var name string
		switch v := item.(type) {
		case string:
			name = v
		case map[string]any:
			if vis, ok := v["visibility"].(string); ok && vis == "hide" {
				continue
			}
			for _, key := range []string{"slug", "id", "name"} {
				if got, ok := v[key].(string); ok && got != "" {
					name = got
					break
				}
			}
		}
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// findList returns the first plausible list of models in a decoded document.
func findList(doc any, depth int) []any {
	if depth > 3 {
		return nil
	}
	switch v := doc.(type) {
	case []any:
		return v
	case map[string]any:
		// a key actually called "models" beats any other array in the document
		if inner, ok := v["models"].([]any); ok {
			return inner
		}
		for _, val := range v {
			if got := findList(val, depth+1); got != nil {
				return got
			}
		}
	}
	return nil
}
