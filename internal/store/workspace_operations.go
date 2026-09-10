package store

import (
	"database/sql"
	"fmt"
)

type WorkspaceOperation struct {
	ID              int64   `json:"id"`
	SessionID       int64   `json:"session_id"`
	PlanJSON        string  `json:"-"`
	State           string  `json:"state"`
	Error           string  `json:"error,omitempty"`
	CancelRequested bool    `json:"cancel_requested"`
	CreatedAt       float64 `json:"created_at"`
	UpdatedAt       float64 `json:"updated_at"`
}

func (o *WorkspaceOperation) Active() bool { return o.State == "running" || o.State == "recovering" }

const operationCols = `id,session_id,plan_json,state,error,cancel_requested,created_at,updated_at`

func scanWorkspaceOperation(s interface{ Scan(...any) error }) (*WorkspaceOperation, error) {
	var o WorkspaceOperation
	err := s.Scan(&o.ID, &o.SessionID, &o.PlanJSON, &o.State, &o.Error, &o.CancelRequested, &o.CreatedAt, &o.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return &o, err
}
func (db *DB) WorkspaceOperation(id int64) (*WorkspaceOperation, error) {
	return scanWorkspaceOperation(db.QueryRow(`SELECT `+operationCols+` FROM workspace_operations WHERE id=?`, id))
}
func (db *DB) WorkspaceOperations(sessionID int64, activeOnly bool) ([]*WorkspaceOperation, error) {
	query := `SELECT ` + operationCols + ` FROM workspace_operations WHERE 1=1`
	args := []any{}
	if sessionID != 0 {
		query += ` AND session_id=?`
		args = append(args, sessionID)
	}
	if activeOnly {
		query += ` AND state IN ('running','recovering')`
	}
	query += ` ORDER BY id DESC`
	if !activeOnly {
		query += ` LIMIT 20`
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*WorkspaceOperation{}
	for rows.Next() {
		o, err := scanWorkspaceOperation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Reserve the operation and its intended allocation in one durable transaction.
func (db *DB) BeginWorkspaceOperation(sessionID int64, oldPlan, newPlan string) (*WorkspaceOperation, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := Now()
	result, err := tx.Exec(`INSERT INTO workspace_operations(session_id,plan_json,created_at,updated_at) VALUES(?,?,?,?)`, sessionID, newPlan, now, now)
	if err != nil {
		return nil, fmt.Errorf("a workspace operation is already active or could not be reserved: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	updated, err := tx.Exec(`UPDATE sessions SET worktree_json=? WHERE id=? AND worktree_json=?`, newPlan, sessionID, oldPlan)
	if err != nil {
		return nil, err
	}
	n, _ := updated.RowsAffected()
	if n != 1 {
		return nil, fmt.Errorf("workspace changed while preparing the extension; refresh and retry")
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return db.WorkspaceOperation(id)
}
func (db *DB) NoteWorkspaceOperation(id int64, detail string) error {
	_, err := db.Exec(`UPDATE workspace_operations SET state='recovering',error=?,updated_at=? WHERE id=? AND state IN ('running','recovering')`, detail, Now(), id)
	return err
}
func (db *DB) CancelWorkspaceOperation(id int64) error {
	_, err := db.Exec(`UPDATE workspace_operations SET cancel_requested=1,updated_at=? WHERE id=? AND state IN ('running','recovering')`, Now(), id)
	return err
}
func (db *DB) FinishWorkspaceOperation(id int64, state, detail, actualPlan string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE workspace_operations SET state=CASE WHEN ?='failed' AND cancel_requested=1 THEN 'cancelled' ELSE ? END,error=?,updated_at=? WHERE id=? AND state IN ('running','recovering')`, state, state, detail, Now(), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil
	}
	if _, err = tx.Exec(`UPDATE sessions SET worktree_json=? WHERE id=(SELECT session_id FROM workspace_operations WHERE id=?)`, actualPlan, id); err != nil {
		return err
	}
	return tx.Commit()
}
