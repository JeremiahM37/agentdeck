package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLaunchProfilesAvailableWithoutSessions(t *testing.T) {
	m := sampleDashboard()
	m.rows, m.visible = nil, nil
	if actions := m.actions(); len(actions) != 2 || actions[0].Operation != "launch-profiles" || actions[1].Operation != "recent-sessions" {
		t.Fatal("empty session dashboards should keep profile and recently closed actions")
	}
	m.Update(key("P"))
	if m.form == nil || m.form.fields[0].Value != "new" {
		t.Fatal("P did not open profile management")
	}
}

func TestSessionFormUsesSelectedProfileAgentAndKeepsDraftOnError(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/sessions" {
			t.Error(r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(409)
		w.Write([]byte(`{"detail":"profile temporarily unavailable"}`))
	}))
	defer srv.Close()
	m := sampleDashboard()
	m.client = New(srv.URL, "")
	m.profiles = []row{{"id": float64(7), "name": "Work account", "agent": "codex"}}
	m.newForm()
	for i := range m.form.fields {
		switch m.form.fields[i].Key {
		case "name":
			m.form.fields[i].Value = "keep profile draft"
		case "profile_id":
			m.form.fields[i].Value = "7"
		case "agent":
			m.form.fields[i].Value = "claude"
		}
	}
	body, err := formBody(m.form.fields)
	if err != nil {
		t.Fatal(err)
	}
	cmd := m.form.submit(body)
	m.Update(cmd())
	if received["profile_id"] != float64(7) || received["agent"] != nil {
		t.Fatal("profile selection sent a conflicting agent", received)
	}
	if m.form == nil || m.form.fields[0].Value != "keep profile draft" {
		t.Fatal("launch failure lost the profile draft")
	}
}
