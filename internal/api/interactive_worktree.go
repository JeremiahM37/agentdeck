package api

import "net/http"

func (s *Server) removeSessionWorktree(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	if err := s.Sessions.RemoveWorktree(r.Context(), row.ID); err != nil {
		httpError(w, 409, "%s", err)
		return
	}
	var err error
	row, err = s.DB.Session(row.ID)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, 200, s.sessionView(row))
}
