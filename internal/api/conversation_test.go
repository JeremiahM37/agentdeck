package api_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskMessagesAreDurableAndIdempotent(t *testing.T) {
	h := newHarness(t)
	task := h.task(h.seededProjectID(), "instructions", "original", nil)
	path := fmt.Sprintf("/api/tasks/%d/messages", task.id())
	h.post(path, obj{"text": " ", "request_id": "request-0"}, 422)
	first := h.post(path, obj{"text": "Keep the public API stable", "request_id": "request-1"}, 202)
	again := h.post(path, obj{"text": "Keep the public API stable", "request_id": "request-1"}, 202)
	if first.id() != again.id() {
		t.Fatal("retry created duplicate message")
	}
	h.post(path, obj{"text": "different", "request_id": "request-1"}, 409)
	h.waitUntil("backlog instructions saved", func() bool {
		rows, _ := h.App.DB.TaskMessages(task.id())
		return len(rows) == 1 && rows[0].Status == "delivered"
	})
	saved, _ := h.App.DB.Task(task.id())
	if saved.Status != "backlog" || strings.Count(saved.Prompt, "Keep the public API stable") != 1 {
		t.Fatalf("unexpected task: %+v", saved)
	}
	attempts, _ := h.App.DB.AttemptsWhere("task_id=?", task.id())
	if len(attempts) != 0 {
		t.Fatal("message dispatched backlog task")
	}
}

func TestRealTaskConversationResumesTheSameWorktree(t *testing.T) {
	r := newRealRig(t)
	id := r.dispatch("conversation", "add initial note")
	r.waitStatus(id, "review")
	first, _ := r.app.DB.LatestAttempt(id)
	path := fmt.Sprintf("/api/tasks/%d/messages", id)
	body := map[string]any{"text": "Add another note, preserving the first", "request_id": "follow-up-1"}
	for i := 0; i < 2; i++ {
		code, raw := r.do("POST", path, body)
		if code != 202 {
			t.Fatalf("send: %d %s", code, raw)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		last, _ := r.app.DB.LatestAttempt(id)
		if last.N == 2 && last.Status == "done" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	last, _ := r.app.DB.LatestAttempt(id)
	if last.N != 2 || last.Status != "done" || last.WorktreePath != first.WorktreePath || last.ResumeSession != first.SessionID {
		t.Fatalf("follow-up: %+v; first: %+v", last, first)
	}
	raw, _ := os.ReadFile(filepath.Join(last.WorktreePath, "NOTES.md"))
	if strings.Count(string(raw), "agent was here") != 2 {
		t.Fatalf("lost previous edit: %s", raw)
	}
	messages, _ := r.app.DB.TaskMessages(id)
	if len(messages) != 1 || messages[0].AttemptID == nil || *messages[0].AttemptID != last.ID {
		t.Fatalf("receipt: %+v", messages)
	}
}

func TestRealRunningTaskWaitsThenInterruptsForFollowup(t *testing.T) {
	r := newRealRig(t)
	// Hold the first real process after it has written a file. A resumed run exits.
	script := strings.Replace(fakeAgent, "# fail on demand", `if [[ "$prompt" != *"OPERATOR MESSAGE:"* ]]; then
  echo ready > ready-marker
  sleep 60
fi
# fail on demand`, 1)
	if err := os.WriteFile(r.app.Cfg.ClaudeBin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	id := r.dispatch("interrupt", "first run")
	r.waitStatus(id, "running")
	first, _ := r.app.DB.LatestAttempt(id)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(first.WorktreePath, "ready-marker")); err == nil {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	path := fmt.Sprintf("/api/tasks/%d/messages", id)
	code, raw := r.do("POST", path, map[string]any{"text": "Keep the first edit", "request_id": "queued-follow-up"})
	if code != 202 {
		t.Fatalf("queue: %d %s", code, raw)
	}
	time.Sleep(250 * time.Millisecond)
	messages, _ := r.app.DB.TaskMessages(id)
	if messages[0].Status != "pending" {
		t.Fatal("delivered before active run finished")
	}
	code, raw = r.do("POST", path, map[string]any{"text": "Change direction now", "interrupt": true, "request_id": "interrupt-follow-up"})
	if code != 202 {
		t.Fatalf("interrupt: %d %s", code, raw)
	}
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		last, _ := r.app.DB.LatestAttempt(id)
		if last.N == 2 && last.Status == "done" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	last, _ := r.app.DB.LatestAttempt(id)
	if last.N != 2 || last.Status != "done" || last.WorktreePath != first.WorktreePath {
		t.Fatalf("follow-up: %+v", last)
	}
	old, _ := r.app.DB.Attempt(first.ID)
	if old.Status != "cancelled" {
		t.Fatalf("old run: %+v", old)
	}
	if !strings.Contains(last.Prompt, "Keep the first edit") || !strings.Contains(last.Prompt, "Change direction now") {
		t.Fatal("lost pending instructions")
	}
	raw, _ = os.ReadFile(filepath.Join(last.WorktreePath, "NOTES.md"))
	if strings.Count(string(raw), "agent was here") != 2 {
		t.Fatalf("lost prior work: %s", raw)
	}
}

func TestPendingMessageSurvivesRestartAndIsDeliveredOnce(t *testing.T) {
	d := newDeployment(t)
	d.stop()
	d.cfg.TickInterval = time.Hour
	d.boot()
	id := d.newTask(d.seedProject(), "restart message", "original")
	path := fmt.Sprintf("/api/tasks/%d/messages", id)
	body := map[string]any{"text": "Instruction saved before restart", "request_id": "restart-message"}
	code, raw := d.do("POST", path, body)
	if code != 202 {
		t.Fatalf("send: %d %s", code, raw)
	}
	rows, _ := d.app.DB.TaskMessages(id)
	if len(rows) != 1 || rows[0].Status != "pending" {
		t.Fatal("expected durable pending message")
	}
	d.stop()
	d.cfg.TickInterval = 40 * time.Millisecond
	d.boot()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ = d.app.DB.TaskMessages(id)
		if rows[0].Status == "delivered" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if rows[0].Status != "delivered" {
		t.Fatal("restart lost delivery")
	}
	code, raw = d.do("POST", path, body)
	if code != 202 {
		t.Fatalf("retry: %d %s", code, raw)
	}
	d.stop()
	d.boot()
	saved, _ := d.app.DB.Task(id)
	if saved.Status != "backlog" || strings.Count(saved.Prompt, "Instruction saved before restart") != 1 {
		t.Fatalf("replayed after restart: %+v", saved)
	}
}
