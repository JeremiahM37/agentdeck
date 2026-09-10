package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func key(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func sampleDashboard() *dashboard {
	m := newDashboard(New("http://unused", ""), nil)
	m.rows = []row{{"id": float64(1), "name": "Alpha UI", "project_name": "Website", "target_name": "Laptop", "status": "running", "pane_tail": "hello"}, {"id": float64(2), "name": "API repair", "project_name": "Backend", "target_name": "Server", "status": "waiting", "pane_tail": "needs input"}}
	m.filter()
	return m
}
func TestDashboardSearchGroupingAndSelectionSurviveRefresh(t *testing.T) {
	m := sampleDashboard()
	m.selected = 1
	chosen := id(m.current())
	m.rows = append(m.rows, row{"id": float64(3), "name": "A new arrival", "project_name": "Backend"})
	m.filter()
	if id(m.current()) != chosen {
		t.Fatal("refresh jumped selection")
	}
	m.query.SetValue("@ apir")
	m.filter()
	if len(m.visible) != 1 || id(m.current()) != "2" {
		t.Fatalf("fuzzy waiting search: %v", m.visible)
	}
	m.query.SetValue("")
	m.attention = true
	m.filter()
	if len(m.visible) != 1 {
		t.Fatal("attention filter")
	}
	m.attention = false
	m.grouping = 1
	m.filter()
	if m.group(m.current()) == "Backend" {
		t.Fatal("target grouping not applied")
	}
}
func TestDashboardIgnoresStaleResponsesAndKeepsRowsOnFailure(t *testing.T) {
	m := sampleDashboard()
	cmd := m.refresh()
	_ = cmd
	m.switchSection(1)
	m.Update(rowsMsg{section: "sessions", generation: 1, rows: []row{{"id": float64(9)}}})
	if len(m.rows) != 0 {
		t.Fatal("stale tab response applied")
	}
	m.Update(rowsMsg{section: "tasks", generation: m.generation, rows: []row{{"id": float64(4), "title": "Keep me"}}})
	m.Update(rowsMsg{section: "tasks", generation: m.generation, err: fmt.Errorf("offline")})
	if len(m.rows) != 1 || !strings.Contains(m.View(), "OFFLINE") {
		t.Fatal("offline state lost last snapshot")
	}
	m.Update(resultMsg{preview: true, key: "sessions/9", label: "Wrong", data: []byte(`{"text":"stale"}`)})
	if strings.Contains(m.preview.View(), "stale") {
		t.Fatal("stale preview overwrote selection")
	}
}
func TestDashboardFitsUnicodeAndUntrustedOutput(t *testing.T) {
	m := sampleDashboard()
	m.rows[0]["name"] = "安全な名前🧪 repeated repeated repeated"
	m.rows[0]["pane_tail"] = "\x1b]52;c;c2VjcmV0\x07\x1b[2Jline\nlong 🧪 日本語 preview"
	m.filter()
	for _, size := range [][2]int{{35, 12}, {60, 18}, {80, 24}, {120, 35}, {180, 45}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		for _, mode := range []string{"list", "help", "menu"} {
			m.help = mode == "help"
			m.menu = mode == "menu"
			v := m.View()
			if strings.Contains(v, "\x1b]52") || strings.Contains(v, "\x1b[2J") {
				t.Fatal("remote control sequence escaped")
			}
			lines := strings.Split(v, "\n")
			if len(lines) > size[1] {
				t.Fatalf("%s at %v: %d lines", mode, size, len(lines))
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("%s at %v: width %d: %q", mode, size, ansi.StringWidth(line), line)
				}
			}
		}
	}
}
func TestDashboardFormsUseNamesPreserveDraftsAndSubmitRealHTTP(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sessions" || r.Method != "POST" {
			t.Errorf("wrong route %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{"id":4}`)
	}))
	defer srv.Close()
	m := sampleDashboard()
	m.client = New(srv.URL, "")
	m.projects = []row{{"id": float64(7), "name": "Named project"}}
	m.targets = []row{{"id": float64(8), "name": "Named target"}}
	m.newForm()
	for i := range m.form.fields {
		f := &m.form.fields[i]
		switch f.Key {
		case "name":
			f.Value = "Test name"
		case "project_id":
			f.Value = "7"
		case "prime":
			f.Value = "first line\nsecond line"
		}
	}
	m.form.index = 1
	m.focusField()
	if !strings.Contains(m.formView(), "Named project") {
		t.Fatal("project name absent")
	}
	m.Update(rowsMsg{section: "sessions", generation: m.generation, rows: m.rows})
	if m.form.fields[0].Value != "Test name" {
		t.Fatal("refresh replaced draft")
	}
	cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyCtrlS})
	msg := cmd()
	m.Update(msg)
	if got["name"] != "Test name" || got["project_id"] != float64(7) || got["target_id"] != nil || got["prime"] != "first line\nsecond line" || got["yolo"] != false {
		t.Fatalf("body: %#v", got)
	}
	if m.form != nil {
		t.Fatal("successful form stayed open")
	}
}
func TestDashboardFailedMutationKeepsDraft(t *testing.T) {
	m := sampleDashboard()
	m.renameForm()
	m.form.editor.SetValue("keep my draft")
	m.saveField()
	m.busy = true
	m.Update(resultMsg{err: fmt.Errorf("connection lost")})
	if m.form == nil || m.form.fields[0].Value != "keep my draft" || m.busy {
		t.Fatal("failed request discarded draft")
	}
}
func TestDashboardTaskCreateDispatchDoesNotDuplicateAfterPartialFailure(t *testing.T) {
	creates, dispatches := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tasks":
			creates++
			fmt.Fprint(w, `{"id":12}`)
		case "/api/tasks/12/dispatch":
			dispatches++
			w.WriteHeader(409)
			fmt.Fprint(w, `{"detail":"target unavailable"}`)
		default:
			t.Error(r.URL.Path)
		}
	}))
	defer srv.Close()
	m := newDashboard(New(srv.URL, ""), nil)
	m.section = 1
	m.openForm("New task", []field{{Key: "title", Label: "Title", Value: "One task"}}, nil)
	cmd := m.createAndDispatch(map[string]any{"title": "One task", "project_id": 7})
	m.Update(cmd())
	if creates != 1 || dispatches != 1 || m.form != nil || !strings.Contains(m.notice, "Created task 12; dispatch failed") {
		t.Fatalf("partial failure: %d %d %s", creates, dispatches, m.notice)
	}
}
func TestDashboardDestructiveActionsRequireConfirmationAndRetainIdentity(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "DELETE" || r.URL.Path != "/api/sessions/2" {
			t.Error(r.URL.Path)
		}
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	m := sampleDashboard()
	m.client = New(srv.URL, "")
	m.current()["origin"] = "discovered"
	actions := m.actions()
	a := actions[len(actions)-1]
	if !strings.Contains(a.Label, "leave running") {
		t.Fatal("adopted session warning wrong")
	}
	if cmd := m.choose(a); cmd != nil || m.pending == nil {
		t.Fatal("deletion bypassed confirmation")
	}
	m.Update(key("n"))
	if requests != 0 || m.pending != nil {
		t.Fatal("cancel sent request")
	}
	m.choose(a)
	m.selected = 1
	_, cmd := m.Update(key("y"))
	m.Update(cmd())
	if requests != 1 {
		t.Fatal("confirmation did not run once")
	}
}
func TestDashboardDiffHistoryAndEmptyResourceViews(t *testing.T) {
	got := formatDetail("Diff", []byte(`{"files":[{"path":"main.go","patch":"@@ -1 +1 @@\n-old\n+new"}]}`))
	if !strings.Contains(got, "\n-old\n+new") {
		t.Fatal("diff rendered as escaped JSON")
	}
	m := newDashboard(New("http://unused", ""), nil)
	m.updated = time.Now()
	m.Update(resultMsg{preview: true, key: m.key(), label: "Usage", data: []byte(`{"total":42}`)})
	if !strings.Contains(m.preview.View(), "42") {
		t.Fatal("global resource hidden on empty list")
	}
}

