package api_test

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The board stream is what makes the UI live; without it every client falls back
// to the 30s poll and the app feels broken.
func TestBoardStreamPushesTaskEvents(t *testing.T) {
	h := newHarness(t)
	req, _ := http.NewRequest("GET", h.URL+"/api/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type: %q", ct)
	}
	// nginx buffers proxied responses by default, holding every event forever
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Error("the stream must opt out of proxy buffering")
	}

	lines := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	// the first frame is the connect comment, so a client knows it is attached
	select {
	case line := <-lines:
		if !strings.HasPrefix(line, ":") {
			t.Fatalf("first frame: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no connect frame")
	}

	task := h.task(h.seededProjectID(), "streamed", "x", nil)
	deadline := time.After(10 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("the stream closed early")
			}
			if strings.HasPrefix(line, "data:") &&
				strings.Contains(line, fmt.Sprintf(`"id":%d`, task.id())) {
				return
			}
		case <-deadline:
			t.Fatal("the new task never reached the board stream")
		}
	}
}

func TestTaskStreamCarriesAgentEvents(t *testing.T) {
	h := newHarness(t)
	task := h.task(h.seededProjectID(), "streamed agent", "x [mock:slow]", nil)
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/tasks/%d/stream", h.URL, task.id()), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	lines := make(chan string, 128)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	h.post(fmt.Sprintf("/api/tasks/%d/dispatch", task.id()), obj{}, 200)

	deadline := time.After(15 * time.Second)
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("the stream closed early")
			}
			if line == "event: agent_event" {
				return
			}
		case <-deadline:
			t.Fatal("no agent event reached the task stream")
		}
	}
}
