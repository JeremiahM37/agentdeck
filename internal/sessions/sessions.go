// Package sessions is agentdeck's second execution mode: an INTERACTIVE agent
// you work with, rather than a task you hand off.
//
// A task is "go do this, show me the diff". A session is "I am working on this
// project with Claude, for days". Both run in tmux on a target, but their
// lifecycles are opposite: a task's session is created and destroyed around one
// unit of work, while a session outlives many, and the operator attaches to it,
// detaches, checks it from a phone, and comes back.
//
// The durable unit is the PROJECT, not the conversation. A session's tmux
// process is disposable; the row, its wraps, and the project's memory are not —
// which is what makes a six-month project survive the context window that
// happened to be working on it this week.
package sessions

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/JeremiahM37/agentdeck/internal/shellq"
)

// Statuses a session can be in. These are derived from what the pane is
// actually doing, never from what agentdeck asked it to do.
const (
	StatusStarting = "starting" // launched, nothing on the pane yet
	StatusRunning  = "running"  // the agent is working
	StatusWaiting  = "waiting"  // sitting at its prompt, waiting for you
	StatusIdle     = "idle"     // quiet, but not obviously at a prompt
	StatusDead     = "dead"     // the tmux session is gone
)

// Agents that can be run interactively. Unlike a dispatched task, an interactive
// session has a human at the keyboard, so there is no permission mode to resolve
// and no gated-approval requirement — the operator IS the gate.
var InteractiveAgents = []string{"claude", "codex", "gemini"}

// Launcher builds the shell commands that drive interactive sessions.
type Launcher struct {
	ClaudeBin string
	CodexBin  string
	GeminiBin string
}

func (l Launcher) bin(agent string) string {
	switch agent {
	case "codex":
		return orDefault(l.CodexBin, "codex")
	case "gemini":
		return orDefault(l.GeminiBin, "gemini")
	default:
		return orDefault(l.ClaudeBin, "claude")
	}
}

// LaunchCommand starts an interactive agent in a detached tmux session.
//
// `exec bash` on the end is deliberate: when the agent exits, the pane stays
// alive at a shell instead of the tmux session vanishing. That keeps the window
// (and anything you were about to read in the scrollback) around until you
// dismiss it yourself.
func (l Launcher) LaunchCommand(agent, workdir, tmuxName, model string, resume bool) string {
	parts := []string{l.bin(agent)}
	switch agent {
	case "codex":
		if resume {
			parts = append(parts, "resume", "--last")
		}
		if model != "" {
			parts = append(parts, "-m", model)
		}
	case "gemini":
		if model != "" {
			parts = append(parts, "-m", model)
		}
	default:
		if resume {
			parts = append(parts, "--continue")
		}
		if model != "" {
			parts = append(parts, "--model", model)
		}
	}
	inner := fmt.Sprintf("cd %s && %s; exec bash",
		shellq.Quote(workdir), strings.Join(parts, " "))
	return fmt.Sprintf("tmux new-session -d -s %s %s",
		shellq.Quote(tmuxName), shellq.Quote(inner))
}

// PaneLines is how much scrollback a poll pulls back. Enough to read the current
// state and show a preview, small enough to poll every couple of seconds.
const PaneLines = 40

// PollDelimiter separates one session's capture from the next in a batched poll.
//
// RECORD SEPARATOR (0x1E), deliberately not NUL: these delimiters travel inside
// a shell command line, and exec() truncates argv at the first NUL byte — a NUL
// delimiter silently cut the command in half on a real target while every mock
// test passed.
const PollDelimiter = "\x1e---AGENTDECK-PANE---\x1e"

// PollCommand captures every named session's pane in ONE command.
//
// One exec per target per tick, not one per session: over SSH the round trip
// dominates, and a board with a dozen live sessions would otherwise spend the
// whole tick opening channels.
func PollCommand(names []string) string {
	var b strings.Builder
	for i, name := range names {
		if i > 0 {
			b.WriteString("; ")
		}
		q := shellq.Quote(name)
		fmt.Fprintf(&b, "printf '%%s' %s; ", shellq.Quote(PollDelimiter+name+"\n"))
		// a missing session prints nothing and is reported as dead by absence
		fmt.Fprintf(&b, "tmux capture-pane -p -t %s -S -%d 2>/dev/null || true", q, PaneLines)
	}
	return b.String()
}

// ParsePoll splits a batched capture back into per-session pane text.
func ParsePoll(out string) map[string]string {
	panes := map[string]string{}
	for _, chunk := range strings.Split(out, PollDelimiter) {
		if chunk == "" {
			continue
		}
		name, body, found := strings.Cut(chunk, "\n")
		if !found {
			continue
		}
		panes[strings.TrimSpace(name)] = body
	}
	return panes
}

// Hash fingerprints a pane so a poll can tell "changed" from "quiet" without
// storing the whole capture.
func Hash(pane string) string {
	sum := sha256.Sum256([]byte(strings.TrimRight(pane, " \n\t")))
	return hex.EncodeToString(sum[:8])
}

