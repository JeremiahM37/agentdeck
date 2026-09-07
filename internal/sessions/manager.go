package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/bus"
	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/memory"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

// Manager owns the lifecycle of every interactive session.
type Manager struct {
	DB       *store.DB
	Reg      *executor.Registry
	Bus      *bus.Bus
	Launcher Launcher
	// Specs resolves an agent name to how it is launched. Injected so the set is
	// the operator's, not a constant in this package.
	Specs  func() []Spec
	Memory memory.Provider
	Log    *slog.Logger

	// HandoffTimeout bounds how long we wait for an agent to write its wrap
	// before giving up and saying so.
	HandoffTimeout time.Duration

	mu       sync.Mutex
	handoffs map[int64]bool // sessions with a wrap in flight
}

// New builds a session manager.
func New(db *store.DB, reg *executor.Registry, b *bus.Bus, l Launcher,
	mem memory.Provider, log *slog.Logger) *Manager {
	if mem == nil {
		mem = memory.None{}
	}
	return &Manager{DB: db, Reg: reg, Bus: b, Launcher: l, Memory: mem, Log: log,
		HandoffTimeout: 4 * time.Minute, handoffs: map[int64]bool{}}
}

func (m *Manager) publish(s *store.Session) {
	m.Bus.Publish("board", "session", s)
	m.Bus.Publish(fmt.Sprintf("session:%d", s.ID), "session", s)
}

// ---- launching ---------------------------------------------------------------

// LaunchOpts are the inputs of a new interactive session.
type LaunchOpts struct {
	ProjectID *int64
	TargetID  int64
	Name      string
	Agent     string
	Model     string
	Workdir   string
	// Resume asks the agent to pick up its own previous conversation
	// (`claude --continue`), which is what you want when re-opening a project
	// you were in yesterday.
	Resume bool
	// Env is layered over the agent's and under nothing: it is how a session is
	// pointed at a local model (ANTHROPIC_BASE_URL, OPENAI_BASE_URL, …).
	Env map[string]string
	// Prime is typed into the session once it is up — a project briefing, or a
	// predecessor's handoff.
	Prime string
	// Yolo runs the agent without approval prompts. Defaulted on by the API for
	// interactive sessions — see Start.Yolo.
	Yolo bool
	// Scratch asks for a throwaway working directory on the target instead of a
	// project's repository: an empty room to think in. The directory is a real
	// git repository, so whatever the work turns into can later be promoted to a
	// project and dispatched against without moving anything.
	Scratch bool
}

// Launch starts an interactive agent and records it.
func (m *Manager) Launch(ctx context.Context, o LaunchOpts) (*store.Session, error) {
	target, err := m.DB.Target(o.TargetID)
	if err != nil {
		return nil, err
	}
	workdir := o.Workdir
	if workdir == "" && o.ProjectID != nil {
		proj, err := m.DB.Project(*o.ProjectID)
		if err != nil {
			return nil, err
		}
		workdir = proj.RepoPath
	}
	agent := o.Agent
	if agent == "" {
		agent = "claude"
	}
	if workdir == "" && o.Scratch {
		ex, err := m.Reg.For(target)
		if err != nil {
			return nil, err
		}
		workdir, err = m.makeScratch(ctx, ex, firstNonEmpty(o.Name, agent))
		if err != nil {
			return nil, err
		}
	}
	if workdir == "" {
		return nil, fmt.Errorf("a session needs a working directory")
	}
	name := o.Name
	if name == "" {
		name = agent
	}
	sess, err := m.DB.InsertSession(&store.Session{
		ProjectID: o.ProjectID, TargetID: o.TargetID, Name: name, Agent: agent,
		Model: o.Model, Workdir: workdir, Status: StatusStarting, Origin: "agentdeck",
	})
	if err != nil {
		return nil, err
	}
	// the id is only known after the insert, so the tmux name is set here — and
	// the `adk-s` prefix keeps interactive sessions clearly apart from the
	// `adk-<attempt>` sessions a dispatched task owns
	tmuxName := fmt.Sprintf("adk-s%d", sess.ID)
	if err := m.DB.Update("sessions", sess.ID, map[string]any{
		"tmux_session": tmuxName}); err != nil {
		return nil, err
	}
	sess.TmuxSession = tmuxName

	ex, err := m.Reg.For(target)
	if err != nil {
		return nil, err
	}
	spec, ok := Find(m.specs(), agent)
	if !ok {
		m.end(sess.ID, "dead")
		return nil, fmt.Errorf("unknown agent %q", agent)
	}
	spec = m.Launcher.resolve(spec)
	// resuming replays a conversation, and the CLIs do not accept an opening
	// message alongside that — so a prime on a resumed session still has to be
	// typed in once it is up
	argPrompt := ""
	if o.Prime != "" && !o.Resume && spec.PromptArg {
		argPrompt = o.Prime
	}
	// agent-wide env first, then the project's, so a project can point one agent
	// at a different endpoint without redefining the agent
	env := map[string]string{}
	for k, v := range spec.Env {
		env[k] = v
	}
	for k, v := range o.Env {
		env[k] = v
	}
	envPrefix, err := EnvPrefix(env)
	if err != nil {
		m.end(sess.ID, "dead")
		return nil, err
	}
	cmd := spec.LaunchCommand(Start{
		Workdir: workdir, TmuxName: tmuxName, Model: o.Model, Resume: o.Resume,
		Prompt: argPrompt, EnvPrefix: envPrefix, Yolo: o.Yolo})
	r, err := ex.Run(ctx, cmd, executor.RunOpts{Timeout: 60})
	if err != nil {
		m.end(sess.ID, "dead")
		return nil, err
	}
	if !r.OK() {
		m.end(sess.ID, "dead")
		return nil, executor.Errf("tmux launch failed: %s", strings.TrimSpace(r.Stderr))
	}
	if o.Prime != "" && argPrompt == "" {
		// the fallback path: wait until the pane settles before typing, rather
		// than guessing a delay and landing in whatever the CLI put on screen
		go m.primeWhenReady(sess.ID, o.Prime)
	}
	fresh, err := m.DB.Session(sess.ID)
	if err != nil {
		return sess, nil
	}
	m.publish(fresh)
	m.Log.Info("session launched", "session", fresh.ID, "agent", agent,
		"target", target.Name, "workdir", workdir)
	return fresh, nil
}

