package api_test

import (
	"fmt"
	"strings"
	"testing"
)

// gated dispatches a task whose fake agent will ask for permission through the
// REAL hook endpoints.
func gated(h *harness, pid int64, title string) obj {
	h.t.Helper()
	task := h.task(pid, title, "deploy it [mock:approval]", obj{"permission_mode": "default"})
	h.post(fmt.Sprintf("/api/tasks/%d/dispatch", task.id()), obj{}, 200)
	return task
}

func TestApproveLetsAgentFinish(t *testing.T) {
	h := newHarness(t)
	pid := h.seededProjectID()
	task := gated(h, pid, "Gated approve")
	appr := h.waitApproval(task.id())
	if appr.str("tool_name") != "Bash" {
		t.Fatalf("tool: %v", appr)
	}
	if !strings.Contains(appr.sub("input").str("command"), "rm -rf build/") {
		t.Errorf("input: %v", appr.sub("input"))
	}
	if appr.str("task_title") != "Gated approve" {
		t.Errorf("the approval must name its task: %v", appr)
	}

	decided := h.post(fmt.Sprintf("/api/approvals/%d/decision", appr.id()),
		obj{"decision": "approved"}, 200)
	if decided.str("status") != "approved" {
		t.Fatalf("decision: %v", decided)
	}
	h.waitStatus(task.id(), "review")
	seen := map[string]bool{}
	for _, e := range h.getList(fmt.Sprintf("/api/tasks/%d/events", task.id())) {
		seen[e.str("type")] = true
	}
	if !seen["tool_use"] || !seen["result"] {
		t.Errorf("the agent did not continue after approval: %v", seen)
	}
}

func TestDenyStopsAgent(t *testing.T) {
	h := newHarness(t)
	pid := h.seededProjectID()
	task := gated(h, pid, "Gated deny")
	appr := h.waitApproval(task.id())
	h.post(fmt.Sprintf("/api/approvals/%d/decision", appr.id()),
		obj{"decision": "denied", "note": "too risky"}, 200)
	h.waitStatus(task.id(), "review")

	stopped := false
	for _, e := range h.getList(fmt.Sprintf("/api/tasks/%d/events", task.id())) {
		if e.str("type") == "text" &&
			strings.Contains(strings.ToLower(e.sub("payload").str("text")), "denied") {
			stopped = true
		}
	}
	if !stopped {
		t.Error("the denial reason never reached the agent")
	}
	denied := h.getList("/api/approvals?status=denied")
	if len(denied) == 0 || denied[0].str("note") != "too risky" {
		t.Errorf("the decision was not recorded: %v", denied)
	}
}

func TestHookEndpointRejectsBadToken(t *testing.T) {
	h := newHarness(t)
	code := h.status("POST", "/api/hook/approval",
		obj{"token": "nope", "tool_name": "Bash", "tool_input": obj{}})
	if code != 403 {
		t.Fatalf("an unknown attempt token must be refused, got %d", code)
	}
}

func TestDoubleDecisionConflicts(t *testing.T) {
	h := newHarness(t)
	pid := h.seededProjectID()
	task := gated(h, pid, "Gated double")
	appr := h.waitApproval(task.id())
	path := fmt.Sprintf("/api/approvals/%d/decision", appr.id())
	if code := h.status("POST", path, obj{"decision": "approved"}); code != 200 {
		t.Fatalf("first decision: %d", code)
	}
	if code := h.status("POST", path, obj{"decision": "denied"}); code != 409 {
		t.Errorf("a second decision must conflict, got %d", code)
	}
	if code := h.status("POST", "/api/approvals/9999/decision",
		obj{"decision": "approved"}); code != 409 {
		t.Errorf("deciding a nonexistent approval: %d", code)
	}
}

func TestDecisionMustBeApprovedOrDenied(t *testing.T) {
	h := newHarness(t)
	if code := h.status("POST", "/api/approvals/1/decision",
		obj{"decision": "maybe"}); code != 400 {
		t.Fatalf("an invented decision must be rejected, got %d", code)
	}
}
