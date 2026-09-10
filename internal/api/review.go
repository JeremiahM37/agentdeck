package api

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"path"

	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/shellq"
)

//go:embed scripts/review.py
var reviewScript string

func (s *Server) terminalChanges(w http.ResponseWriter, r *http.Request) {
	att, target, err := s.resolveAttachment(r.PathValue("kind"), r.PathValue("id"))
	if err != nil {
		httpError(w, 404, "%s", err)
		return
	}
	dir, _, err := s.terminalDirectory(r.PathValue("kind"), r.PathValue("id"))
	if err != nil || !path.IsAbs(dir) {
		httpError(w, 409, "workspace unavailable")
		return
	}
	ex, err := s.Reg.For(target)
	if err != nil {
		respondErr(w, err)
		return
	}
	if target.Kind == "sandbox" {
		ex = executor.NewPct(att.SandboxVMID)
	}
	scope := r.URL.Query().Get("scope")
	if scope == "" {
		scope = "working"
	}
	if scope != "working" && scope != "staged" {
		httpError(w, 400, "choose working or staged changes")
		return
	}
	cmd := "python3 -c " + shellq.Quote(reviewScript) + " " + shellq.Quote(dir) + " " + shellq.Quote(scope) + " " + shellq.Quote(r.URL.Query().Get("path"))
	result, err := ex.Run(r.Context(), cmd, executor.RunOpts{Timeout: 30})
	var out map[string]json.RawMessage
	if err != nil || json.Unmarshal([]byte(result.Stdout), &out) != nil {
		httpError(w, 502, "could not read Git changes on this target")
		return
	}
	if !result.OK() {
		var message string
		json.Unmarshal(out["error"], &message)
		httpError(w, 409, "%s", message)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}
