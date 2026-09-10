package console

import (
	"strings"
	"testing"
)

func TestGroupedPreviewShowsEachSetupResult(t *testing.T) {
	m := sampleDashboard()
	m.width = 120
	m.height = 50
	m.preview.Width = 100
	m.preview.Height = 40
	m.rows = []row{{"id": float64(91), "name": "Grouped setup", "workspace": map[string]any{
		"state": "failed", "repositories": []any{
			map[string]any{"name": "API", "worktree": map[string]any{"state": "ready", "setup_command": "setup-api", "setup_state": "complete", "setup_output": "API prepared"}},
			map[string]any{"name": "Web", "worktree": map[string]any{"state": "failed", "setup_command": "setup-web", "setup_state": "failed", "setup_output": "missing package", "error": "exit 7"}},
		},
	}}}
	m.filter()
	m.updatePreview()
	text := m.preview.View()
	for _, want := range []string{"API: ready", "Project setup: complete", "API prepared", "Web: failed", "Project setup: failed", "missing package", "Setup error: exit 7"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
}
