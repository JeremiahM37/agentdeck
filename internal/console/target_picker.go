package console

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// TargetOption is the small, display-oriented target shape used by the
// command-line target picker.
type TargetOption struct {
	ID   int64
	Name string
	Kind string
}

// PickTarget presents a searchable, keyboard-driven target picker. Callers
// should resolve an explicit name or ID before invoking this for scripts.
func PickTarget(in io.Reader, out io.Writer, targets []TargetOption) (int64, error) {
	if len(targets) == 0 {
		return 0, fmt.Errorf("no machines are configured")
	}
	q := textinput.New()
	q.Prompt = "Machine / "
	q.Placeholder = "type to filter"
	q.CharLimit = 120
	q.Focus()
	m := &targetPicker{all: targets, query: q, cursor: 0}
	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out))
	model, err := p.Run()
	if err != nil {
		return 0, err
	}
	result := model.(targetPicker)
	if result.err != nil {
		return 0, result.err
	}
	if result.selected == 0 {
		return 0, fmt.Errorf("machine selection cancelled")
	}
	return result.selected, nil
}

type targetPicker struct {
	all      []TargetOption
	query    textinput.Model
	cursor   int
	selected int64
	err      error
}

func (m targetPicker) Init() tea.Cmd { return textinput.Blink }

func (m targetPicker) matches() []TargetOption {
	q := strings.ToLower(strings.TrimSpace(m.query.Value()))
	if q == "" {
		return m.all
	}
	out := make([]TargetOption, 0, len(m.all))
	for _, target := range m.all {
		if strings.Contains(strings.ToLower(target.Name), q) || strings.Contains(strings.ToLower(target.Kind), q) {
			out = append(out, target)
		}
	}
	return out
}

func (m targetPicker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.err = fmt.Errorf("machine selection cancelled")
			return m, tea.Quit
		case "up", "ctrl+p":
			matches := m.matches()
			if len(matches) > 0 {
				m.cursor = (m.cursor - 1 + len(matches)) % len(matches)
			}
			return m, nil
		case "down", "ctrl+n":
			matches := m.matches()
			if len(matches) > 0 {
				m.cursor = (m.cursor + 1) % len(matches)
			}
			return m, nil
		case "enter":
			matches := m.matches()
			if len(matches) == 0 {
				return m, nil
			}
			if m.cursor >= len(matches) {
				m.cursor = 0
			}
			m.selected = matches[m.cursor].ID
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.query, cmd = m.query.Update(msg)
	if matches := m.matches(); len(matches) == 0 {
		m.cursor = 0
	} else if m.cursor >= len(matches) {
		m.cursor = len(matches) - 1
	}
	return m, cmd
}

func (m targetPicker) View() string {
	matches := m.matches()
	lines := []string{"Choose a machine (type to filter, Enter select, Esc cancel)", "", m.query.View(), ""}
	const maxVisible = 12
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(matches) {
		end = len(matches)
	}
	for i, target := range matches[start:end] {
		i += start
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%s%-28s %s", prefix, target.Name, target.Kind))
	}
	if start > 0 || end < len(matches) {
		lines = append(lines, fmt.Sprintf("  showing %d-%d of %d", start+1, end, len(matches)))
	}
	if len(matches) == 0 {
		lines = append(lines, "  No matching machines")
	}
	return strings.Join(lines, "\n")
}
