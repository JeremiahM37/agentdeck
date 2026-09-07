package store

import (
	"database/sql"
	"errors"
)

const sessionCols = `s.id, s.project_id, s.target_id, s.name, s.agent, s.model,
	s.workdir, s.tmux_session, s.status, s.origin, s.pane_hash, s.pane_tail,
	s.context_pct, s.last_activity_at, s.created_at, s.updated_at, s.ended_at`

func scanSession(sc interface{ Scan(...any) error }, withJoin bool) (*Session, error) {
	var s Session
	dest := []any{&s.ID, &s.ProjectID, &s.TargetID, &s.Name, &s.Agent, &s.Model,
		&s.Workdir, &s.TmuxSession, &s.Status, &s.Origin, &s.PaneHash, &s.PaneTail,
		&s.ContextPct, &s.LastActivityAt, &s.CreatedAt, &s.UpdatedAt, &s.EndedAt}
	if withJoin {
		var projectName sql.NullString
		dest = append(dest, &projectName, &s.TargetName, &s.TargetKind)
		if err := sc.Scan(dest...); err != nil {
			return nil, err
		}
		s.ProjectName = projectName.String
		return &s, nil
	}
	return &s, sc.Scan(dest...)
}

const sessionJoin = `FROM sessions s
	LEFT JOIN projects p ON p.id=s.project_id
	JOIN targets t ON t.id=s.target_id`

// Sessions lists sessions newest first, joined with their project and target.
// `includeEnded` brings back the ones whose process is gone — the record is the
// point, so a dead session is still worth showing until it is dismissed.
func (db *DB) Sessions(includeEnded bool) ([]*Session, error) {
	q := `SELECT ` + sessionCols + `, p.name, t.name, t.kind ` + sessionJoin
	if !includeEnded {
		q += ` WHERE s.ended_at IS NULL`
	}
	q += ` ORDER BY s.created_at DESC`
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Session{}
	for rows.Next() {
		s, err := scanSession(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Session fetches one session by id.
func (db *DB) Session(id int64) (*Session, error) {
	s, err := scanSession(db.QueryRow(`SELECT `+sessionCols+`, p.name, t.name, t.kind `+
		sessionJoin+` WHERE s.id=?`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// LiveSessions are the ones the poller still has to watch.
func (db *DB) LiveSessions() ([]*Session, error) {
	rows, err := db.Query(`SELECT ` + sessionCols + ` FROM sessions s WHERE s.ended_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Session{}
	for rows.Next() {
		s, err := scanSession(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SessionByTmux finds a live session by its tmux name on a target. Discovery
// uses it to tell "already adopted" from "new to us".
func (db *DB) SessionByTmux(targetID int64, tmuxName string) (*Session, error) {
	s, err := scanSession(db.QueryRow(`SELECT `+sessionCols+` FROM sessions s
		WHERE s.target_id=? AND s.tmux_session=? AND s.ended_at IS NULL`,
		targetID, tmuxName), false)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

// InsertSession writes a new session row.
func (db *DB) InsertSession(s *Session) (*Session, error) {
	now := Now()
	res, err := db.Exec(`INSERT INTO sessions(project_id, target_id, name, agent, model,
		workdir, tmux_session, status, origin, last_activity_at, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ProjectID, s.TargetID, s.Name, nz(s.Agent, "claude"), s.Model, s.Workdir,
		s.TmuxSession, nz(s.Status, "starting"), nz(s.Origin, "agentdeck"), now, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return db.Session(id)
}

// Wraps lists a project's handoffs, newest first — the thread of a long project
// across every context that has worked on it.
func (db *DB) Wraps(projectID int64, limit int) ([]*Wrap, error) {
	rows, err := db.Query(`SELECT id, session_id, project_id, summary, next_session_id,
		created_at FROM session_wraps WHERE project_id=? ORDER BY id DESC LIMIT ?`,
		projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Wrap{}
	for rows.Next() {
		var w Wrap
		if err := rows.Scan(&w.ID, &w.SessionID, &w.ProjectID, &w.Summary,
			&w.NextSessionID, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &w)
	}
	return out, rows.Err()
}

// SessionWraps lists the handoffs a single session produced.
func (db *DB) SessionWraps(sessionID int64) ([]*Wrap, error) {
	rows, err := db.Query(`SELECT id, session_id, project_id, summary, next_session_id,
		created_at FROM session_wraps WHERE session_id=? ORDER BY id DESC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Wrap{}
	for rows.Next() {
		var w Wrap
		if err := rows.Scan(&w.ID, &w.SessionID, &w.ProjectID, &w.Summary,
			&w.NextSessionID, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &w)
	}
	return out, rows.Err()
}

// InsertWrap records a handoff summary.
func (db *DB) InsertWrap(w *Wrap) (int64, error) {
	res, err := db.Exec(`INSERT INTO session_wraps(session_id, project_id, summary,
		transcript, next_session_id, created_at) VALUES(?,?,?,?,?,?)`,
		w.SessionID, w.ProjectID, w.Summary, w.Transcript, w.NextSessionID, Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
