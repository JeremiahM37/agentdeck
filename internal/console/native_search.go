package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type nativeSearchHit struct {
	ID, Title, Target, Agent, Cwd, Snippet string
}
type nativeSearchScope struct {
	Target, Agent, State, Error string
	More                        bool
	Progress                    struct {
		Documents int
		Pending   int `json:"pending_files"`
		Oversized int `json:"oversized_entries"`
		Issues    []string
	}
}
type nativeSearchData struct {
	ID             string
	Done, Complete bool
	Results        []nativeSearchHit
	Scopes         []nativeSearchScope
}
type nativeSearchState struct {
	data                                        nativeSearchData
	query, agent, target, failure, mode, reader string
	selected, readGeneration, generation        int
	starting                                    bool
	viewport                                    viewport.Model
}
type nativeSearchMsg struct {
	owner      *nativeSearchState
	generation int
	started    bool
	data       nativeSearchData
	err        error
}
type nativeSearchTick struct {
	owner      *nativeSearchState
	generation int
}
type nativeSearchReadMsg struct {
	owner      *nativeSearchState
	generation int
	data       []byte
	err        error
}

func (m *dashboard) nativeSearchForm() tea.Cmd {
	targets := []choice{{"All targets", ""}}
	for _, r := range m.targets {
		targets = append(targets, choice{name(r), id(r)})
	}
	return m.openForm("Search saved conversations", []field{
		{Key: "query", Label: "Conversation text", Required: true},
		{Key: "target", Label: "Target", Options: targets},
		{Key: "agent", Label: "Agent", Options: []choice{{"Claude and Codex", ""}, {"Claude", "claude"}, {"Codex", "codex"}}},
	}, func(values map[string]any) tea.Cmd {
		query := strings.TrimSpace(str(values["query"]))
		if len(query) > 500 {
			m.notice = "Search query must be at most 500 characters"
			return nil
		}
		m.form = nil
		s := &nativeSearchState{query: query, target: str(values["target"]), agent: str(values["agent"]), viewport: viewport.New(max(1, m.width-4), max(1, m.height-7))}
		m.nativeSearch = s
		return m.startNativeSearch(false)
	})
}
func (m *dashboard) startNativeSearch(reset bool) tea.Cmd {
	s := m.nativeSearch
	if s == nil || s.starting {
		return nil
	}
	s.starting = true
	s.failure = ""
	s.generation++
	generation := s.generation
	c := m.client
	previous := s.data.ID
	body := map[string]any{"query": s.query, "agent": s.agent, "reset": reset}
	if target, err := strconv.ParseInt(s.target, 10, 64); err == nil {
		body["target_id"] = target
	}
	return func() tea.Msg {
		if previous != "" {
			if _, err := c.JSON("DELETE", "/conversation-search/"+url.PathEscape(previous), nil); err != nil {
				var status *HTTPError
				if !errors.As(err, &status) || status.Status != 404 {
					return nativeSearchMsg{owner: s, generation: generation, err: err}
				}
			}
		}
		b, err := c.JSON("POST", "/conversation-search", body)
		var data nativeSearchData
		if err == nil {
			err = json.Unmarshal(b, &data)
		}
		return nativeSearchMsg{owner: s, generation: generation, started: true, data: data, err: err}
	}
}
func (m *dashboard) cancelNativeSearch(s *nativeSearchState) tea.Cmd {
	c := *m.client
	h := http.Client{}
	if c.HTTP != nil {
		h = *c.HTTP
	}
	h.Timeout = 3 * time.Second
	c.HTTP = &h
	id := s.data.ID
	if id == "" {
		return nil
	}
	s.generation++
	generation := s.generation
	return func() tea.Msg {
		b, err := c.JSON("DELETE", "/conversation-search/"+url.PathEscape(id), nil)
		var data nativeSearchData
		if err == nil {
			err = json.Unmarshal(b, &data)
		}
		return nativeSearchMsg{owner: s, generation: generation, data: data, err: err}
	}
}
func (m *dashboard) pollNativeSearch(s *nativeSearchState) tea.Cmd {
	c := m.client
	id := s.data.ID
	generation := s.generation
	return func() tea.Msg {
		b, err := c.JSON("GET", "/conversation-search/"+url.PathEscape(id), nil)
		var data nativeSearchData
		if err == nil {
			err = json.Unmarshal(b, &data)
		}
		return nativeSearchMsg{owner: s, generation: generation, data: data, err: err}
	}
}
func (m *dashboard) receiveNativeSearch(v nativeSearchMsg) tea.Cmd {
	s := m.nativeSearch
	if s != v.owner || s.generation != v.generation {
		if v.started && v.err == nil && v.data.ID != "" && !v.data.Done {
			return m.cancelNativeSearch(&nativeSearchState{data: v.data})
		}
		return nil
	}
	s.starting = false
	if v.err != nil {
		s.failure = clean(v.err.Error())
		return nil
	}
	previousOffset := s.viewport.YOffset
	preserveOffset := len(s.data.Results) > 0
	selected := ""
	if s.selected < len(s.data.Results) {
		selected = s.data.Results[s.selected].ID
	}
	s.data = v.data
	s.failure = ""
	s.selected = min(s.selected, max(0, len(s.data.Results)-1))
	for i, hit := range s.data.Results {
		if hit.ID == selected {
			s.selected = i
			break
		}
	}
	m.renderNativeSearch()
	if preserveOffset {
		s.viewport.SetYOffset(previousOffset)
	}
	if !s.data.Done {
		generation := s.generation
		return tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg { return nativeSearchTick{s, generation} })
	}
	return nil
}
func (m *dashboard) readNativeSearch() tea.Cmd {
	s := m.nativeSearch
	if len(s.data.Results) == 0 {
		return nil
	}
	hit := s.data.Results[s.selected]
	s.mode = "reader"
	s.reader = "Loading matching message…"
	s.readGeneration++
	generation := s.readGeneration
	m.renderNativeSearch()
	c := m.client
	path := "/conversation-search/" + url.PathEscape(s.data.ID) + "/results/" + url.PathEscape(hit.ID)
	return func() tea.Msg { b, err := c.JSON("GET", path, nil); return nativeSearchReadMsg{s, generation, b, err} }
}
func (m *dashboard) receiveNativeSearchRead(v nativeSearchReadMsg) {
	s := m.nativeSearch
	if s != v.owner || s.readGeneration != v.generation || s.mode != "reader" {
		return
	}
	if v.err != nil {
		s.reader = "Could not read match: " + clean(v.err.Error()) + "\nEsc returns to results; r retries the search."
	} else {
		var page struct {
			Messages []struct {
				Role, Text         string
				Matched, Truncated bool
			}
			Changed  int  `json:"changed_neighbors"`
			Complete bool `json:"index_complete"`
		}
		if err := json.Unmarshal(v.data, &page); err != nil {
			s.reader = "Could not parse matching context"
		} else {
			var lines []string
			for _, message := range page.Messages {
				title := strings.ToUpper(message.Role)
				if message.Matched {
					title += " · MATCHING MESSAGE"
				}
				text := title + "\n" + message.Text
				if message.Truncated {
					text += "\n[Long message shortened]"
				}
				lines = append(lines, text)
			}
			if page.Changed > 0 {
				lines = append(lines, fmt.Sprintf("%d changed neighboring messages omitted", page.Changed))
			}
			if !page.Complete {
				lines = append(lines, "Indexing is incomplete; this is the available context")
			}
			s.reader = strings.Join(lines, "\n\n")
		}
	}
	m.renderNativeSearch()
	s.viewport.GotoTop()
	// Center the matching heading even when the earlier context is long.
	for i, line := range strings.Split(ansi.Wrap(clean(s.reader), max(1, m.width-4), ""), "\n") {
		if strings.Contains(line, "· MATCHING MESSAGE") {
			s.viewport.SetYOffset(max(0, i-2))
			break
		}
	}
}
func (m *dashboard) renderNativeSearch() {
	s := m.nativeSearch
	if s == nil {
		return
	}
	s.viewport.Width = max(1, m.width-4)
	s.viewport.Height = max(1, m.height-4-len(strings.Split(ansi.Wrap(nativeSearchHelp, max(1, m.width-2), ""), "\n")))
	if s.mode == "reader" {
		s.viewport.SetContent(ansi.Wrap(clean(s.reader), s.viewport.Width, ""))
		return
	}
	var lines []string
	selectedStart := 0
	if s.mode == "progress" {
		for _, scope := range s.data.Scopes {
			lines = append(lines, ansi.Wrap(clean(fmt.Sprintf("%s · %s · %s\n%d conversations · %d pending · %d oversized\n%s %s", scope.Target, scope.Agent, scope.State, scope.Progress.Documents, scope.Progress.Pending, scope.Progress.Oversized, scope.Error, strings.Join(scope.Progress.Issues, "; "))), s.viewport.Width, ""))
		}
	} else {
		for i, hit := range s.data.Results {
			prefix := "  "
			if i == s.selected {
				prefix = "› "
				selectedStart = len(strings.Split(strings.Join(lines, "\n\n"), "\n"))
				if len(lines) > 0 {
					selectedStart++
				} else {
					selectedStart = 0
				}
			}
			snippet := []rune(clean(hit.Snippet))
			if len(snippet) > 300 {
				snippet = append(snippet[:300], '…')
			}
			text := ansi.Wrap(clean(prefix+hit.Title+"\n"+hit.Target+" · "+hit.Agent+" · "+hit.Cwd+"\n"+string(snippet)), s.viewport.Width, "")
			if i == s.selected {
				text = accent.Render(text)
			}
			lines = append(lines, text)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "No matches yet. Search includes saved histories outside tracked workspaces.")
	}
	s.viewport.SetContent(strings.Join(lines, "\n\n"))
	if s.mode == "" && (selectedStart < s.viewport.YOffset || selectedStart >= s.viewport.YOffset+s.viewport.Height-2) {
		s.viewport.SetYOffset(selectedStart)
	}
}
func (m *dashboard) updateNativeSearch(k tea.KeyMsg) tea.Cmd {
	s := m.nativeSearch
	switch k.String() {
	case "ctrl+c", "q":
		return tea.Sequence(m.cancelNativeSearch(s), tea.Quit)
	case "esc":
		if s.mode != "" {
			s.mode = ""
			s.readGeneration++
			m.renderNativeSearch()
			return nil
		}
		m.nativeSearch = nil
		return m.cancelNativeSearch(s)
	case "n":
		if s.starting {
			return nil
		}
		m.nativeSearch = nil
		return tea.Batch(m.cancelNativeSearch(s), m.nativeSearchForm())
	case "r":
		if s.starting {
			return nil
		}
		s.mode = ""
		return m.startNativeSearch(false)
	case "R":
		if s.starting {
			return nil
		}
		s.mode = ""
		return m.startNativeSearch(true)
	case "s":
		if s.starting {
			return nil
		}
		return m.cancelNativeSearch(s)
	case "p":
		if s.mode == "progress" {
			s.mode = ""
		} else {
			s.mode = "progress"
		}
		m.renderNativeSearch()
		s.viewport.GotoTop()
		return nil
	case "enter":
		if s.mode == "" && !s.starting {
			return m.readNativeSearch()
		}
	case "up", "k":
		if s.mode == "" {
			s.selected = max(0, s.selected-1)
			m.renderNativeSearch()
			return nil
		}
	case "down", "j":
		if s.mode == "" {
			s.selected = min(max(0, len(s.data.Results)-1), s.selected+1)
			m.renderNativeSearch()
			return nil
		}
	}
	var cmd tea.Cmd
	s.viewport, cmd = s.viewport.Update(k)
	return cmd
}

