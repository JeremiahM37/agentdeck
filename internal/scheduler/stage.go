package scheduler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/JeremiahM37/agentdeck/internal/agents"
	"github.com/JeremiahM37/agentdeck/internal/ctxbundle"
	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/hooks"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

// launchKW is what stageRuntime tells the launcher about the files it wrote.
type launchKW struct {
	SettingsPath string
	MCPConfig    string
	StrictMCP    bool
	ExtraArgs    []string
}

// EffectiveMCPServers lists the server names this attempt can actually reach, so
// the parity profile grants exactly those.
//
// A local agent runs as the control plane user and inherits that user's MCP
// servers; a remote one only ever has what the project ships in `mcp`. Granting
// a name the agent does not have is harmless, but claiming it would be a lie —
// so remote targets get the project's own servers and nothing more.
func EffectiveMCPServers(hostConfigPath string, project *store.Project, target *store.Target,
	mcp map[string]any) []string {
	names := []string{}
	if inner, ok := mcp["mcpServers"].(map[string]any); ok {
		for k := range inner {
			names = append(names, k)
		}
	} else {
		for k := range mcp {
			names = append(names, k)
		}
	}
	if HostLocalKinds[target.Kind] && project.StrictMCP == 0 {
		names = append(names, agents.HostMCPServers(hostConfigPath)...)
	}
	return names
}