// ScratchRootEnv names the variable a target can set to move its scratch area.
const ScratchRootEnv = "AGENTDECK_SCRATCH_ROOT"

// makeScratch creates a throwaway working directory ON THE TARGET and returns
// its absolute path.
//
// The path is resolved by the target's own shell rather than composed here: the
// control plane's $HOME is not the target's, and a workdir that only exists
// locally would launch the agent into a directory that is not there. `pwd` after
// the `cd` is what makes the returned path real.
func (m *Manager) makeScratch(ctx context.Context, ex executor.Executor, label string) (string, error) {
	// mktemp, not a name we compose: two scratch sessions started in the same
	// second would otherwise be handed the same directory and overwrite each
	// other's work. The target's own mkdir is the only atomic claim available.
	slug := scratchSlug(label)
	cmd := fmt.Sprintf(
		`root="${%s:-$HOME/agentdeck-scratch}"; mkdir -p "$root" && `+
			`d=$(mktemp -d "$root/%s-XXXXXX") && cd "$d" && `+
			`{ git rev-parse --git-dir >/dev/null 2>&1 || git init -q >/dev/null 2>&1; }; pwd`,
		ScratchRootEnv, slug)
	r, err := ex.Run(ctx, cmd, executor.RunOpts{Timeout: 30})
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(r.Stdout)
	if !r.OK() || dir == "" {
		return "", executor.Errf("could not create a scratch directory: %s",
			strings.TrimSpace(r.Stderr))
	}
	return dir, nil
}

// scratchSlug makes a label safe to use as a single path segment, and stamps it
// so two scratch sessions started the same day stay apart.
func scratchSlug(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 32 {
		slug = strings.Trim(slug[:32], "-")
	}
	if slug == "" {
		slug = "scratch"
	}
	// the date makes the directory readable; mktemp adds what makes it unique
	return slug + "-" + time.Now().Format("20060102")
}

// primeWhenReady types an opening message once the pane has stopped changing.
//
// Used only where the prompt cannot be an argument. A fixed delay is a race: the
// paste lands in whatever the CLI is showing at that instant, which is how a
// primed message once answered codex's self-update prompt.
func (m *Manager) primeWhenReady(id int64, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var last string
	stable := 0
	for i := 0; i < 45; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		sess, ex, err := m.resolve(id)
		if err != nil {
			return
		}
		out, err := ex.Run(ctx, PollCommand([]string{sess.TmuxSession}),
			RunOptsShort())
		if err != nil {
			continue
		}
		pane := ParsePoll(out.Stdout)[sess.TmuxSession]
		if strings.TrimSpace(pane) == "" {
			continue
		}
		if pane == last {
			stable++
		} else {
			stable, last = 0, pane
		}
		// settled for two consecutive polls and showing an input prompt
		if stable >= 1 && DeriveStatus(pane, Hash(pane)) == StatusWaiting {
			if err := m.SendText(ctx, id, text); err != nil {
				m.Log.Warn("priming session failed", "session", id, "err", err)
			}
			return
		}
	}
	m.Log.Warn("gave up priming: the pane never settled at a prompt", "session", id)
}

