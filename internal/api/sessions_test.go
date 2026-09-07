package api_test

// Sessions are the interactive half of the board: an agent you work WITH for
// days, as opposed to a task you hand off. These drive the whole loop — launch,
// status, send, attach, discover, adopt, hand off — against a real HTTP server
// and a scripted target.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/JeremiahM37/agentdeck/internal/executor"
)

func (h *harness) session(body obj) obj {
	h.t.Helper()
	return h.post("/api/sessions", body, 201)
}

func (h *harness) sessionByID(id int64) obj {
	return h.get(fmt.Sprintf("/api/sessions/%d", id))
}

func (h *harness) waitSessionStatus(id int64, want ...string) obj {
	h.t.Helper()
	h.waitUntil(fmt.Sprintf("session %d to reach %v", id, want), func() bool {
		got := h.sessionByID(id).str("status")
		for _, w := range want {
			if got == w {
				return true
			}
		}
		return false
	})
	return h.sessionByID(id)
}

func TestSessionLaunchesAndReportsItsOwnState(t *testing.T) {
	h := newHarness(t)
	pid := h.seededProjectID()
	sess := h.session(obj{"project_id": pid, "name": "sglang", "agent": "claude",
		"model": "opus"})
	if sess.str("name") != "sglang" || sess.str("origin") != "agentdeck" {
		t.Fatalf("session: %v", sess)
	}
	if sess.str("tmux_session") == "" {
		t.Error("a session must own a tmux session")
	}
	// a project implies its target and working directory
	if int64(sess.num("target_id")) == 0 || sess.str("workdir") == "" {
		t.Errorf("project should supply target and workdir: %v", sess)
	}

	// status is derived from the pane, not from what we asked for
	live := h.waitSessionStatus(sess.id(), "waiting", "idle", "running")
	if live.str("pane_tail") == "" {
		t.Error("the card should preview what the pane is showing")
	}
	if _, ok := live["idle_seconds"].(float64); !ok {
		t.Error("idle time is the honest signal behind the status label")
	}
}

func TestSessionInteractiveLaunchIsNotAHeadlessTask(t *testing.T) {
	h := newHarness(t)
	h.session(obj{"project_id": h.seededProjectID(), "agent": "claude"})
	cmd := h.launchCmd()
	for _, unwanted := range []string{"stream-json", "prompt.md", "exit_code"} {
		if strings.Contains(cmd, unwanted) {
			t.Errorf("a session must not launch like a dispatched task (%q): %s", unwanted, cmd)
		}
	}
}

func TestSessionSendTypesIntoThePane(t *testing.T) {
	h := newHarness(t)
	sess := h.session(obj{"project_id": h.seededProjectID()})
	h.waitSessionStatus(sess.id(), "waiting", "idle", "running")

	h.post(fmt.Sprintf("/api/sessions/%d/send", sess.id()),
		obj{"text": "what is the state of the refactor?"}, 200)
	h.waitUntil("the message to appear on the pane", func() bool {
		return strings.Contains(h.sessionByID(sess.id()).str("pane_tail"), "state of the refactor")
	})
	// while the agent answers, the board should say it is working
	h.waitSessionStatus(sess.id(), "waiting", "idle")
}

func TestSessionSendKeyIsAnAllowlist(t *testing.T) {
	h := newHarness(t)
	sess := h.session(obj{"project_id": h.seededProjectID()})
	path := fmt.Sprintf("/api/sessions/%d/send", sess.id())
	h.post(path, obj{"key": "escape"}, 200)
	if code := h.status("POST", path, obj{"key": "rm -rf /"}); code != 422 {
		t.Fatalf("a raw input channel must only accept named keys, got %d", code)
	}
	if code := h.status("POST", path, obj{}); code != 400 {
		t.Errorf("an empty send: %d", code)
	}
}

func TestSessionAttachOpensATerminal(t *testing.T) {
	h := newHarness(t)
	h.App.Terminals.LookPath = func(string) (string, error) { return "/usr/bin/ttyd", nil }
	var argv []string
	h.App.Terminals.Spawn = func(port int, a []string) (*exec.Cmd, error) {
		argv = a
		return exec.Command("true"), nil
	}
	sess := h.session(obj{"project_id": h.seededProjectID()})
	got := h.post(fmt.Sprintf("/api/sessions/%d/terminal", sess.id()), nil, 200)
	if got.num("port") == 0 {
		t.Fatalf("attach: %v", got)
	}
	// this is the "drop me into the actual chat" path — it must attach to the
	// session's own tmux, not to a task's
	if len(argv) == 0 || argv[len(argv)-1] != sess.str("tmux_session") {
		t.Fatalf("ttyd wrapped the wrong session: %v", argv)
	}
}