// busyMarkers are what an agent prints while it is actually working. Claude Code
// and codex both offer an interrupt hint while a turn is in flight, and that is
// a far better signal than guessing from output volume.
var busyMarkers = []string{"esc to interrupt", "ctrl+c to interrupt", "Esc to interrupt"}

// promptMarkers are what an agent's input box looks like when it is waiting for
// you. These are cosmetic and WILL drift with CLI releases — which is why they
// only ever refine the answer, and `last_activity_at` (below) is what the UI
// leans on.
var promptMarkers = []string{"❯", "│ >", "> ", "Press up to edit"}

var contextRe = regexp.MustCompile(`[Cc]ontext (?:left )?(?:until auto-compact|remaining)?:?\s*(\d{1,3})%`)

// DeriveStatus decides what a session is doing from its pane.
//
// The order matters: an explicit "working" marker beats everything, then actual
// change since the last poll, then the shape of the prompt. Anything we cannot
// justify is `idle` rather than a confident guess.
func DeriveStatus(pane, prevHash string) string {
	if strings.TrimSpace(pane) == "" {
		return StatusStarting
	}
	tail := lastLines(pane, 12)
	for _, m := range busyMarkers {
		if strings.Contains(tail, m) {
			return StatusRunning
		}
	}
	if prevHash != "" && Hash(pane) != prevHash {
		return StatusRunning // the pane moved, whatever it did not say
	}
	for _, m := range promptMarkers {
		if strings.Contains(tail, m) {
			return StatusWaiting
		}
	}
	return StatusIdle
}

// ContextPct reads the agent's own context gauge off the pane when it shows one.
// Absent is the normal case (agents only surface it when it starts to matter),
// so this returns nil rather than a fabricated number.
func ContextPct(pane string) *int {
	m := contextRe.FindStringSubmatch(pane)
	if m == nil {
		return nil
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n < 0 || n > 100 {
		return nil
	}
	return &n
}

// Preview is the tail of a pane, cleaned up for a card in the UI.
func Preview(pane string, lines int) string {
	var kept []string
	for _, line := range strings.Split(pane, "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) > lines {
		kept = kept[len(kept)-lines:]
	}
	return strings.Join(kept, "\n")
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ---- driving a session ------------------------------------------------------

// SendTextCommand types a message into a session and submits it.
//
// It goes through a tmux buffer rather than `send-keys -l` so the text survives
// verbatim: newlines, quotes and unicode all reach the agent as one paste
// instead of being re-interpreted as shell syntax or as separate submissions.
func SendTextCommand(tmuxName, stagePath string) string {
	q, p := shellq.Quote(tmuxName), shellq.Quote(stagePath)
	return fmt.Sprintf("tmux load-buffer -b agentdeck %s && "+
		"tmux paste-buffer -b agentdeck -t %s -d && "+
		"tmux send-keys -t %s Enter && rm -f %s", p, q, q, p)
}

// Keys the operator may send. An allowlist, because this is a raw input channel
// into a process with the operator's own permissions.
var Keys = map[string]string{
	"enter":  "Enter",
	"escape": "Escape", // how you interrupt Claude Code mid-turn
	"ctrl-c": "C-c",
	"up":     "Up",
	"tab":    "Tab",
}

// SendKeyCommand presses one named key in a session.
func SendKeyCommand(tmuxName, key string) (string, bool) {
	code, ok := Keys[key]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("tmux send-keys -t %s %s", shellq.Quote(tmuxName), code), true
}

// KillCommand ends a session's tmux process.
func KillCommand(tmuxName string) string {
	return fmt.Sprintf("tmux kill-session -t %s 2>/dev/null || true", shellq.Quote(tmuxName))
}

// HasSessionCommand asks whether a tmux session still exists.
func HasSessionCommand(tmuxName string) string {
	return fmt.Sprintf("tmux has-session -t %s 2>/dev/null", shellq.Quote(tmuxName))
}

// TimesCommand asks tmux when a session started and when it last did anything.
//
// Adoption needs both: an agent you started three days ago should say "up 3d",
// not "up 4s", and its idle clock should start from tmux's own activity stamp
// rather than from the moment agentdeck happened to notice it. It doubles as the
// existence check, since it fails on a session that is not there.
func TimesCommand(tmuxName string) string {
	return fmt.Sprintf("tmux display-message -p -t %s '#{session_created} #{session_activity}'",
		shellq.Quote(tmuxName))
}

// ParseTimes reads the epoch pair TimesCommand prints.
func ParseTimes(out string) (created, activity float64, ok bool) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 2 {
		return 0, 0, false
	}
	c, err1 := strconv.ParseFloat(fields[0], 64)
	a, err2 := strconv.ParseFloat(fields[1], 64)
	if err1 != nil || err2 != nil || c <= 0 {
		return 0, 0, false
	}
	return c, a, true
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
