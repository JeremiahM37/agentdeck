package sessions

import (
	"context"
	"fmt"

	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

// ResumeConversation starts an exact native conversation after its old terminal
// has stopped. History validation belongs to the API's target-side reader.
func (m *Manager) ResumeConversation(ctx context.Context, sourceID int64, cid, name string) (*store.Session, error) {
	source, ex, err := m.resolve(sourceID)
	if err != nil {
		return nil, err
	}
	if source.EndedAt == nil {
		return nil, fmt.Errorf("stop the original session before resuming; use Fork to branch a running conversation")
	}
	if m.ExactResumeID(source.Agent, cid) == "" {
		return nil, fmt.Errorf("this agent cannot resume that exact conversation")
	}
	key := fmt.Sprintf("%d/%s/%s", source.TargetID, source.Agent, cid)
	m.mu.Lock()
	if m.resuming == nil {
		m.resuming = map[string]bool{}
	}
	if m.resuming[key] {
		m.mu.Unlock()
		return nil, fmt.Errorf("this conversation is already being resumed")
	}
	m.resuming[key] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.resuming, key); m.mu.Unlock() }()

	// Include released records: stopping tracking does not stop their processes.
	rows, err := m.DB.Sessions(true)
	if err != nil {
		return nil, err
	}
	names := []string{source.TmuxSession}
	for _, row := range rows {
		if row.TargetID == source.TargetID && row.Agent == source.Agent && row.ResumeID == cid {
			if row.TmuxSession == "" {
				return nil, fmt.Errorf("session %d has an unfinished launch; inspect it before retrying", row.ID)
			}
			names = append(names, row.TmuxSession)
		}
	}
	result, err := ex.Run(ctx, PollCommand(names), executor.RunOpts{Timeout: 10})
	if err != nil || !result.OK() {
		return nil, fmt.Errorf("could not establish that the previous terminal has stopped")
	}
	panes, complete := ParsePollSnapshot(result.Stdout, names)
	if !complete {
		return nil, fmt.Errorf("incomplete terminal check; retry when the target is reachable")
	}
	for _, pane := range panes {
		if !pane.Missing {
			return nil, fmt.Errorf("a previous terminal is still running or could not be checked; attach to it or stop it before resuming")
		}
	}
	current, err := m.DB.Session(sourceID)
	if err != nil {
		return nil, err
	}
	if current.EndedAt == nil || current.TargetID != source.TargetID || current.TmuxSession != source.TmuxSession || current.Workdir != source.Workdir || current.Agent != source.Agent {
		return nil, fmt.Errorf("source session changed; refresh before resuming")
	}
	if name == "" {
		name = source.Name + " · resumed"
	}
	return m.Launch(ctx, LaunchOpts{TargetID: source.TargetID, ProjectID: source.ProjectID, GroupPath: source.GroupPath, Name: name, Agent: source.Agent, Model: source.Model, Workdir: source.Workdir, ResumeID: cid})
}
