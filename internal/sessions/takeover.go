package sessions

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/JeremiahM37/agentdeck/internal/agents"
	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

// ExactResumeID never falls back to the last conversation in a directory.
func (m *Manager) ExactResumeID(agent, id string) string {
	spec, ok := Find(m.specs(), agent)
	if !ok || len(spec.ResumeIDArgs) == 0 {
		return ""
	}
	return id
}

// PrepareTakeover preserves the task's runtime configuration while removing
// broker hooks tied to the background attempt. Nothing is launched here.
func (m *Manager) PrepareTakeover(ctx context.Context, ex executor.Executor, task *store.Task, project *store.Project, att *store.Attempt, env map[string]string) (LaunchOpts, error) {
	spec, ok := Find(m.specs(), task.Agent)
	if !ok {
		return LaunchOpts{}, fmt.Errorf("unknown interactive agent %q", task.Agent)
	}
	// Without an exact conversation identity, keep a written handoff alongside
	// the unchanged files. Never resume an unrelated --last conversation.
	resumeID := att.SessionID
	if len(spec.ResumeIDArgs) == 0 {
		resumeID = ""
	}
	prime := ""
	if resumeID == "" {
		prompt := firstNonEmpty(att.Prompt, task.Prompt)
		handoff := "# Interactive takeover\n\nThe operator took over this existing run. Preserve its work and wait for their next instruction.\n\n## Original request\n" + prompt + "\n\n## Last result\n" + att.ResultJSON + "\n\nThe complete event log is in .agentdeck/events.jsonl.\n"
		path := agents.RuntimeDir(att.WorktreePath) + "/takeover.md"
		if err := ex.WriteFile(ctx, path, []byte(handoff)); err != nil {
			return LaunchOpts{}, err
		}
		prime = "Read " + path + " for the previous run's context. This is an interactive takeover in the same worktree; preserve its changes and wait for my next instruction."
	}
	// Carry authentication, project environment and Claude's explicit tool/MCP
	// configuration into the interactive process. Approval broker hooks belong
	// to the cancelled attempt, so they must not be copied into the new one.
	var extraArgs []string
	if task.Agent == "claude" || task.Agent == "" {
		extraArgs = append(extraArgs, "--permission-mode", firstNonEmpty(task.PermissionMode, "default"))
		raw, err := ex.ReadFile(ctx, agents.RuntimeDir(att.WorktreePath)+"/settings.json", 0)
		if err != nil {
			return LaunchOpts{}, err
		}
		settings := map[string]any{}
		if len(raw) > 0 {
			if err = json.Unmarshal(raw, &settings); err != nil {
				return LaunchOpts{}, fmt.Errorf("read task settings: %w", err)
			}
			delete(settings, "hooks")
			path := agents.RuntimeDir(att.WorktreePath) + "/interactive-settings.json"
			if err = ex.WriteFile(ctx, path, []byte(store.J(settings))); err != nil {
				return LaunchOpts{}, err
			}
			extraArgs = append(extraArgs, "--settings", path)
		}
		raw, err = ex.ReadFile(ctx, agents.RuntimeDir(att.WorktreePath)+"/mcp.json", 0)
		if err != nil {
			return LaunchOpts{}, err
		}
		if len(raw) > 0 {
			extraArgs = append(extraArgs, "--mcp-config", agents.RuntimeDir(att.WorktreePath)+"/mcp.json")
		}
		if project.StrictMCP != 0 {
			extraArgs = append(extraArgs, "--strict-mcp-config")
		}
	}
	return LaunchOpts{ProjectID: &project.ID, TargetID: project.TargetID, Name: task.Title, Agent: task.Agent,
		Model: firstNonEmpty(att.Model, task.Model), Workdir: att.WorktreePath, ResumeID: resumeID, Prime: prime,
		Env: env, ExtraArgs: extraArgs, Yolo: task.Agent != "claude" && task.PermissionMode == "bypassPermissions"}, nil
}
