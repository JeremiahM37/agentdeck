// Package state is the task state machine — the single source of truth for
// which board moves are legal.
package state

import "fmt"

// Statuses are the board columns, in lifecycle order.
var Statuses = []string{"backlog", "queued", "running", "review", "done", "failed", "cancelled"}

// Transitions maps a status to everything it may become.
var Transitions = map[string]map[string]bool{
	"backlog":   {"queued": true, "cancelled": true},
	"queued":    {"running": true, "cancelled": true, "failed": true, "backlog": true},
	"running":   {"review": true, "failed": true, "cancelled": true},
	"review":    {"done": true, "queued": true, "cancelled": true}, // queued = follow-up attempt
	"failed":    {"queued": true, "backlog": true, "cancelled": true},
	"done":      {"queued": true}, // an explicit message reopens completed work
	"cancelled": {"backlog": true, "queued": true},
}

// IllegalTransition is returned for any move the machine forbids.
type IllegalTransition struct{ Msg string }

func (e *IllegalTransition) Error() string { return e.Msg }

// Check reports whether old -> new is allowed. Staying put is always fine.
func Check(old, new string) error {
	known := false
	for _, s := range Statuses {
		if s == new {
			known = true
			break
		}
	}
	if !known {
		return &IllegalTransition{fmt.Sprintf("unknown status %q", new)}
	}
	if new == old {
		return nil
	}
	if !Transitions[old][new] {
		return &IllegalTransition{fmt.Sprintf("%s → %s not allowed", old, new)}
	}
	return nil
}