func TestDashboardProcessesBatchedKeysButDoesNotExecutePastes(t *testing.T) {
	m := sampleDashboard()
	m.Update(key("/repair"))
	if !m.searching || m.query.Value() != "repair" || len(m.visible) != 1 {
		t.Fatal("batched search keys lost")
	}
	m.searching = false
	m.query.Blur()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2n"), Paste: true})
	if m.section != 0 || m.form != nil {
		t.Fatal("paste executed commands")
	}
}

func TestDashboardPreviewDoesNotPadPastTerminalWidth(t *testing.T) {
	m := sampleDashboard()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	if strings.Contains(strings.Join(strings.Split(m.View(), "\n")[4:30], "\n"), "…") {
		t.Fatal("short list and preview unnecessarily truncated")
	}
}

func TestDashboardNativeEditorsCanClearOptionalFields(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()
	m := sampleDashboard()
	m.client = New(srv.URL, "")
	m.projects = []row{{"id": float64(8), "name": "Project"}}
	m.current()["project_id"] = float64(8)
	m.editCommonForm()
	for i := range m.form.fields {
		if m.form.fields[i].Key == "project_id" {
			m.form.fields[i].Value = ""
		}
	}
	m.form.index = 1
	m.focusField()
	cmd := m.updateForm(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.Update(cmd())
	if v, ok := got["project_id"]; !ok || v != nil {
		t.Fatalf("unassign must send explicit null: %v", got)
	}
	m.notificationForm([]byte(`{"discord_webhook":"https://old.example","ntfy_server":"","ntfy_topic":""}`))
	m.form.editor.SetValue("")
	cmd = m.updateForm(tea.KeyMsg{Type: tea.KeyCtrlS})
	m.Update(cmd())
	if got["discord_webhook"] != "" {
		t.Fatal("could not clear webhook")
	}
}