func TestSessionKillEndsItAndDismissClearsIt(t *testing.T) {
	h := newHarness(t)
	sess := h.session(obj{"project_id": h.seededProjectID()})
	h.waitSessionStatus(sess.id(), "waiting", "idle", "running")
	h.decode("DELETE", fmt.Sprintf("/api/sessions/%d", sess.id()), nil, 200, nil)
	if got := h.sessionByID(sess.id()).str("status"); got != "dead" {
		t.Fatalf("status after kill: %s", got)
	}
	// a dead session leaves the live list but keeps its record
	for _, s := range h.getList("/api/sessions") {
		if s.id() == sess.id() {
			t.Error("a dead session should not stay on the live list")
		}
	}
	found := false
	for _, s := range h.getList("/api/sessions?all=true") {
		if s.id() == sess.id() {
			found = true
		}
	}
	if !found {
		t.Error("the record must survive the process")
	}
}

func TestSessionPollNoticesAProcessThatVanished(t *testing.T) {
	h := newHarness(t)
	sess := h.session(obj{"project_id": h.seededProjectID()})
	h.waitSessionStatus(sess.id(), "waiting", "idle", "running")
	// kill the tmux session behind agentdeck's back, the way a reboot or a
	// stray `tmux kill-server` would
	mock := h.mock()
	if _, err := mock.Run(context.Background(), "tmux kill-session -t "+sess.str("tmux_session"),
		executor.RunOpts{Timeout: 20}); err != nil {
		t.Fatal(err)
	}
	h.waitSessionStatus(sess.id(), "dead")
}

// Discovery is the half a task board misses entirely.
func TestDiscoverFindsAgentsAgentdeckDidNotStart(t *testing.T) {
	h := newHarness(t)
	found := h.getList("/api/sessions/discover")
	var legacy obj
	for _, c := range found {
		if c.str("tmux_session") == "legacy-claude" {
			legacy = c
		}
		if c.str("tmux_session") == "just-a-shell" {
			t.Error("a pane with no agent on it is just a terminal, not a candidate")
		}
	}
	if legacy == nil {
		t.Fatalf("the externally-started agent was not found: %v", found)
	}
	if legacy.str("agent") != "claude" || legacy.str("model") != "opus" {
		t.Errorf("candidate: %v", legacy)
	}
	if legacy["adopted"] != false {
		t.Error("an unknown agent must not claim to be adopted")
	}
	// its working directory matches a registered project, so the UI can offer
	// the right home for it instead of asking
	if legacy.str("project_name") != "demo-app" {
		t.Errorf("expected the matching project, got %q", legacy.str("project_name"))
	}
}

func TestAdoptTakesOverAnExistingTmuxSession(t *testing.T) {
	h := newHarness(t)
	pid := h.seededProjectID()
	sess := h.post("/api/sessions/adopt", obj{
		"target_id": h.firstTargetID(), "tmux_session": "legacy-claude",
		"project_id": pid, "name": "adopted", "workdir": "/mock/demo-app"}, 201)
	if sess.str("origin") != "discovered" {
		t.Errorf("origin: %v", sess.str("origin"))
	}
	if sess.str("tmux_session") != "legacy-claude" {
		t.Errorf("adoption must not rename the session: %v", sess)
	}
	// adopting twice is a conflict, not a duplicate card
	if code := h.status("POST", "/api/sessions/adopt", obj{
		"target_id": h.firstTargetID(), "tmux_session": "legacy-claude"}); code != 409 {
		t.Errorf("second adoption: %d", code)
	}
	// and discovery now reports THAT target's copy as known, so the UI does not
	// offer to adopt the same agent twice
	tid := h.firstTargetID()
	for _, c := range h.getList("/api/sessions/discover") {
		if c.str("tmux_session") != "legacy-claude" || int64(c.num("target_id")) != tid {
			continue
		}
		if c["adopted"] != true || int64(c.num("session_id")) != sess.id() {
			t.Errorf("an adopted session should show as adopted: %v", c)
		}
	}
}

func TestAdoptRefusesASessionThatIsNotThere(t *testing.T) {
	h := newHarness(t)
	code, body := h.request("POST", "/api/sessions/adopt", obj{
		"target_id": h.firstTargetID(), "tmux_session": "ghost-session"}, nil)
	if code != 400 {
		t.Fatalf("got %d %s", code, body)
	}
	if !strings.Contains(string(body), "no tmux session") {
		t.Errorf("the error should say what is missing: %s", body)
	}
}

