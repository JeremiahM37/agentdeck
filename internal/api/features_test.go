package api_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestAutoVerifyPass(t *testing.T) {
	h := newHarness(t)
	p := h.project("verpass", obj{"verify_cmd": "mockverify-pass"})
	task := h.run(p.id(), "t", "do it", nil)
	v := task.sub("attempt").sub("verify")
	if v.num("rc") != 0 || !strings.Contains(v.str("output"), "5 passed") {
		t.Fatalf("verify: %v", v)
	}
	found := false
	for _, e := range h.getList(fmt.Sprintf("/api/tasks/%d/events", task.id())) {
		if e.str("type") == "verify" && e.sub("payload").num("rc") == 0 {
			found = true
		}
	}
	if !found {
		t.Error("the verify result must land on the timeline, not just the badge")
	}
}

func TestAutoVerifyFailStillReviewable(t *testing.T) {
	h := newHarness(t)
	p := h.project("verfail", obj{"verify_cmd": "mockverify-fail"})
	task := h.run(p.id(), "t", "do it", nil)
	v := task.sub("attempt").sub("verify")
	if v.num("rc") != 1 || !strings.Contains(v.str("output"), "2 failed") {
		t.Fatalf("verify: %v", v)
	}
}

func TestCommitPushPR(t *testing.T) {
	h := newHarness(t)
	task := h.run(h.seededProjectID(), "Commit me", "do it", nil)
	got := h.post(fmt.Sprintf("/api/tasks/%d/commit", task.id()),
		obj{"message": "feat: thing", "push": true, "pr": true}, 200)
	steps := map[string]obj{}
	for _, s := range got.list("steps") {
		steps[s.str("step")] = s
	}
	if steps["commit"].num("rc") != 0 || steps["push"].num("rc") != 0 {
		t.Fatalf("steps: %v", steps)
	}
	if steps["pr"].str("url") != "https://github.com/mock/repo/pull/7" {
		t.Errorf("PR url: %v", steps["pr"])
	}
}

func TestCommitRequiresWorktreeAndReview(t *testing.T) {
	h := newHarness(t)
	task := h.task(h.seededProjectID(), "no wt", "", nil)
	if code := h.status("POST", fmt.Sprintf("/api/tasks/%d/commit", task.id()), obj{}); code != 409 {
		t.Fatalf("committing a task with no worktree: %d", code)
	}
}

func TestCleanupAfterDone(t *testing.T) {
	h := newHarness(t)
	task := h.run(h.seededProjectID(), "Clean me", "do it", nil)
	// not allowed while the diff is still under review
	if code := h.status("POST", fmt.Sprintf("/api/tasks/%d/cleanup", task.id()), nil); code != 409 {
		t.Errorf("cleanup during review: %d", code)
	}
	h.post(fmt.Sprintf("/api/tasks/%d/complete", task.id()), nil, 200)
	got := h.post(fmt.Sprintf("/api/tasks/%d/cleanup", task.id()), nil, 200)
	removed, _ := got["removed_attempts"].([]any)
	if len(removed) != 1 || removed[0].(float64) != 1 {
		t.Fatalf("removed: %v", got)
	}
	fresh := h.get(fmt.Sprintf("/api/tasks/%d", task.id()))
	if fresh.sub("attempt").str("worktree_path") != "" {
		t.Error("the worktree path should be cleared once reclaimed")
	}
}