// stageRuntime writes everything the agent reads at startup and returns the
// launch flags that point at it.
//
// Shared by the worktree and sandbox paths. They drifted apart once — sandbox
// runs silently skipped project memory — and a context or permission feature
// landing on only one of them shows up as nothing but worse output.
func (s *Scheduler) stageRuntime(ctx context.Context, ex executor.Executor, workdir string,
	att *store.Attempt, c *runCtx) (launchKW, error) {
	var kw launchKW
	rt := agents.RuntimeDir(workdir)
	isReviewer := c.Task.CreatedBy == "reviewer-gate"

	prompt := firstNonEmpty(att.Prompt, c.Task.Prompt, c.Task.Title)
	if !isReviewer {
		notes, err := s.DB.ProjectNotes(c.Project.ID, 12)
		if err == nil && len(notes) > 0 {
			texts := make([]string, 0, len(notes))
			for _, n := range notes {
				texts = append(texts, n.Note)
			}
			prompt = BuildNotesPrefix(texts) + prompt
		}
		prompt += AgentTaskFooter
	}

	// context bundle: target-wide files first (host conventions), then the
	// project's own. Staged from the CONTROL PLANE's filesystem, so a remote
	// target gets exactly what a local one does.
	patterns := append(store.UnjStrings(c.Target.ContextJSON),
		store.UnjStrings(c.Project.ContextJSON)...)
	files, skipped := ctxbundle.Collect(patterns)
	for _, f := range files {
		if err := ex.WriteFile(ctx, fmt.Sprintf("%s/%s/%s", rt, ctxbundle.Subdir, f.Name),
			f.Data); err != nil {
			return kw, err
		}
	}
	if len(files) > 0 {
		if err := ex.WriteFile(ctx, fmt.Sprintf("%s/%s/%s", rt, ctxbundle.Subdir, ctxbundle.IndexName),
			[]byte(ctxbundle.IndexMarkdown(files, skipped))); err != nil {
			return kw, err
		}
	}
	if len(skipped) > 0 {
		s.Log.Warn("context not staged", "attempt", att.ID, "notes", skipped)
	}
	prompt = ctxbundle.PromptPrefix(files, skipped) + prompt

	if err := ex.WriteFile(ctx, rt+"/prompt.md", []byte(prompt)); err != nil {
		return kw, err
	}
	// task-filing kit: lets the agent put follow-up cards on the board
	if err := ex.WriteFile(ctx, rt+"/adk.py", hooks.ADK); err != nil {
		return kw, err
	}
	if err := ex.WriteFile(ctx, rt+"/env", []byte(fmt.Sprintf(
		"ADK_URL=%s\nADK_TOKEN=%s\n", s.Cfg.BaseURL, att.Token))); err != nil {
		return kw, err
	}

	// per-project MCP: the host user's config is absent on ssh/pct/sandbox
	// targets, so without this a remote agent has strictly fewer tools than a
	// local one
	mcp := store.UnjObj(c.Project.MCPJSON)
	agent := firstNonEmpty(c.Task.Agent, "claude")
	if agent == "codex" && c.Project.StrictMCP != 0 {
		return kw, fmt.Errorf("strict_mcp is unsupported for Codex additive configuration")
	}
	if agent == "claude" && (len(mcp) > 0 || c.Project.StrictMCP != 0) {
		payload := mcp
		if _, ok := mcp["mcpServers"]; !ok {
			payload = map[string]any{"mcpServers": mcp}
		}
		raw, _ := json.Marshal(payload)
		if err := ex.WriteFile(ctx, rt+"/mcp.json", raw); err != nil {
			return kw, err
		}
		kw.MCPConfig = agents.MCPRel
		kw.StrictMCP = c.Project.StrictMCP != 0
	} else if agent == "codex" && len(mcp) > 0 {
		var err error
		kw.ExtraArgs, err = agents.CodexMCPArgs(mcp)
		if err != nil {
			return kw, err
		}
	}

	// settings.json is written for EVERY permission mode, not just the gated one:
	// headless has no prompt, so a tool the rules don't grant is denied outright
	// and the operator never learns why. Rules are how acceptEdits gets Bash.
	gated := c.Task.PermissionMode == "default"
	if gated {
		if err := ex.WriteFile(ctx, rt+"/hook.py", hooks.Hook); err != nil {
			return kw, err
		}
	}
	memoryDir := c.Target.MemoryDir
	agent = firstNonEmpty(c.Task.Agent, "claude")
	if agent != "claude" {
		memoryDir = "" // the memory layout is Claude Code's; nobody else reads it
	}
	perms, err := agents.ParsePermissions(c.Project.PermissionsJSON)
	if err != nil {
		return kw, err
	}
	settings, err := agents.BuildSettings(agents.SettingsInput{
		BaseURL:       s.Cfg.BaseURL,
		Token:         att.Token,
		Gated:         gated,
		Permissions:   perms,
		Matcher:       firstNonEmpty(c.Project.GateMatcher, agents.DefaultGateMatcher),
		Profile:       firstNonEmpty(c.Project.CapabilityProfile, "restricted"),
		MCPServers:    EffectiveMCPServers(s.Cfg.HostClaudeConfig, c.Project, c.Target, mcp),
		MemoryDir:     memoryDir,
		ExpireSeconds: int(s.Cfg.ApprovalExpire.Seconds()),
	})
	if err != nil {
		return kw, err
	}
	raw, _ := json.Marshal(settings)
	if err := ex.WriteFile(ctx, rt+"/settings.json", raw); err != nil {
		return kw, err
	}

	// reused worktrees (follow-ups, reviewer gate) carry the PREVIOUS attempt's
	// runtime files — a stale exit_code finalises this attempt instantly with the
	// previous run's output, which mock-only tests happily hide
	if _, err := ex.Run(ctx, fmt.Sprintf("rm -f %s/exit_code %s/events.jsonl %s/stderr.log",
		rt, rt, rt), executor.RunOpts{Timeout: 20}); err != nil {
		return kw, err
	}

	if memoryDir != "" {
		r, err := ex.Run(ctx, agents.MemoryLinkCommand(workdir, memoryDir),
			executor.RunOpts{Timeout: 30})
		// a broken link degrades the agent's knowledge; it is never worth failing
		// the run over
		if err != nil {
			s.Log.Warn("memory link failed", "attempt", att.ID, "err", err)
		} else if !r.OK() {
			s.Log.Warn("memory link refused", "attempt", att.ID,
				"detail", firstNonEmpty(r.Stderr, r.Stdout))
		}
	}

	kw.SettingsPath = agents.SettingsRel
	return kw, nil
}

func randomToken() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
