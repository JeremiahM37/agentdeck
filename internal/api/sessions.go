package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/JeremiahM37/agentdeck/internal/memory"
	"github.com/JeremiahM37/agentdeck/internal/sessions"
	"github.com/JeremiahM37/agentdeck/internal/store"
	"github.com/JeremiahM37/agentdeck/internal/terminal"
)

// sessionView is a session plus the two things a card always needs: how long it
// has been quiet, and whether a handoff is currently being written.
type sessionView struct {
	*store.Session
	IdleSeconds     float64 `json:"idle_seconds"`
	UptimeSeconds   float64 `json:"uptime_seconds"`
	HandoffInFlight bool    `json:"handoff_in_flight"`
	Wraps           int     `json:"wraps"`
}

func (s *Server) sessionView(row *store.Session) *sessionView {
	v := &sessionView{
		Session:         row,
		IdleSeconds:     sessions.IdleFor(row).Seconds(),
		UptimeSeconds:   store.Now() - row.CreatedAt,
		HandoffInFlight: s.Sessions.InFlight(row.ID),
	}
	if wraps, err := s.DB.SessionWraps(row.ID); err == nil {
		v.Wraps = len(wraps)
	}
	return v
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Sessions(r.URL.Query().Get("all") == "true")
	if err != nil {
		respondErr(w, err)
		return
	}
	out := make([]*sessionView, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.sessionView(row))
	}
	writeJSON(w, 200, out)
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, s.sessionView(row))
}

type sessionIn struct {
	ProjectID *int64 `json:"project_id"`
	TargetID  int64  `json:"target_id"`
	Name      string `json:"name"`
	Agent     string `json:"agent"`
	Model     string `json:"model"`
	Workdir   string `json:"workdir"`
	// Resume picks the agent's own previous conversation back up, which is what
	// you want when re-opening a project you were in yesterday.
	Resume bool   `json:"resume"`
	Prime  string `json:"prime"`
	// Brief prepends what the memory provider knows about the project, so a
	// fresh session starts with the project's knowledge rather than a blank slate.
	Brief bool `json:"brief"`
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var in sessionIn
	if err := decodeBody(r, &in); err != nil {
		httpError(w, 422, "%s", err.Error())
		return
	}
	if in.Agent != "" && !oneOf(in.Agent, sessions.InteractiveAgents...) {
		httpError(w, 422, "agent must be one of %v", sessions.InteractiveAgents)
		return
	}
	// a project implies its target, so the caller only has to name one of them
	if in.TargetID == 0 && in.ProjectID != nil {
		proj, err := s.DB.Project(*in.ProjectID)
		if err != nil {
			httpError(w, 400, "no such project")
			return
		}
		in.TargetID = proj.TargetID
	}
	if in.TargetID == 0 {
		httpError(w, 400, "a session needs a project or a target")
		return
	}
	if _, err := s.DB.Target(in.TargetID); err != nil {
		httpError(w, 400, "no such target")
		return
	}
	prime := in.Prime
	if in.Brief && in.ProjectID != nil {
		if proj, err := s.DB.Project(*in.ProjectID); err == nil {
			if brief := s.projectBrief(r.Context(), proj); brief != "" {
				prime = strings.TrimSpace(brief + "\n\n" + prime)
			}
		}
	}
	sess, err := s.Sessions.Launch(r.Context(), sessions.LaunchOpts{
		ProjectID: in.ProjectID, TargetID: in.TargetID, Name: in.Name,
		Agent: in.Agent, Model: in.Model, Workdir: in.Workdir,
		Resume: in.Resume, Prime: prime,
	})
	if err != nil {
		httpError(w, 409, "%s", err.Error())
		return
	}
	writeJSON(w, 201, s.sessionView(sess))
}

// projectBrief gathers what this project already knows: the memory provider's
// facts plus the most recent session handoff.
func (s *Server) projectBrief(ctx context.Context, proj *store.Project) string {
	var parts []string
	if s.Memory != nil && s.Memory.Available(ctx) {
		if facts, err := s.Memory.Recall(ctx, proj.Name, 8); err == nil {
			if block := memory.Prime(facts); block != "" {
				parts = append(parts, block)
			}
		}
	}
	if wraps, err := s.DB.Wraps(proj.ID, 1); err == nil && len(wraps) > 0 {
		parts = append(parts, "## Where the last session left off\n\n"+
			strings.TrimSpace(wraps[0].Summary))
	}
	return strings.Join(parts, "\n\n")
}

type sendIn struct {
	Text string `json:"text"`
	Key  string `json:"key"`
}

// sendToSession is the phone-side keyboard: type a message, or press one of the
// allowlisted keys (Escape interrupts a turn), without attaching a terminal.
func (s *Server) sendToSession(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	var in sendIn
	if err := decodeBody(r, &in); err != nil {
		httpError(w, 422, "%s", err.Error())
		return
	}
	if row.Status == sessions.StatusDead {
		httpError(w, 409, "this session has ended")
		return
	}
	switch {
	case in.Key != "":
		if err := s.Sessions.SendKey(r.Context(), row.ID, in.Key); err != nil {
			httpError(w, 422, "%s", err.Error())
			return
		}
	case strings.TrimSpace(in.Text) != "":
		if err := s.Sessions.SendText(r.Context(), row.ID, in.Text); err != nil {
			httpError(w, 409, "%s", err.Error())
			return
		}
	default:
		httpError(w, 400, "send needs text or key")
		return
	}
	writeJSON(w, 200, map[string]any{"sent": true})
}

