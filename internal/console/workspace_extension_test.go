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

func TestWorkspaceActionsHideExtensionControlsDuringInitialSetup(t *testing.T) {
	r := row{
		"id":          float64(1),
		"setup_state": "creating",
		"workspace": map[string]any{
			"state": "creating",
			"repositories": []any{map[string]any{
				"project_id": float64(3),
				"worktree":   map[string]any{"state": "ready"},
			}},
		},
	}
	progress := false
	for _, action := range workspaceActions(r, "/sessions/1") {
		if strings.Contains(action.Label, "repository") || strings.Contains(action.Label, "Repository") {
			t.Fatalf("initial setup exposed extension action %q", action.Label)
		}
		progress = progress || action.Label == "Workspace setup progress"
	}
	if !progress {
		t.Fatal("initial setup hid workspace progress")
	}

	r["setup_state"] = "ready"
	found := false
	for _, action := range workspaceActions(r, "/sessions/1") {
		if action.Label == "Add repository" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ready workspace omitted extension action")
	}
}
