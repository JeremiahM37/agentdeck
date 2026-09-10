package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/JeremiahM37/agentdeck/internal/store"
	"github.com/JeremiahM37/agentdeck/internal/worktree"
	"path"
	"strings"
)

func (m *Manager) RemoveWorktree(ctx context.Context, id int64) error {
	m.workspaceMu.Lock()
	defer m.workspaceMu.Unlock()
	row, ex, err := m.resolve(id)
	if err != nil {
		return err
	}
	var plan worktree.Interactive
	if row.WorktreeJSON == "" || json.Unmarshal([]byte(row.WorktreeJSON), &plan) != nil {
		return fmt.Errorf("this session does not own a worktree")
	}
	if plan.State == "removed" {
		return nil
	}
	live, err := m.DB.LiveSessions()
	if err != nil {
		return err
	}
	for _, other := range live {
		if other.TargetID == row.TargetID && (path.Clean(other.Workdir) == path.Clean(plan.Path) || strings.HasPrefix(path.Clean(other.Workdir), path.Clean(plan.Path)+"/")) {
			return fmt.Errorf("end session %d before removing its worktree", other.ID)
		}
	}
	if err := worktree.RunInteractive(ctx, ex, "remove", &plan); err != nil {
		if saveErr := m.DB.Update("sessions", id, map[string]any{"worktree_json": store.J(plan)}); saveErr != nil {
			return fmt.Errorf("%w; could not save cleanup progress: %v", err, saveErr)
		}
		if fresh, loadErr := m.DB.Session(id); loadErr == nil {
			m.publish(fresh)
		}
		return err
	}
	if err := m.DB.Update("sessions", id, map[string]any{"worktree_json": store.J(plan)}); err != nil {
		return err
	}
	if fresh, err := m.DB.Session(id); err == nil {
		m.publish(fresh)
	}
	return nil
}

// Resolve all selections before inserting a session or touching a target. Extra
// repositories inherit the target, never its environment or agent configuration.
func (m *Manager) workspaceSources(o LaunchOpts) ([]worktree.RepositorySource, error) {
	if o.Worktree == nil || len(o.Worktree.ExtraRepositories) == 0 {
		return nil, nil
	}
	if o.ProjectID == nil || o.Workdir != "" {
		return nil, fmt.Errorf("a multi-repository workspace needs a primary project and no working-directory override")
	}
	if len(o.Worktree.ExtraRepositories) > 7 {
		return nil, fmt.Errorf("a workspace supports at most eight repositories")
	}
	primary, err := m.DB.Project(*o.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("primary project is unavailable: %w", err)
	}
	if primary.TargetID != o.TargetID {
		return nil, fmt.Errorf("all workspace projects must use the session target")
	}
	seen := map[int64]bool{primary.ID: true}
	primaryID := primary.ID
	sources := []worktree.RepositorySource{{Name: primary.Name, Repo: primary.RepoPath, Base: o.Worktree.Base, ProjectID: &primaryID}}
	for _, selected := range o.Worktree.ExtraRepositories {
		if seen[selected.ProjectID] {
			return nil, fmt.Errorf("a workspace project was selected more than once")
		}
		project, err := m.DB.Project(selected.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("workspace project %d is unavailable", selected.ProjectID)
		}
		if project.TargetID != o.TargetID {
			return nil, fmt.Errorf("all workspace projects must use the session target")
		}
		id := project.ID
		sources = append(sources, worktree.RepositorySource{Name: project.Name, Repo: project.RepoPath, Base: selected.Base, ProjectID: &id})
		seen[id] = true
	}
	return sources, nil
}
