package api_test

// A scratch session is an empty room: any agent CLI, a throwaway directory, no
// decision about what the work is. Promotion is where that decision gets made,
// afterwards, without moving anything.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/store"
)

func TestAScratchSessionGetsItsOwnDirectory(t *testing.T) {
	h := newHarness(t)
	code, body := h.request("POST", "/api/sessions", obj{
		"agent": "claude", "scratch": true, "name": "thinking"}, nil)
	if code != 201 {
		t.Fatalf("launching a scratch session: %d %s", code, body)
	}
	var sess store.Session
	json.Unmarshal(body, &sess)
	if sess.ProjectID != nil {
		t.Errorf("a scratch session must not be attached to a project: %v", *sess.ProjectID)
	}
	if sess.Workdir == "" {
		t.Fatal("a scratch session still needs somewhere to work")
	}
	if !strings.Contains(sess.Workdir, "agentdeck-scratch") {
		t.Errorf("scratch work belongs under the scratch root, got %q", sess.Workdir)
	}
	if !strings.Contains(sess.Workdir, "thinking") {
		t.Errorf("the directory should carry the session's name: %q", sess.Workdir)
	}
}

// Two scratch sessions started together must not land in one directory and
// overwrite each other's work.
func TestScratchSessionsDoNotShareADirectory(t *testing.T) {
	h := newHarness(t)
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		code, body := h.request("POST", "/api/sessions", obj{"agent": "claude", "scratch": true}, nil)
		if code != 201 {
			t.Fatalf("%d: %s", code, body)
		}
		var sess store.Session
		json.Unmarshal(body, &sess)
		if seen[sess.Workdir] {
			t.Fatalf("two scratch sessions share %q", sess.Workdir)
		}
		seen[sess.Workdir] = true
	}
}

// The agent is the operator's choice, including one they defined themselves.
func TestAScratchSessionRunsAnyConfiguredAgent(t *testing.T) {
	h := newHarness(t)
	if code, body := h.request("PUT", "/api/agents", []obj{
		{"name": "claude", "command": "claude", "prompt_arg": true},
		{"name": "codex", "command": "codex", "prompt_arg": true},
		{"name": "aider", "command": "aider", "model_flag": "--model"},
	}, nil); code != 200 {
		t.Fatalf("defining agents: %d %s", code, body)
	}

	for _, agent := range []string{"claude", "codex", "aider"} {
		code, body := h.request("POST", "/api/sessions", obj{"agent": agent, "scratch": true}, nil)
		if code != 201 {
			t.Fatalf("%s: %d %s", agent, code, body)
		}
		var sess store.Session
		json.Unmarshal(body, &sess)
		if sess.Agent != agent {
			t.Errorf("asked for %q, got %q", agent, sess.Agent)
		}
	}
	// and an agent that was never defined is refused rather than silently
	// launching whatever the default is
	if code, body := h.request("POST", "/api/sessions",
		obj{"agent": "not-an-agent", "scratch": true}, nil); code != 422 {
		t.Errorf("unknown agent: %d %s", code, body)
	}
}

func TestPromotingAScratchSessionCreatesAProject(t *testing.T) {
	h := newHarness(t)
	code, body := h.request("POST", "/api/sessions", obj{
		"agent": "claude", "scratch": true, "name": "notes app"}, nil)
	if code != 201 {
		t.Fatalf("%d %s", code, body)
	}
	var sess store.Session
	json.Unmarshal(body, &sess)

	code, body = h.request("POST", fmt.Sprintf("/api/sessions/%d/promote", sess.ID),
		obj{"name": "notes-app"}, nil)
	if code != 200 {
		t.Fatalf("promote: %d %s", code, body)
	}
	var out struct {
		Project store.Project `json:"project"`
		Session store.Session `json:"session"`
	}
	json.Unmarshal(body, &out)

	if out.Project.ID == 0 {
		t.Fatal("no project was created")
	}
	if out.Project.Name != "notes-app" {
		t.Errorf("project name: %q", out.Project.Name)
	}
	// nothing moves: the project's repository is where the agent already works
	if out.Project.RepoPath != sess.Workdir {
		t.Errorf("the project should adopt the session's directory: %q vs %q",
			out.Project.RepoPath, sess.Workdir)
	}
	if out.Project.TargetID != sess.TargetID {
		t.Errorf("the project must live on the same target as the session")
	}
	if out.Project.DefaultAgent != "claude" {
		t.Errorf("the project should default to the agent the work was done with, got %q",
			out.Project.DefaultAgent)
	}
	// and the conversation carries straight on, now attached
	if out.Session.ProjectID == nil || *out.Session.ProjectID != out.Project.ID {
		t.Fatalf("the session was not linked to its new project: %v", out.Session.ProjectID)
	}
	stored, err := h.App.DB.Session(sess.ID)
	if err != nil || stored.ProjectID == nil || *stored.ProjectID != out.Project.ID {
		t.Errorf("the link did not persist: %v", err)
	}
	// the session now appears under the project on the board
	code, body = h.request("GET", "/api/sessions", nil, nil)
	if code != 200 || !strings.Contains(string(body), `"project_name":"notes-app"`) {
		t.Errorf("the promoted session is not shown under its project: %s", body)
	}
}