const nativeSearchHelp = "↑↓ choose/scroll · Enter read · p progress · s stop · r retry · R rebuild · n new · Esc back · q quit"

func (m *dashboard) nativeSearchView() string {
	if m.width < 35 || m.height < 12 {
		return "Search saved conversations\nResize to at least 35 × 12.\nEsc back · q quit"
	}
	s := m.nativeSearch
	state := "Searching…"
	if s.data.Done {
		state = "Search complete"
		if !s.data.Complete {
			state = "Search finished with incomplete profiles; p shows details"
		}
	}
	if s.starting {
		state = "Starting search…"
	}
	if s.failure != "" {
		state = s.failure + " · r retries"
	}
	noun := "conversations"
	if len(s.data.Results) == 1 {
		noun = "conversation"
	}
	summary := fmt.Sprintf("%d %s · %s", len(s.data.Results), noun, state)
	for _, scope := range s.data.Scopes {
		if scope.More {
			summary += " · narrow query for more matches"
			break
		}
	}
	return accent.Bold(true).Render(" Search saved conversations") + "\n" + ansi.Truncate(oneLine(s.query), max(1, m.width-2), "…") + "\n" + ansi.Truncate(oneLine(summary), max(1, m.width-2), "…") + "\n\n" + s.viewport.View() + "\n" + ansi.Wrap(nativeSearchHelp, max(1, m.width-2), "")
}
