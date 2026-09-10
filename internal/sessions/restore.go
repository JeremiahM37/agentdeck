package sessions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/shellq"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

// A local tmux option survives detachment but disappears with the session.
// Names, working directories and recent timestamps cannot prove this identity.
const trackingOption = "@agentdeck-tracking-identity"

func trackingIdentityCommand(name, seed string) string {
	target := shellq.Quote("=" + name + ":")
	read := "tmux show-options -qv -t " + target + " " + trackingOption
	if seed == "" {
		return read
	}
	return "tmux set-option -o -t " + target + " " + trackingOption + " " + shellq.Quote(seed) + " && " + read
}

func validTrackingIdentity(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 16
}

func captureTrackingIdentity(ctx context.Context, ex executor.Executor, name string) string {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ""
	}
	r, err := ex.Run(ctx, trackingIdentityCommand(name, hex.EncodeToString(nonce[:])), executor.RunOpts{Timeout: 10})
	value := strings.TrimSpace(r.Stdout)
	if err != nil || !r.OK() || !validTrackingIdentity(value) {
		return ""
	}
	return value
}

// Restore resumes monitoring the original adopted tmux session. It never
// launches an agent, recreates tmux or guesses a native conversation ID.
func (m *Manager) Restore(ctx context.Context, id int64) (*store.Session, error) {
	sess, ex, err := m.resolve(id)
	if err != nil {
		return nil, err
	}
	if sess.EndedAt == nil {
		return nil, fmt.Errorf("this session is already tracked")
	}
	if sess.Origin != "discovered" || sess.Status == StatusDead || !validTrackingIdentity(sess.TrackingIdentity) {
		return nil, fmt.Errorf("this record cannot identify a running session; use Find running sessions to adopt one explicitly")
	}
	if _, err := m.DB.SessionByTmux(sess.TargetID, sess.TmuxSession); err == nil {
		return nil, fmt.Errorf("this tmux session is already tracked by another record")
	} else if err != store.ErrNotFound {
		return nil, err
	}
	r, err := ex.Run(ctx, trackingIdentityCommand(sess.TmuxSession, ""), executor.RunOpts{Timeout: 10})
	if err != nil {
		return nil, err
	}
	if !r.OK() || strings.TrimSpace(r.Stdout) != sess.TrackingIdentity {
		return nil, fmt.Errorf("the original tmux session is gone or has changed; use Find running sessions instead")
	}
	err = func() error {
		m.lifecycleMu.Lock()
		defer m.lifecycleMu.Unlock()
		current, err := m.DB.Session(id)
		if err != nil {
			return err
		}
		if current.EndedAt == nil {
			return fmt.Errorf("this session is already tracked")
		}
		if current.Status == StatusDead || current.TrackingIdentity != sess.TrackingIdentity || current.TargetID != sess.TargetID || current.TmuxSession != sess.TmuxSession {
			return fmt.Errorf("session changed while checking identity; retry recovery")
		}
		if _, err := m.DB.SessionByTmux(sess.TargetID, sess.TmuxSession); err == nil {
			return ErrAlreadyAdopted
		} else if err != store.ErrNotFound {
			return err
		}
		return m.DB.Update("sessions", id, map[string]any{"ended_at": nil, "status": StatusIdle, "updated_at": store.Now()})
	}()
	if err != nil {
		return nil, err
	}
	m.pollTarget(ctx, sess.TargetID)
	fresh, err := m.DB.Session(id)
	if err != nil {
		return nil, err
	}
	m.publish(fresh)
	return fresh, nil
}
