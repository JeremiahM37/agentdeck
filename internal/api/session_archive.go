package api

import "net/http"

func (s *Server) archiveSession(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	var in struct {
		Stop bool `json:"stop"`
	}
	if err := decodeBody(r, &in); err != nil {
		httpError(w, 422, "provide stop=true to end a live terminal, or stop=false for an already stopped record")
		return
	}
	next, err := s.Sessions.Archive(r.Context(), row.ID, in.Stop)
	if err != nil {
		httpError(w, 409, "%s", err)
		return
	}
	writeJSON(w, 200, s.sessionView(next))
}
func (s *Server) unarchiveSession(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	next, err := s.Sessions.Unarchive(row.ID)
	if err != nil {
		httpError(w, 409, "%s", err)
		return
	}
	writeJSON(w, 200, s.sessionView(next))
}
func (s *Server) archivedHistory(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	var text string
	if err := s.DB.QueryRow("SELECT archive_text FROM sessions WHERE id=?", row.ID).Scan(&text); err != nil {
		respondErr(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"text": text, "note": "Snapshot of the last 10,000 scrollback lines plus the visible screen, limited to 2 MiB; saved agent conversations remain separate."})
}
