package sessions

import (
	"context"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

// Poll refreshes every live session's status from its target.
//
// It batches by target: one capture command per machine per tick, however many
// sessions are on it. Status is derived from what the pane is actually doing —
// see DeriveStatus — and `last_activity_at` is only moved when the pane really
// changed, because "quiet for 40 minutes" is the number an operator acts on.
func (m *Manager) Poll(ctx context.Context) { m.poll(ctx, nil) }

// Action refreshes visit only the affected target, not every machine in the fleet.
func (m *Manager) pollTarget(ctx context.Context, targetID int64) { m.poll(ctx, &targetID) }

func (m *Manager) poll(ctx context.Context, onlyTarget *int64) {
	live, err := m.DB.LiveSessions()
	if err != nil || len(live) == 0 {
		return
	}
	byTarget := map[int64][]*store.Session{}
	for _, s := range live {
		if onlyTarget != nil && s.TargetID != *onlyTarget {
			continue
		}
		if s.SetupState == "creating" {
			if !m.setupActive(s.ID) {
				m.recoverSetup(ctx, s)
			}
			continue
		}
		byTarget[s.TargetID] = append(byTarget[s.TargetID], s)
	}
	for targetID, group := range byTarget {
		target, err := m.DB.Target(targetID)
		if err != nil {
			continue
		}
		ex, err := m.Reg.For(target)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(group))
		for _, s := range group {
			names = append(names, s.TmuxSession)
		}
		r, err := ex.Run(ctx, PollCommand(names), executor.RunOpts{Timeout: 45})
		if err != nil || !r.OK() {
			// an unreachable target is not evidence a session died; leave the
			// rows alone and try again next tick
			m.Log.Debug("session poll failed", "target", target.Name, "err", err, "exit_code", r.RC)
			continue
		}
		panes, complete := ParsePollSnapshot(r.Stdout, names)
		if !complete {
			m.Log.Debug("incomplete session poll", "target", target.Name)
			continue
		}
		for _, s := range group {
			pane := panes[s.TmuxSession]
			if pane.Failed {
				continue
			}
			m.applyPane(s, pane.Text, pane.Missing)
		}
	}
}

// applyPane folds one capture into a session row, publishing only on a real
// change so the board's SSE stream stays quiet while nothing is happening.
func (m *Manager) applyPane(s *store.Session, pane string, missing bool) {
	if s.SetupState == "creating" {
		if m.setupActive(s.ID) {
			return
		}
		current, err := m.DB.Session(s.ID)
		if err != nil || current.SetupState != "creating" {
			return // A snapshot taken before setup completed is not an interruption.
		}
		// A persisted reservation without this process's worker was interrupted.
		// Keep any target allocation for inspection; do not launch twice.
		now := store.Now()
		if err := m.DB.Update("sessions", s.ID, map[string]any{"setup_state": "failed", "setup_error": "Setup was interrupted; inspect the workspace before retrying", "status": StatusDead, "ended_at": now, "updated_at": now}); err == nil {
			if fresh, err := m.DB.Session(s.ID); err == nil {
				m.publish(fresh)
			}
		}
		return
	}
	now := store.Now()
	fields := map[string]any{"updated_at": now}
	var status string

	if missing {
		// Only an explicit missing-session response establishes absence. A brand-new session gets a grace period
		// before we call it dead, since launch and first paint are not instant.
		if s.Status == StatusStarting && now-s.CreatedAt < 20 {
			return
		}
		status = StatusDead
		fields["ended_at"] = now
	} else {
		status = DeriveStatus(pane, s.PaneHash)
		hash := Hash(pane)
		if hash != s.PaneHash {
			fields["pane_hash"] = hash
			fields["pane_tail"] = Preview(pane, 8)
			// The FIRST time we see a pane we cannot know it just changed — an
			// adopted session may have been sitting there for days. Moving the
			// activity clock here would reset every adopted session to "quiet
			// 0s" the moment agentdeck noticed it.
			if s.PaneHash != "" {
				fields["last_activity_at"] = now
			}
		}
		if pct := ContextPct(pane); pct != nil {
			fields["context_pct"] = *pct
		}
	}
	changed := status != s.Status || len(fields) > 1
	fields["status"] = status
	if err := m.DB.Update("sessions", s.ID, fields); err != nil {
		return
	}
	if !changed {
		return
	}
	if fresh, err := m.DB.Session(s.ID); err == nil {
		m.publish(fresh)
		if status == StatusDead && s.Status != StatusDead {
			m.Log.Info("session ended", "session", s.ID, "name", s.Name)
		}
	}
}

// IdleFor is how long a session has been quiet — the honest signal behind every
// status label, and the one the UI shows next to it.
func IdleFor(s *store.Session) time.Duration {
	last := s.CreatedAt
	if s.LastActivityAt != nil && *s.LastActivityAt > last {
		last = *s.LastActivityAt
	}
	d := time.Duration((store.Now() - last) * float64(time.Second))
	if d < 0 {
		return 0
	}
	return d
}
