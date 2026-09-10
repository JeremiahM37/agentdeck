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