// The name is optional — promoting with no name should still produce something
// readable, not a timestamped scratch directory name.
func TestPromotingWithoutANameUsesTheDirectory(t *testing.T) {
	h := newHarness(t)
	code, body := h.request("POST", "/api/sessions",
		obj{"agent": "claude", "scratch": true, "name": "inference tuning"}, nil)
	if code != 201 {
		t.Fatalf("%d %s", code, body)
	}
	var sess store.Session
	json.Unmarshal(body, &sess)

	code, body = h.request("POST", fmt.Sprintf("/api/sessions/%d/promote", sess.ID), obj{}, nil)
	if code != 200 {
		t.Fatalf("promote: %d %s", code, body)
	}
	var out struct {
		Project store.Project `json:"project"`
	}
	json.Unmarshal(body, &out)
	if out.Project.Name != "inference-tuning" {
		t.Errorf("the timestamp should be dropped from the name, got %q", out.Project.Name)
	}
}

// Promoting into a project that already exists is the other half: work that
// turns out to belong to something you already track.
func TestPromotingIntoAnExistingProject(t *testing.T) {
	h := newHarness(t)
	existing := h.seededProjectID()
	code, body := h.request("POST", "/api/sessions", obj{"agent": "claude", "scratch": true}, nil)
	if code != 201 {
		t.Fatalf("%d %s", code, body)
	}
	var sess store.Session
	json.Unmarshal(body, &sess)

	before, _ := h.App.DB.Projects()
	code, body = h.request("POST", fmt.Sprintf("/api/sessions/%d/promote", sess.ID),
		obj{"project_id": existing}, nil)
	if code != 200 {
		t.Fatalf("promote: %d %s", code, body)
	}
	after, _ := h.App.DB.Projects()
	if len(after) != len(before) {
		t.Errorf("adopting into an existing project created a new one: %d -> %d",
			len(before), len(after))
	}
	stored, _ := h.App.DB.Session(sess.ID)
	if stored.ProjectID == nil || *stored.ProjectID != existing {
		t.Errorf("the session was not adopted: %v", stored.ProjectID)
	}
}

// Two sessions in the same directory are the same project, not two.
func TestPromotingTheSameDirectoryTwiceReusesTheProject(t *testing.T) {
	h := newHarness(t)
	var ids []int64
	var workdir string
	for i := 0; i < 2; i++ {
		body := obj{"agent": "claude"}
		if workdir == "" {
			body["scratch"] = true
		} else {
			body["workdir"] = workdir
		}
		code, raw := h.request("POST", "/api/sessions", body, nil)
		if code != 201 {
			t.Fatalf("%d %s", code, raw)
		}
		var sess store.Session
		json.Unmarshal(raw, &sess)
		workdir = sess.Workdir
		ids = append(ids, sess.ID)
	}

	var projectIDs []int64
	for _, id := range ids {
		code, raw := h.request("POST", fmt.Sprintf("/api/sessions/%d/promote", id), obj{}, nil)
		if code != 200 {
			t.Fatalf("promote %d: %d %s", id, code, raw)
		}
		var out struct {
			Project store.Project `json:"project"`
		}
		json.Unmarshal(raw, &out)
		projectIDs = append(projectIDs, out.Project.ID)
	}
	if projectIDs[0] != projectIDs[1] {
		t.Errorf("one directory produced two projects: %v", projectIDs)
	}
}

