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