// ProjectEnv is the environment a project asks its agents to run with — the
// any-model door, shared by dispatched tasks and interactive sessions.
func (m *Manager) ProjectEnv(projectID *int64) map[string]string {
	out := map[string]string{}
	if projectID == nil {
		return out
	}
	proj, err := m.DB.Project(*projectID)
	if err != nil {
		return out
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(nzs(proj.EnvJSON, "{}")), &raw); err != nil {
		return out
	}
	for k, v := range raw {
		out[k] = fmt.Sprint(v)
	}
	return out
}

func nzs(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// specs is the agent set, falling back to the built-ins when none is injected.
// Resolve fills a spec's binary in from configuration, so a caller sees the
// command that would actually run rather than the name it was defined with.
func (m *Manager) Resolve(s Spec) Spec { return m.Launcher.resolve(s) }

func (m *Manager) specs() []Spec {
	if m.Specs == nil {
		return Builtins()
	}
	return m.Specs()
}

func (m *Manager) end(id int64, status string) {
	now := store.Now()
	m.DB.Update("sessions", id, map[string]any{
		"status": status, "ended_at": now, "updated_at": now})
}

// ---- driving -----------------------------------------------------------------

// SendText types a message into a session and submits it — the phone-side
// equivalent of typing at the terminal.
func (m *Manager) SendText(ctx context.Context, id int64, text string) error {
	sess, ex, err := m.resolve(id)
	if err != nil {
		return err
	}
	stage := fmt.Sprintf("/tmp/agentdeck-send-%d-%d", sess.ID, time.Now().UnixNano())
	if err := ex.WriteFile(ctx, stage, []byte(text)); err != nil {
		return err
	}
	r, err := ex.Run(ctx, SendTextCommand(sess.TmuxSession, stage),
		executor.RunOpts{Timeout: 30})
	if err != nil {
		return err
	}
	if !r.OK() {
		return executor.Errf("send failed: %s", strings.TrimSpace(r.Stderr))
	}
	return nil
}

// SendKey presses one allowlisted key in a session — Escape to interrupt a turn,
// Enter to accept, and so on.
func (m *Manager) SendKey(ctx context.Context, id int64, key string) error {
	sess, ex, err := m.resolve(id)
	if err != nil {
		return err
	}
	cmd, ok := SendKeyCommand(sess.TmuxSession, key)
	if !ok {
		return fmt.Errorf("unknown key %q", key)
	}
	_, err = ex.Run(ctx, cmd, executor.RunOpts{Timeout: 20})
	return err
}

// Kill ends a session's process and closes its record.
func (m *Manager) Kill(ctx context.Context, id int64) error {
	sess, ex, err := m.resolve(id)
	if err != nil {
		return err
	}
	if _, err := ex.Run(ctx, KillCommand(sess.TmuxSession),
		executor.RunOpts{Timeout: 20}); err != nil {
		return err
	}
	m.end(id, StatusDead)
	if fresh, err := m.DB.Session(id); err == nil {
		m.publish(fresh)
	}
	return nil
}

// Release stops tracking a session and LEAVES ITS PROCESS RUNNING.
//
// This is the counterpart to Adopt. Adoption is non-destructive — it just starts
// watching a terminal the operator already had open — so letting go of one has
// to be non-destructive too, or "add it to the board" quietly becomes "hand its
// life over to the board".
func (m *Manager) Release(id int64) error {
	sess, err := m.DB.Session(id)
	if err != nil {
		return err
	}
	now := store.Now()
	if err := m.DB.Update("sessions", sess.ID, map[string]any{
		"status": StatusIdle, "ended_at": now, "updated_at": now}); err != nil {
		return err
	}
	m.Bus.Publish("board", "session_dismissed", map[string]any{"id": id})
	m.Log.Info("session released (process left running)", "session", id,
		"tmux", sess.TmuxSession)
	return nil
}

// Dismiss drops a dead session from the live list without touching any process.
func (m *Manager) Dismiss(id int64) error {
	sess, err := m.DB.Session(id)
	if err != nil {
		return err
	}
	m.end(sess.ID, StatusDead)
	m.Bus.Publish("board", "session_dismissed", map[string]any{"id": id})
	return nil
}

func (m *Manager) resolve(id int64) (*store.Session, executor.Executor, error) {
	sess, err := m.DB.Session(id)
	if err != nil {
		return nil, nil, err
	}
	target, err := m.DB.Target(sess.TargetID)
	if err != nil {
		return nil, nil, err
	}
	ex, err := m.Reg.For(target)
	if err != nil {
		return nil, nil, err
	}
	return sess, ex, nil
}
