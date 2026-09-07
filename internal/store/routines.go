package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Routine is a job you saved because you keep asking for it.
//
// It is the difference between typing "go through every open PR on librarr,
// sglang and gamarr, test it end to end, fix what is wrong, and merge once CI is
// green" for the fifth time, and pressing one button. A routine carries the
// prompt and everything a dispatch needs, and can run on a schedule or only when
// you ask.
type Routine struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Prompt is what the agent is asked to do, once per project.
	Prompt string `json:"prompt"`
	// Title prefixes the tasks it creates, so a board full of them is readable.
	Title string `json:"title"`
	// ProjectIDs is every project this runs against — one task each.
	ProjectIDs []int64 `json:"project_ids"`

	Agent          string `json:"agent"`
	Model          string `json:"model"`
	PermissionMode string `json:"permission_mode"`

	// Schedule empty means manual only. See routines.ParseSchedule.
	Schedule string `json:"schedule"`
	Enabled  bool   `json:"enabled"`
	// Dispatch runs the tasks immediately rather than leaving them queued.
	Dispatch bool `json:"dispatch"`

	LastRunAt *float64 `json:"last_run_at"`
	NextRunAt *float64 `json:"next_run_at"`
	CreatedAt float64  `json:"created_at"`
}

const routineCols = `id, name, prompt, title, project_ids, agent, model,
	permission_mode, schedule, enabled, dispatch, last_run_at, next_run_at, created_at`

func scanRoutine(sc interface{ Scan(...any) error }) (*Routine, error) {
	var r Routine
	var ids string
	var enabled, dispatch int
	if err := sc.Scan(&r.ID, &r.Name, &r.Prompt, &r.Title, &ids, &r.Agent, &r.Model,
		&r.PermissionMode, &r.Schedule, &enabled, &dispatch,
		&r.LastRunAt, &r.NextRunAt, &r.CreatedAt); err != nil {
		return nil, err
	}
	r.Enabled, r.Dispatch = enabled == 1, dispatch == 1
	json.Unmarshal([]byte(ids), &r.ProjectIDs)
	if r.ProjectIDs == nil {
		r.ProjectIDs = []int64{}
	}
	return &r, nil
}

// Routines lists every saved routine, newest first.
func (db *DB) Routines() ([]*Routine, error) {
	rows, err := db.Query(`SELECT ` + routineCols + ` FROM routines ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Routine{}
	for rows.Next() {
		r, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Routine fetches one by id.
func (db *DB) Routine(id int64) (*Routine, error) {
	r, err := scanRoutine(db.QueryRow(`SELECT `+routineCols+` FROM routines WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// DueRoutines returns the enabled, scheduled routines whose time has come.
func (db *DB) DueRoutines(now float64) ([]*Routine, error) {
	rows, err := db.Query(`SELECT `+routineCols+` FROM routines
		WHERE enabled=1 AND schedule != '' AND next_run_at IS NOT NULL AND next_run_at <= ?
		ORDER BY next_run_at`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Routine{}
	for rows.Next() {
		r, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertRoutine saves a new routine.
func (db *DB) InsertRoutine(r *Routine) (*Routine, error) {
	ids, _ := json.Marshal(r.ProjectIDs)
	if r.ProjectIDs == nil {
		ids = []byte("[]")
	}
	r.CreatedAt = Now()
	res, err := db.Exec(`INSERT INTO routines
		(name, prompt, title, project_ids, agent, model, permission_mode,
		 schedule, enabled, dispatch, next_run_at, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.Name, r.Prompt, r.Title, string(ids), r.Agent, r.Model, r.PermissionMode,
		r.Schedule, boolInt(r.Enabled), boolInt(r.Dispatch), r.NextRunAt, r.CreatedAt)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.Routine(id)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
