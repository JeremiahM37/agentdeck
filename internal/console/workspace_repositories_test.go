package console

import (
	"encoding/json"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkspaceRepositoryWorkflowPreservesDraftAndRequest(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(503)
		w.Write([]byte(`{"detail":"retry later"}`))
	}))
	defer server.Close()
	m := sampleDashboard()
	m.client = New(server.URL, "")
	m.projects = []row{{"id": float64(1), "target_id": float64(1), "name": "Primary"}, {"id": float64(2), "target_id": float64(1), "name": "Second"}, {"id": float64(3), "target_id": float64(2), "name": "Wrong target"}}
	m.newForm()
	original := m.form
	body := map[string]any{"name": "keep draft", "project_id": int64(1), "worktree": map[string]any{"base": "main"}}
	m.workspaceRepositoryForm(body, original)
	for _, option := range m.form.fields[0].Options {
		if strings.Contains(option.Label, "Wrong target") {
			t.Fatal("mixed-target choice exposed")
		}
	}
	m.form.submit(map[string]any{"action": "add/2"})
	m.form.submit(map[string]any{"base": "release"})
	m.updateForm(tea.KeyMsg{Type: tea.KeyEsc})
	if m.form != original {
		t.Fatal("back lost original draft")
	}
	m.workspaceRepositoryForm(body, original)
	if !strings.Contains(m.form.title, "Second @ release") {
		t.Fatal("back lost repository selection")
	}
	cmd := m.form.submit(map[string]any{"action": "create"})
	m.Update(cmd())
	if m.form == nil || !strings.Contains(m.form.title, "Second") {
		t.Fatal("failed request lost draft")
	}
	extra := received["worktree"].(map[string]any)["extra_repositories"].([]any)
	if len(extra) != 1 || extra[0].(map[string]any)["base"] != "release" || received["name"] != "keep draft" {
		t.Fatal(received)
	}
	m.width = 60
	for _, line := range strings.Split(m.formView(), "\n") {
		if ansi.StringWidth(line) > 60 {
			t.Fatalf("form overflow: %q", line)
		}
	}
	m.form.submit(map[string]any{"action": "edit/2"})
	m.form.submit(map[string]any{"operation": "remove"})
	if strings.Contains(m.form.title, "Second") {
		t.Fatal("remove failed")
	}
}
