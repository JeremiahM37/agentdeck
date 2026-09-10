package console

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestReviewIgnoresOldFileAndClosedViewResponses(t *testing.T) {
	m := sampleDashboard()
	m.openReview()
	r := m.review
	m.loadReview("new.txt")
	m.receiveReview(reviewMsg{owner: r, generation: r.generation - 1, data: reviewData{Path: "old.txt", Patch: "wrong"}})
	if r.data.Path != "" {
		t.Fatal("old request replaced current file")
	}
	m.receiveReview(reviewMsg{owner: r, generation: r.generation, data: reviewData{Path: "new.txt", Patch: "+right\x1b]52;c;Y2xpcGJvYXJk\a"}})
	if strings.Contains(r.viewport.View(), "52;") || !strings.Contains(r.viewport.View(), "+right") {
		t.Fatal("terminal escape was not sanitized")
	}
	m.updateReview(tea.KeyMsg{Type: tea.KeyEsc})
	m.openReview()
	m.receiveReview(reviewMsg{owner: r, generation: r.generation, data: reviewData{Path: "old.txt"}})
	if m.review.data.Path != "" {
		t.Fatal("closed review response changed reopened view")
	}
}

func TestReviewReflowsWidePatchAfterTerminalResize(t *testing.T) {
	m := sampleDashboard()
	m.openReview()
	r := m.review
	m.receiveReview(reviewMsg{owner: r, generation: r.generation, data: reviewData{Path: "wide.txt", Patch: "+" + strings.Repeat("界", 90)}})
	m.Update(tea.WindowSizeMsg{Width: 45, Height: 18})
	output := m.View()
	for _, line := range strings.Split(output, "\n") {
		if ansi.StringWidth(line) > 45 {
			t.Fatalf("overlapping review row: %q", line)
		}
	}
	if !strings.Contains(output, "wide.txt") {
		t.Fatal("lost current file")
	}
}

func TestReviewRepositorySwitchDiscardsPriorPatch(t *testing.T) {
	m := sampleDashboard()
	m.openReview()
	r := m.review
	repositories := []reviewRepository{{ID: 0, Name: "Primary"}, {ID: 1, Name: "Second"}}
	m.receiveReview(reviewMsg{owner: r, generation: r.generation, data: reviewData{Repositories: repositories, Path: "file", Patch: "primary"}})
	previous := r.generation
	if m.updateReview(tea.KeyMsg{Type: tea.KeyTab}) == nil || r.repository != 1 {
		t.Fatal("Tab did not request second repository")
	}
	m.receiveReview(reviewMsg{owner: r, generation: previous, data: reviewData{Patch: "late primary"}})
	if !r.loading || r.repository != 1 {
		t.Fatal("late response changed selected repository")
	}
	m.receiveReview(reviewMsg{owner: r, generation: r.generation, data: reviewData{Repositories: repositories, SelectedRepository: 1, Path: "file", Patch: "second"}})
	if !strings.Contains(m.View(), "Second") || !strings.Contains(r.viewport.View(), "second") {
		t.Fatal("selected repository not labeled")
	}
	m.updateReview(tea.KeyMsg{Type: tea.KeyTab})
	if r.repository != 0 {
		t.Fatal("repository cycling did not return to primary")
	}
}
