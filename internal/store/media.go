package store

import (
	"database/sql"
	"errors"
)

// Media is one thing an agent posted for the operator to look at.
type Media struct {
	ID        int64   `json:"id"`
	SessionID *int64  `json:"session_id"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Note      string  `json:"note"`
	Name      string  `json:"name"`
	Mime      string  `json:"mime"`
	Size      int64   `json:"size"`
	Blob      string  `json:"-"`
	URL       string  `json:"url"`
	Source    string  `json:"source"`
	CreatedAt float64 `json:"created_at"`

	// joined so the feed can say who posted without a second request
	SessionName string `json:"session_name,omitempty"`
}

const mediaCols = `m.id,m.session_id,m.kind,m.title,m.note,m.name,m.mime,m.size,m.blob,m.url,m.source,m.created_at,COALESCE(s.name,'')`

func scanMedia(sc interface{ Scan(...any) error }) (*Media, error) {
	m := new(Media)
	err := sc.Scan(&m.ID, &m.SessionID, &m.Kind, &m.Title, &m.Note, &m.Name, &m.Mime, &m.Size,
		&m.Blob, &m.URL, &m.Source, &m.CreatedAt, &m.SessionName)
	return m, err
}

// MediaList is newest first. sessionID 0 means every session.
func (db *DB) MediaList(sessionID int64, limit int) ([]*Media, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	q := `SELECT ` + mediaCols + ` FROM media m LEFT JOIN sessions s ON s.id=m.session_id`
	args := []any{}
	if sessionID > 0 {
		q += ` WHERE m.session_id=?`
		args = append(args, sessionID)
	}
	rows, err := db.Query(q+` ORDER BY m.id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Media{}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) MediaByID(id int64) (*Media, error) {
	m, err := scanMedia(db.QueryRow(`SELECT `+mediaCols+` FROM media m LEFT JOIN sessions s ON s.id=m.session_id WHERE m.id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (db *DB) InsertMedia(m *Media) (*Media, error) {
	res, err := db.Exec(`INSERT INTO media(session_id,kind,title,note,name,mime,size,blob,url,source,created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, m.SessionID, m.Kind, m.Title, m.Note, m.Name, m.Mime, m.Size, m.Blob, m.URL, m.Source, Now())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return db.MediaByID(id)
}

func (db *DB) DeleteMedia(id int64) error {
	_, err := db.Exec(`DELETE FROM media WHERE id=?`, id)
	return err
}

// LiveSessionByTmux resolves the session an agent is posting from. Tmux names
// are reused across a session's life only by that session, but an ended row
// can share one, so the live row wins and the newest breaks a tie.
func (db *DB) LiveSessionByTmux(name string) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM sessions WHERE tmux_session=? ORDER BY (ended_at IS NULL) DESC, id DESC LIMIT 1`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