// A session that already belongs somewhere must not be silently re-homed.
func TestPromotingAnAttachedSessionIsRefused(t *testing.T) {
	h := newHarness(t)
	project := h.seededProjectID()
	code, body := h.request("POST", "/api/sessions",
		obj{"agent": "claude", "project_id": project}, nil)
	if code != 201 {
		t.Fatalf("%d %s", code, body)
	}
	var sess store.Session
	json.Unmarshal(body, &sess)
	if code, body := h.request("POST", fmt.Sprintf("/api/sessions/%d/promote", sess.ID),
		obj{"name": "somewhere else"}, nil); code != 409 {
		t.Errorf("expected a refusal, got %d %s", code, body)
	}
}

func TestPromotingAnUnknownSessionIs404(t *testing.T) {
	h := newHarness(t)
	if code := h.status("POST", "/api/sessions/9999/promote", obj{"name": "x"}); code != 404 {
		t.Errorf("got %d", code)
	}
}

// ---- the real thing ----------------------------------------------------

// The whole flow on this machine: a real blank tmux session with a real agent
// process in a real directory, worked in, then promoted into a project that a
// task can actually be dispatched against.
func TestARealScratchSessionIsPromotableAndDispatchable(t *testing.T) {
	r := newInteractiveRig(t)
	sess := r.launchSession(map[string]any{
		"agent": "claude", "scratch": true, "name": "spike"})

	if sess.ProjectID != nil {
		t.Error("a scratch session should start unattached")
	}
	r.waitForLog(sess.Workdir, "argv:", 10*time.Second)
	if !tmuxAlive(sess.TmuxSession) {
		t.Fatal("no real tmux session was started")
	}
	// the directory is real, and it is a git repository so it can host worktrees
	if _, err := os.Stat(sess.Workdir); err != nil {
		t.Fatalf("the scratch directory is not on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sess.Workdir, ".git")); err != nil {
		t.Fatalf("a scratch directory must be a git repository to be promotable: %v", err)
	}

	// do some work in the room
	if err := os.WriteFile(filepath.Join(sess.Workdir, "idea.md"),
		[]byte("# the idea\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, body := r.do("POST", fmt.Sprintf("/api/sessions/%d/promote", sess.ID),
		map[string]any{"name": "the-spike"})
	if code != 200 {
		t.Fatalf("promote: %d %s", code, body)
	}
	var out struct {
		Project store.Project `json:"project"`
	}
	json.Unmarshal(body, &out)

	// the tmux session is untouched by promotion — the operator is still in it
	if !tmuxAlive(sess.TmuxSession) {
		t.Fatal("promoting the session killed the conversation")
	}
	// and the work is still where it was
	if _, err := os.Stat(filepath.Join(out.Project.RepoPath, "idea.md")); err != nil {
		t.Errorf("promotion moved the work: %v", err)
	}

	// the real payoff: the new project can now be dispatched against, which
	// needs a real commit for a worktree to branch from
	mustRun(t, out.Project.RepoPath, "git", "config", "user.email", "t@example.com")
	mustRun(t, out.Project.RepoPath, "git", "config", "user.name", "t")
	mustRun(t, out.Project.RepoPath, "git", "add", "-A")
	mustRun(t, out.Project.RepoPath, "git", "commit", "-q", "-m", "the spike so far")

	// point the agent binary back at the one-shot fake for the dispatch
	if err := os.WriteFile(r.app.Cfg.ClaudeBin, []byte(fakeAgent), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := r.project
	r.project = out.Project.ID
	id := r.dispatch("first real task", "continue the spike")
	task := r.waitStatus(id, "done", "review", "failed")
	r.project = saved
	if task.Status == "failed" {
		att, _ := r.app.DB.LatestAttempt(id)
		t.Fatalf("a promoted project could not run a task: %s", att.ResultJSON)
	}
	att, _ := r.app.DB.LatestAttempt(id)
	if att.WorktreePath == "" {
		t.Error("no worktree was created for the promoted project")
	}
}