func (s *Server) attachSession(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	if row.Status == sessions.StatusDead {
		httpError(w, 409, "this session has ended")
		return
	}
	target, err := s.DB.Target(row.TargetID)
	if err != nil {
		respondErr(w, err)
		return
	}
	port, err := s.Terminals.Attach(r.Context(), terminal.Attachment{
		Key:         fmt.Sprintf("session:%d", row.ID),
		TmuxSession: row.TmuxSession,
	}, target)
	if err != nil {
		httpError(w, 503, "%s", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"port": port, "tmux_session": row.TmuxSession})
}

// deleteSession stops tracking a session, and kills its process ONLY when
// agentdeck owns that process.
//
// The asymmetry is the whole point. A session agentdeck launched is ours to end.
// A session the operator started themselves — the long-running conversation they
// have had open for a week — is not: adopting it was supposed to be
// non-destructive, so un-adopting it must be too. Killing one requires saying so
// explicitly with ?kill=true.
//
// This is not hypothetical. A bulk re-adopt during development called DELETE on
// seven adopted sessions and terminated seven live Claude conversations, because
// the default was "kill" for everything.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	kill := r.URL.Query().Get("kill") == "true"
	owned := row.Origin != "discovered"
	var err error
	switch {
	case row.Status == sessions.StatusDead:
		err = s.Sessions.Dismiss(row.ID) // nothing to kill; drop the card
	case kill || owned:
		err = s.Sessions.Kill(r.Context(), row.ID)
	default:
		// adopted and no explicit kill: let go of it, leave it running
		err = s.Sessions.Release(row.ID)
	}
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"id": row.ID, "killed": kill || (owned && row.Status != sessions.StatusDead)})
}

type handoffIn struct {
	Successor bool   `json:"successor"`
	KillOld   bool   `json:"kill_old"`
	Agent     string `json:"agent"`
	Model     string `json:"model"`
}

// handoffSession asks a session to write its wrap. It returns immediately: an
// agent mid-turn can take minutes to answer, and the operator should not hold a
// request open to discover that.
func (s *Server) handoffSession(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	if row.Status == sessions.StatusDead {
		httpError(w, 409, "this session has ended — nothing left to ask")
		return
	}
	var in handoffIn
	if err := decodeBody(r, &in); err != nil {
		httpError(w, 422, "%s", err.Error())
		return
	}
	if in.Agent != "" && !oneOf(in.Agent, sessions.InteractiveAgents...) {
		httpError(w, 422, "agent must be one of %v", sessions.InteractiveAgents)
		return
	}
	if err := s.Sessions.StartHandoff(row.ID, sessions.HandoffOpts{
		Successor: in.Successor, KillOld: in.KillOld,
		Agent: in.Agent, Model: in.Model}); err != nil {
		httpError(w, 409, "%s", err.Error())
		return
	}
	writeJSON(w, 202, map[string]any{"started": true, "session_id": row.ID})
}

func (s *Server) sessionWraps(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	wraps, err := s.DB.SessionWraps(row.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, 200, wraps)
}

// projectWraps is a project's handoff thread — the continuity that survives
// every individual conversation.
func (s *Server) projectWraps(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, 404, "no such project")
		return
	}
	wraps, err := s.DB.Wraps(id, 50)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, 200, wraps)
}

func (s *Server) discoverSessions(w http.ResponseWriter, r *http.Request) {
	found, err := s.Sessions.Discover(r.Context())
	if err != nil {
		respondErr(w, err)
		return
	}
	if found == nil {
		found = []sessions.Candidate{}
	}
	writeJSON(w, 200, found)
}

type adoptIn struct {
	TargetID    int64  `json:"target_id"`
	TmuxSession string `json:"tmux_session"`
	ProjectID   *int64 `json:"project_id"`
	Name        string `json:"name"`
	Agent       string `json:"agent"`
	Model       string `json:"model"`
	Workdir     string `json:"workdir"`
}

func (s *Server) adoptSession(w http.ResponseWriter, r *http.Request) {
	var in adoptIn
	if err := decodeBody(r, &in); err != nil {
		httpError(w, 422, "%s", err.Error())
		return
	}
	sess, err := s.Sessions.Adopt(r.Context(), sessions.AdoptOpts{
		TargetID: in.TargetID, TmuxSession: in.TmuxSession, ProjectID: in.ProjectID,
		Name: in.Name, Agent: in.Agent, Model: in.Model, Workdir: in.Workdir,
	})
	if errors.Is(err, sessions.ErrAlreadyAdopted) {
		httpError(w, 409, "%s", err.Error())
		return
	}
	if err != nil {
		httpError(w, 400, "%s", err.Error())
		return
	}
	writeJSON(w, 201, s.sessionView(sess))
}

func (s *Server) sessionParam(w http.ResponseWriter, r *http.Request) (*store.Session, bool) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, 404, "no such session")
		return nil, false
	}
	row, err := s.DB.Session(id)
	if err != nil {
		httpError(w, 404, "no such session")
		return nil, false
	}
	return row, true
}