// The handoff is what makes a six-month project survive the context window that
// happened to be working on it this week.
func TestHandoffWritesAWrapAndPrimesASuccessor(t *testing.T) {
	h := newHarness(t)
	pid := h.seededProjectID()
	sess := h.session(obj{"project_id": pid, "name": "long-haul"})
	h.waitSessionStatus(sess.id(), "waiting", "idle", "running")

	h.post(fmt.Sprintf("/api/sessions/%d/handoff", sess.id()),
		obj{"successor": true}, 202)

	// the wrap lands asynchronously — an agent mid-turn can take minutes
	h.waitUntil("the wrap to be written", func() bool {
		return len(h.getList(fmt.Sprintf("/api/sessions/%d/wraps", sess.id()))) > 0
	})
	wrap := h.getList(fmt.Sprintf("/api/sessions/%d/wraps", sess.id()))[0]
	if !strings.Contains(wrap.str("summary"), "WHERE WE ARE") {
		t.Fatalf("the wrap should carry the state the next agent needs: %q", wrap.str("summary"))
	}

	// the old session is retired and a fresh one carries the thread
	h.waitSessionStatus(sess.id(), "dead")
	var successor obj
	h.waitUntil("a successor session", func() bool {
		for _, s := range h.getList("/api/sessions") {
			if s.id() != sess.id() && s.str("name") == "long-haul" {
				successor = s
				return true
			}
		}
		return false
	})
	if successor.str("tmux_session") == sess.str("tmux_session") {
		t.Error("the successor must be a genuinely new process")
	}
	// the successor was primed with its predecessor's handoff
	h.waitUntil("the successor to be primed", func() bool {
		return strings.Contains(h.sessionByID(successor.id()).str("pane_tail"),
			"continuing work on")
	})

	// and the wrap reached project memory, so a DISPATCHED task on this project
	// starts from the same state an interactive session would
	notes := h.getList(fmt.Sprintf("/api/projects/%d/notes", pid))
	if len(notes) == 0 || !strings.Contains(notes[0].str("note"), "Session handoff") {
		t.Errorf("the wrap did not reach project memory: %v", notes)
	}
	// the project's handoff thread is the continuity across every context
	if len(h.getList(fmt.Sprintf("/api/projects/%d/wraps", pid))) == 0 {
		t.Error("the project should carry its own handoff thread")
	}
}

func TestHandoffRefusesToRunTwiceAtOnce(t *testing.T) {
	h := newHarness(t)
	sess := h.session(obj{"project_id": h.seededProjectID()})
	h.waitSessionStatus(sess.id(), "waiting", "idle", "running")
	path := fmt.Sprintf("/api/sessions/%d/handoff", sess.id())
	h.post(path, obj{}, 202)
	if code := h.status("POST", path, obj{}); code != 409 {
		t.Fatalf("a second concurrent handoff should conflict, got %d", code)
	}
}

func TestSessionValidation(t *testing.T) {
	h := newHarness(t)
	if code := h.status("POST", "/api/sessions", obj{"project_id": 9999}); code != 400 {
		t.Errorf("unknown project: %d", code)
	}
	if code := h.status("POST", "/api/sessions", obj{}); code != 400 {
		t.Errorf("no project and no target: %d", code)
	}
	if code := h.status("POST", "/api/sessions",
		obj{"project_id": h.seededProjectID(), "agent": "cursor"}); code != 422 {
		t.Errorf("unknown agent: %d", code)
	}
	if code := h.status("GET", "/api/sessions/9999", nil); code != 404 {
		t.Errorf("unknown session: %d", code)
	}
}

// An agent you started three days ago should say "up 3d", not "up 4s". tmux
// knows when its own session began; adoption asks rather than assuming the
// moment agentdeck noticed is the moment work started.
func TestAdoptTakesTmuxsUptimeNotTheMomentWeNoticed(t *testing.T) {
	h := newHarness(t)
	sess := h.post("/api/sessions/adopt", obj{
		"target_id": h.firstTargetID(), "tmux_session": "legacy-claude",
		"workdir": "/mock/demo-app"}, 201)
	if up := sess.num("uptime_seconds"); up < 3000 {
		t.Fatalf("uptime should come from tmux, got %.0fs", up)
	}
	if idle := sess.num("idle_seconds"); idle < 60 {
		t.Errorf("the idle clock should start from tmux's activity stamp, got %.0fs", idle)
	}
}

