package store

// TaskMessage is an operator instruction and its delivery receipt.
type TaskMessage struct {
	ID        int64   `json:"id"`
	TaskID    int64   `json:"task_id"`
	RequestID string  `json:"request_id"`
	Text      string  `json:"text"`
	Interrupt bool    `json:"interrupt"`
	Status    string  `json:"status"`
	AttemptID *int64  `json:"attempt_id"`
	Error     string  `json:"error,omitempty"`
	CreatedAt float64 `json:"created_at"`
}

func (db *DB) TaskMessages(taskID int64) ([]*TaskMessage, error) {
	rows, err := db.Query(`SELECT id,task_id,request_id,text,interrupt,status,attempt_id,error,created_at
 FROM task_messages WHERE task_id=? ORDER BY id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TaskMessage{}
	for rows.Next() {
		m := new(TaskMessage)
		if err := rows.Scan(&m.ID, &m.TaskID, &m.RequestID, &m.Text, &m.Interrupt, &m.Status, &m.AttemptID, &m.Error, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
