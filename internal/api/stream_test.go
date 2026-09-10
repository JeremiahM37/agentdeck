package api_test

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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

func TestDrainStreamsClosesLiveResponsesAndRejectsNewStreams(t *testing.T) {
	h := newHarness(t)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(h.URL + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("stream status %d", resp.StatusCode)
	}
	h.App.Server.DrainStreams()
	var drains sync.WaitGroup
	for i := 0; i < 8; i++ {
		drains.Add(1)
		go func() { defer drains.Done(); h.App.Server.DrainStreams() }()
	}
	drains.Wait() // safe across signal handling and final cleanup
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("stream did not drain: %v", err)
	}
	if !strings.Contains(string(body), ": connected") {
		t.Fatal("stream never connected")
	}
	for _, path := range []string{"/api/stream", "/api/tasks/1/stream"} {
		next, err := client.Get(h.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		next.Body.Close()
		if next.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("%s status %d", path, next.StatusCode)
		}
	}
	health, err := client.Get(h.URL + "/api/health")
	if err != nil {
		t.Fatal(err)
	}
	defer health.Body.Close()
	if health.StatusCode != 200 {
		t.Fatal("draining streams stopped ordinary requests")
	}
}
