package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func searchDashboard() *dashboard {
	m := sampleDashboard()
	m.width = 80
	m.height = 24
	m.nativeSearch = &nativeSearchState{query: "needle", viewport: viewport.New(76, 17)}
	return m
}
func TestNativeSearchSelectionAndStaleResponses(t *testing.T) {
	m := searchDashboard()
	s := m.nativeSearch
	s.generation = 2
	m.receiveNativeSearch(nativeSearchMsg{owner: s, generation: 1, data: nativeSearchData{ID: "wrong"}})
	if s.data.ID != "" {
		t.Fatal("stale response applied")
	}
	hits := []nativeSearchHit{{ID: "a", Title: "First"}, {ID: "b", Title: "Second"}}
	m.receiveNativeSearch(nativeSearchMsg{owner: s, generation: 2, data: nativeSearchData{ID: "job", Done: true, Complete: true, Results: hits}})
	m.updateNativeSearch(tea.KeyMsg{Type: tea.KeyDown})
	m.receiveNativeSearch(nativeSearchMsg{owner: s, generation: 2, data: nativeSearchData{ID: "job", Done: true, Results: []nativeSearchHit{hits[1], hits[0]}}})
	if s.selected != 0 || s.data.Results[s.selected].ID != "b" {
		t.Fatal("selection moved on refresh")
	}
	s.mode = "reader"
	s.readGeneration = 3
	s.reader = "current"
	m.receiveNativeSearchRead(nativeSearchReadMsg{owner: s, generation: 2, data: []byte(`{"messages":[{"text":"wrong"}]}`)})
	if s.reader != "current" {
		t.Fatal("stale reader applied")
	}
}
func TestNativeSearchLateStartCancelsOnlyItsOwnJob(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewEncoder(w).Encode(nativeSearchData{ID: "old", Done: true})
	}))
	defer server.Close()
	m := searchDashboard()
	m.client = New(server.URL, "")
	s := m.nativeSearch
	s.generation = 5
	s.data.ID = "new"
	cmd := m.receiveNativeSearch(nativeSearchMsg{owner: s, generation: 4, started: true, data: nativeSearchData{ID: "old"}})
	if s.data.ID != "new" {
		t.Fatal("late start replaced active job")
	}
	m.Update(cmd())
	if path != "/api/conversation-search/old" || s.data.ID != "new" {
		t.Fatal("wrong job canceled", path)
	}
}
func TestNativeSearchSanitizesAndReflowsReader(t *testing.T) {
	m := searchDashboard()
	s := m.nativeSearch
	s.mode = "reader"
	s.readGeneration = 1
	data, _ := json.Marshal(map[string]any{"index_complete": true, "messages": []map[string]any{{"role": "assistant", "text": strings.Repeat("earlier text ", 100)}, {"role": "assistant", "text": "needle 界\x1b]52;c;evil\a" + strings.Repeat("界", 60), "matched": true}}})
	m.receiveNativeSearchRead(nativeSearchReadMsg{owner: s, generation: 1, data: data})
	for _, width := range []int{35, 45, 120} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 18})
		out := m.View()
		if strings.Contains(out, "52;") {
			t.Fatal("terminal control leaked")
		}
		for _, line := range strings.Split(out, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("overflow width%d: %q", width, line)
			}
		}
		if len(strings.Split(out, "\n")) > 18 {
			t.Fatalf("height overflow: %s", out)
		}
	}
}

func TestNativeSearchRetryAfterJobExpires(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method)
		if r.Method == "DELETE" {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"search expired"}`))
			return
		}
		json.NewEncoder(w).Encode(nativeSearchData{ID: "new", Done: true, Complete: true})
	}))
	defer server.Close()
	m := searchDashboard()
	m.client = New(server.URL, "")
	m.nativeSearch.data.ID = "expired"
	msg := m.startNativeSearch(false)().(nativeSearchMsg)
	m.receiveNativeSearch(msg)
	if msg.err != nil || m.nativeSearch.data.ID != "new" || strings.Join(calls, ",") != "DELETE,POST" {
		t.Fatalf("expired search blocked retry: %#v %v", msg, calls)
	}
}

func TestNativeSearchMouseAndProgressPreserveReadingPosition(t *testing.T) {
	m := searchDashboard()
	s := m.nativeSearch
	results := []nativeSearchHit{}
	for i := 0; i < 20; i++ {
		results = append(results, nativeSearchHit{ID: strings.Repeat("x", i+1), Title: "Conversation", Snippet: strings.Repeat("text ", 30)})
	}
	m.receiveNativeSearch(nativeSearchMsg{owner: s, data: nativeSearchData{ID: "job", Done: true, Results: results}})
	before := m.selected
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	offset := s.viewport.YOffset
	if offset == 0 {
		t.Fatal("mouse did not scroll search")
	}
	m.receiveNativeSearch(nativeSearchMsg{owner: s, data: nativeSearchData{ID: "job", Done: true, Results: results}})
	if s.viewport.YOffset != offset || m.selected != before {
		t.Fatal("progress or mouse moved the dashboard/reading position")
	}
}

func TestNativeSearchPagesKeepOriginalConversation(t *testing.T) {
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		w.Write([]byte(`{"page_mode":"match","after":22,"index_complete":true,"messages":[{"role":"assistant","text":"needle","matched":true}]}`))
	}))
	defer server.Close()
	m := searchDashboard()
	m.client = New(server.URL, "")
	s := m.nativeSearch
	s.data = nativeSearchData{ID: "job", Results: []nativeSearchHit{{ID: "original"}}}
	m.Update(m.readNativeSearch()())
	s.data.Results = []nativeSearchHit{{ID: "another"}}
	cmd := m.updateNativeSearch(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	m.Update(cmd())
	if len(paths) != 2 || paths[1] != "/api/conversation-search/job/results/original?after=22" {
		t.Fatal("paging changed conversation", paths)
	}
}

func TestSearchShortcutKeepsQuitHintVisible(t *testing.T) {
	m := sampleDashboard()
	for _, width := range []int{45, 80, 120} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		if !strings.Contains(m.View(), "q quit") {
			t.Fatalf("quit hint clipped at %d columns", width)
		}
	}
}
