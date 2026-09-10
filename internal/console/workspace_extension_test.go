package console

import (
	"strings"
	"testing"
)

func TestWorkspaceExtensionFormFiltersTargetsAndExistingRepositories(t *testing.T) {
	m := sampleDashboard()
	m.rows = []row{{"id": float64(1), "target_id": float64(2), "workspace": map[string]any{"repositories": []any{map[string]any{"project_id": float64(3), "worktree": map[string]any{"repo": "/old"}}}}}}
	m.projects = []row{{"id": float64(3), "target_id": float64(2), "name": "Existing", "repo_path": "/old"}, {"id": float64(4), "target_id": float64(9), "name": "Other target"}, {"id": float64(5), "target_id": float64(2), "name": "Available", "repo_path": "/new"}}
	m.filter()
	m.extendWorkspaceForm()
	if m.form == nil || len(m.form.fields[0].Options) != 1 || m.form.fields[0].Options[0].Value != "5" {
		t.Fatalf("wrong project choices: %+v", m.form)
	}
	text := formatDetail("Repository addition progress", []byte(`[{"id":7,"state":"recovering","cancel_requested":true,"error":"Target unavailable"}]`))
	for _, expected := range []string{"Addition 7: recovering", "cancellation requested", "Target unavailable"} {
		if !strings.Contains(text, expected) {
			t.Fatal(text)
		}
	}
	m.Update(resultMsg{label: "Add repository", data: []byte(`{"id":7,"state":"running"}`)})
	if !strings.Contains(m.notice, "started") || strings.Contains(m.notice, "completed") {
		t.Fatal(m.notice)
	}
}
