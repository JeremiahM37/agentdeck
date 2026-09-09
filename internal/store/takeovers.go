package store

import "database/sql"

type Takeover struct {
	TaskID    int64  `json:"task_id"`
	AttemptID int64  `json:"attempt_id"`
	SessionID *int64 `json:"session_id"`
	Status    string `json:"status"`
	Error     string `json:"error"`
}

func (db *DB) Takeover(taskID int64) (*Takeover, error) {
	var t Takeover
	err := db.QueryRow("SELECT task_id,attempt_id,session_id,status,error FROM task_takeovers WHERE task_id=?", taskID).Scan(&t.TaskID, &t.AttemptID, &t.SessionID, &t.Status, &t.Error)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &t, err
}

// Session workspaces remain the operator's even after a terminal is closed.
func (db *DB) SessionWorkdir(targetID int64, path string) bool {
	var n int
	err := db.QueryRow("SELECT count(*) FROM sessions WHERE target_id=? AND workdir=?", targetID, path).Scan(&n)
	return err != nil || n > 0
}