// Adoption is non-destructive, so un-adoption must be too. A bulk re-adopt once
// called DELETE on seven adopted sessions and killed seven live conversations,
// because the default was "kill" for everything.
func TestDeleteReleasesAnAdoptedSessionWithoutKillingIt(t *testing.T) {
	h := newHarness(t)
	sess := h.post("/api/sessions/adopt", obj{
		"target_id": h.firstTargetID(), "tmux_session": "legacy-claude",
		"workdir": "/mock/demo-app"}, 201)

	got := h.request2("DELETE", fmt.Sprintf("/api/sessions/%d", sess.id()), nil, 200)
	if got["killed"] != false {
		t.Fatalf("an adopted session must not be killed by default: %v", got)
	}
	// it is off the board...
	for _, s := range h.getList("/api/sessions") {
		if s.id() == sess.id() {
			t.Error("released session should leave the live list")
		}
	}
	// ...and its terminal is still running, so discovery finds it again
	if !h.cmdLogHas("kill-session -t legacy-claude") {
		found := false
		for _, c := range h.getList("/api/sessions/discover") {
			if c.str("tmux_session") == "legacy-claude" && c["adopted"] == false {
				found = true
			}
		}
		if !found {
			t.Error("the released terminal should still be discoverable")
		}
		return
	}
	t.Fatal("release must never issue a kill-session")
}

func TestDeleteKillsAnAdoptedSessionOnlyWhenAsked(t *testing.T) {
	h := newHarness(t)
	sess := h.post("/api/sessions/adopt", obj{
		"target_id": h.firstTargetID(), "tmux_session": "legacy-claude",
		"workdir": "/mock/demo-app"}, 201)
	got := h.request2("DELETE", fmt.Sprintf("/api/sessions/%d?kill=true", sess.id()), nil, 200)
	if got["killed"] != true {
		t.Fatalf("an explicit kill must kill: %v", got)
	}
	if !h.cmdLogHas("kill-session -t legacy-claude") {
		t.Error("the tmux session was not killed")
	}
}

// A session agentdeck launched is its own to end — no extra ceremony.
func TestDeleteKillsASessionAgentdeckStarted(t *testing.T) {
	h := newHarness(t)
	sess := h.session(obj{"project_id": h.seededProjectID()})
	h.waitSessionStatus(sess.id(), "waiting", "idle", "running")
	got := h.request2("DELETE", fmt.Sprintf("/api/sessions/%d", sess.id()), nil, 200)
	if got["killed"] != true {
		t.Fatalf("an agentdeck-launched session should end on delete: %v", got)
	}
	if h.sessionByID(sess.id()).str("status") != "dead" {
		t.Error("status after delete")
	}
}

// Which project a session belongs to is a judgement the operator makes after the
// fact: an agent's working directory is frequently a scratch dir that matches
// nothing, so adoption must not be the last word.
func TestSessionCanBeReassignedToAProject(t *testing.T) {
	h := newHarness(t)
	sess := h.post("/api/sessions/adopt", obj{
		"target_id": h.firstTargetID(), "tmux_session": "legacy-claude",
		"workdir": "/some/scratch/dir"}, 201)
	if sess["project_id"] != nil {
		t.Fatalf("a scratch dir should match no project: %v", sess["project_id"])
	}
	pid := h.seededProjectID()
	var moved obj
	h.decode("PATCH", fmt.Sprintf("/api/sessions/%d", sess.id()),
		obj{"project_id": pid, "name": "renamed"}, 200, &moved)
	if int64(moved.num("project_id")) != pid || moved.str("name") != "renamed" {
		t.Fatalf("patched: %v", moved)
	}
	if moved.str("project_name") == "" {
		t.Error("the view should carry the project's name for grouping")
	}
	// and it can be un-assigned again
	var cleared obj
	h.decode("PATCH", fmt.Sprintf("/api/sessions/%d", sess.id()),
		obj{"project_id": nil}, 200, &cleared)
	if cleared["project_id"] != nil {
		t.Errorf("expected unassigned, got %v", cleared["project_id"])
	}
	if code := h.status("PATCH", fmt.Sprintf("/api/sessions/%d", sess.id()),
		obj{"project_id": 9999}); code != 400 {
		t.Errorf("unknown project: %d", code)
	}
}

// The dashboard tile leads with "how many agents are waiting for me", so those
// counts have to be flat fields that never disappear.
func TestHealthCarriesSessionCounts(t *testing.T) {
	h := newHarness(t)
	before := h.get("/api/health")
	for _, f := range []string{"sessions", "sessions_waiting"} {
		if _, ok := before[f].(float64); !ok {
			t.Fatalf("%s missing or not a number: %v", f, before[f])
		}
	}
	sess := h.session(obj{"project_id": h.seededProjectID()})
	h.waitSessionStatus(sess.id(), "waiting", "idle", "running")
	if h.get("/api/health").num("sessions") != 1 {
		t.Errorf("sessions: %v", h.get("/api/health")["sessions"])
	}
	h.waitUntil("the waiting count to reflect a session at its prompt", func() bool {
		return h.get("/api/health").num("sessions_waiting") >= 1 ||
			h.sessionByID(sess.id()).str("status") != "waiting"
	})
}