// A rule the operator already agreed to must never page them again.
func TestPolicyAutoApproves(t *testing.T) {
	h := newHarness(t)
	p := h.project("policied", nil)
	h.decode("PATCH", fmt.Sprintf("/api/projects/%d", p.id()),
		obj{"policy": obj{"allow": []obj{{"tool": "Bash", "prefix": "rm"}}}}, 200, nil)
	task := h.task(p.id(), "policy run", "deploy [mock:approval]",
		obj{"permission_mode": "default"})
	h.post(fmt.Sprintf("/api/tasks/%d/dispatch", task.id()), obj{}, 200)
	// completes with no human decision at all (the mock command starts with 'rm')
	h.waitStatus(task.id(), "review")

	var mine []obj
	for _, a := range h.getList("/api/approvals?status=approved") {
		if int64(a.num("task_id")) == task.id() {
			mine = append(mine, a)
		}
	}
	if len(mine) == 0 || mine[0].str("decided_by") != "policy" {
		t.Fatalf("the auto-approval was not recorded as policy: %v", mine)
	}
	if len(h.pendingApprovals()) != 0 {
		t.Error("nothing should be left pending")
	}
}

func TestAlwaysAllowTeachesPolicy(t *testing.T) {
	h := newHarness(t)
	pid := h.seededProjectID()
	first := h.task(pid, "teach policy", "x [mock:approval]", obj{"permission_mode": "default"})
	h.post(fmt.Sprintf("/api/tasks/%d/dispatch", first.id()), obj{}, 200)
	appr := h.waitApproval(first.id())
	h.post(fmt.Sprintf("/api/approvals/%d/decision", appr.id()),
		obj{"decision": "approved", "always_allow": true}, 200)
	h.waitStatus(first.id(), "review")

	var proj obj
	for _, p := range h.getList("/api/projects") {
		if p.id() == pid {
			proj = p
		}
	}
	if !strings.Contains(proj.str("policy_json"), "rm") {
		t.Fatalf("the rule was not learned: %v", proj.str("policy_json"))
	}

	// a second identical run sails through with no human involvement
	second := h.task(pid, "policy reuse", "y [mock:approval]", obj{"permission_mode": "default"})
	h.post(fmt.Sprintf("/api/tasks/%d/dispatch", second.id()), obj{}, 200)
	h.waitStatus(second.id(), "review")
	for _, a := range h.pendingApprovals() {
		if int64(a.num("task_id")) == second.id() {
			t.Fatal("the second run still asked for permission")
		}
	}
}

func TestConcurrencySlotsRespected(t *testing.T) {
	h := newHarness(t)
	tgt := h.post("/api/targets", obj{"name": "narrow", "kind": "mock", "max_concurrent": 1}, 201)
	p := h.post("/api/projects",
		obj{"name": "narrowp", "target_id": tgt.id(), "repo_path": "/mock/n"}, 201)
	t1 := h.task(p.id(), "slot1", "a [mock:slow]", nil)
	t2 := h.task(p.id(), "slot2", "b [mock:slow]", nil)
	h.post(fmt.Sprintf("/api/tasks/%d/dispatch", t1.id()), obj{}, 200)
	h.post(fmt.Sprintf("/api/tasks/%d/dispatch", t2.id()), obj{}, 200)

	h.waitUntil("one of them running", func() bool {
		return h.taskStatus(t1.id()) == "running" || h.taskStatus(t2.id()) == "running"
	})
	statuses := []string{h.taskStatus(t1.id()), h.taskStatus(t2.id())}
	if statuses[0] != "queued" && statuses[1] != "queued" {
		t.Fatalf("a one-slot target ran both at once: %v", statuses)
	}
	h.waitUntil("both to finish", func() bool {
		return h.taskStatus(t1.id()) == "review" && h.taskStatus(t2.id()) == "review"
	})
}

func TestUnicodeAndShellCharsSurvive(t *testing.T) {
	h := newHarness(t)
	title := "héllo 🚀 'quotes' $(no-exec) `bt`"
	task := h.run(h.seededProjectID(), title, "prompt with "+title, nil)
	if task.str("title") != title {
		t.Fatalf("title mangled: %q", task.str("title"))
	}
	found := false
	for _, e := range h.getList(fmt.Sprintf("/api/tasks/%d/events", task.id())) {
		if e.str("type") == "result" {
			found = true
		}
	}
	if !found {
		t.Error("the run did not finish cleanly with a hostile title")
	}
}
