package api

import (
	"fmt"
	"net/http"
)

func (s *Server) sessionWorkspaceProgress(w http.ResponseWriter, r *http.Request) {
	row, ok := s.sessionParam(w, r)
	if !ok {
		return
	}
	plan, err := s.Sessions.WorkspaceProgress(r.Context(), row.ID)
	if err != nil {
		httpError(w, 409, "%s", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("format") == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "Recorded workspace state: %s\nBranch: %s\n\n", plan.State, plan.Branch)
		for _, repo := range plan.Repositories {
			fmt.Fprintf(w, "%s: %s\n", repo.Name, repo.Worktree.State)
			if repo.Worktree.Error != "" {
				fmt.Fprintf(w, "  %s\n", repo.Worktree.Error)
			}
		}
		if plan.Error != "" {
			fmt.Fprintf(w, "\n%s\n", plan.Error)
		}
		return
	}
	writeJSON(w, 200, plan)
}

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
