package api

import "net/http"

func (s *Server) extendSessionWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, 404, "no such session")
		return
	}
	var input struct {
		ProjectID int64  `json:"project_id"`
		Base      string `json:"base"`
	}
	if err = decodeBody(r, &input); err != nil || input.ProjectID <= 0 || len(input.Base) > 512 {
		httpError(w, 422, "choose a project and a valid base")
		return
	}
	op, err := s.Sessions.ExtendWorkspace(r.Context(), id, input.ProjectID, input.Base)
	if err != nil {
		httpError(w, 409, "%s", err.Error())
		return
	}
	writeJSON(w, 202, op)
}
func (s *Server) workspaceOperations(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, 404, "no such session")
		return
	}
	if _, err = s.DB.Session(id); err != nil {
		httpError(w, 404, "no such session")
		return
	}
	rows, err := s.DB.WorkspaceOperations(id, false)
	if err != nil {
		respondErr(w, err)
		return
	}
	writeJSON(w, 200, rows)
}
func (s *Server) workspaceOperation(w http.ResponseWriter, r *http.Request) {
	s.workspaceOperationAction(w, r, "")
}
func (s *Server) cancelWorkspaceOperation(w http.ResponseWriter, r *http.Request) {
	s.workspaceOperationAction(w, r, "cancel")
}
func (s *Server) recoverWorkspaceOperation(w http.ResponseWriter, r *http.Request) {
	s.workspaceOperationAction(w, r, "recover")
}
func (s *Server) workspaceOperationAction(w http.ResponseWriter, r *http.Request, action string) {
	id, e := pathID(r, "id")
	operation, err := pathID(r, "operation")
	if e != nil || err != nil {
		httpError(w, 404, "no such workspace operation")
		return
	}
	op, err := s.Sessions.WorkspaceOperation(id, operation)
	if err != nil {
		httpError(w, 404, "no such workspace operation")
		return
	}
	switch action {
	case "cancel":
		op, err = s.Sessions.CancelWorkspaceOperation(r.Context(), id, operation)
	case "recover":
		op, err = s.Sessions.RecoverWorkspaceOperation(r.Context(), id, operation)
	}
	if err != nil {
		httpError(w, 409, "%s", err.Error())
		return
	}
	writeJSON(w, 200, op)
}
