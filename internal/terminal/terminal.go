// Package terminal is one-click terminal attach: spawn a ttyd on the control
// plane that wraps `tmux attach` (locally, over ssh, or via pct) for a running
// attempt. Ports are ephemeral and --once makes ttyd exit when the client
// disconnects.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/store"
)

// Port range handed out to attached terminals.
const (
	PortLo = 7710
	PortHi = 7730
)

// ErrNoPorts means every terminal port in the range is taken.
var ErrNoPorts = errors.New("no free terminal ports")

// Attachment is whatever you want a terminal on: a task's attempt, or an
// interactive session. Both are just a tmux session on a target, which is why
// one manager serves both.
type Attachment struct {
	// Key is unique across kinds, e.g. "attempt:12" or "session:3", so an
	// attempt and a session can never share a ttyd by accident.
	Key         string
	TmuxSession string
	// SandboxVMID is set when the tmux session lives inside an ephemeral
	// container rather than on the target itself.
	SandboxVMID string
}

// Manager tracks one ttyd per attachment.
type Manager struct {
	mu    sync.Mutex
	procs map[string]*session

	// Spawn is the process launcher. Tests replace it; production shells out.
	Spawn func(port int, argv []string) (*exec.Cmd, error)
	// LookPath reports whether ttyd is installed. Tests override it.
	LookPath func(string) (string, error)
}

type session struct {
	port int
	cmd  *exec.Cmd
}

// NewManager builds a terminal manager wired to the real ttyd binary.
func NewManager() *Manager {
	return &Manager{
		procs:    map[string]*session{},
		LookPath: exec.LookPath,
		Spawn: func(port int, argv []string) (*exec.Cmd, error) {
			args := append([]string{"-p", strconv.Itoa(port), "-W", "--once"}, argv...)
			cmd := exec.Command("ttyd", args...)
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			return cmd, nil
		},
	}
}

// AttachArgv is the command a ttyd wraps, per target kind.
func AttachArgv(a Attachment, target *store.Target) []string {
	sess := a.TmuxSession
	switch {
	case target.Kind == "sandbox" && a.SandboxVMID != "":
		return []string{"sudo", "pct", "exec", a.SandboxVMID, "--", "tmux", "attach", "-t", sess}
	case target.Kind == "pct":
		return []string{"sudo", "pct", "exec", target.Host, "--", "tmux", "attach", "-t", sess}
	case target.Kind == "ssh":
		argv := []string{"ssh", "-tt", "-o", "StrictHostKeyChecking=accept-new"}
		if target.KeyPath != "" {
			argv = append(argv, "-i", target.KeyPath)
		}
		user := target.User
		if user == "" {
			user = "root"
		}
		return append(argv, user+"@"+target.Host, "tmux", "attach", "-t", sess)
	default:
		return []string{"tmux", "attach", "-t", sess}
	}
}

// Attach spawns (or reuses) a ttyd for an attachment and returns its port.
func (m *Manager) Attach(ctx context.Context, a Attachment, target *store.Target) (int, error) {
	m.reap()
	m.mu.Lock()
	if s, ok := m.procs[a.Key]; ok {
		port := s.port
		m.mu.Unlock()
		return port, nil
	}
	m.mu.Unlock()

	if _, err := m.LookPath("ttyd"); err != nil {
		return 0, errors.New("ttyd is not installed on the control plane")
	}
	port, err := m.freePort()
	if err != nil {
		return 0, err
	}
	cmd, err := m.Spawn(port, AttachArgv(a, target))
	if err != nil {
		return 0, fmt.Errorf("ttyd failed to start: %w", err)
	}
	// give it a moment to bind — an immediate exit means the session is gone
	select {
	case <-ctx.Done():
	case <-time.After(300 * time.Millisecond):
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return 0, errors.New("ttyd exited immediately")
	}
	m.mu.Lock()
	m.procs[a.Key] = &session{port: port, cmd: cmd}
	m.mu.Unlock()
	go func() { _ = cmd.Wait() }()
	return port, nil
}

func (m *Manager) reap() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.procs {
		if s.cmd != nil && s.cmd.ProcessState != nil {
			delete(m.procs, id)
		}
	}
}

func (m *Manager) freePort() (int, error) {
	m.mu.Lock()
	used := map[int]bool{}
	for _, s := range m.procs {
		used[s.port] = true
	}
	m.mu.Unlock()
	for port := PortLo; port <= PortHi; port++ {
		if used[port] {
			continue
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
			200*time.Millisecond)
		if err != nil {
			return port, nil // nothing listening: the port is ours
		}
		conn.Close()
	}
	return 0, ErrNoPorts
}

// Shutdown terminates every attached terminal.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	procs := m.procs
	m.procs = map[string]*session{}
	m.mu.Unlock()
	for _, s := range procs {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	}
}
