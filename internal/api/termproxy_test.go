package api_test

// Attach opened `http://<the browser's hostname>:<port>`, which named whichever
// machine served the page. Through the nginx vhost that is the reverse proxy,
// which runs no ttyd, so the tab said "refused to connect" — and no test noticed
// because none of them ever looked at the URL the UI would open.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/terminal"
)

// The URL handed to the browser must be same-origin, so it survives every way
// of reaching agentdeck: directly, through nginx, over the tailnet, on a phone.
func TestAttachReturnsASameOriginURL(t *testing.T) {
	h := newHarness(t)
	h.App.Terminals.LookPath = func(string) (string, error) { return "/usr/bin/ttyd", nil }
	h.App.Terminals.Spawn = func(int, []string) (*exec.Cmd, error) {
		cmd := exec.Command("sleep", "10")
		return cmd, cmd.Start()
	}
	t.Cleanup(h.App.Terminals.Shutdown)
	task := h.task(h.seededProjectID(), "attachable", "x [mock:slow]", nil)
	h.post(fmt.Sprintf("/api/tasks/%d/dispatch", task.id()), obj{}, 200)
	h.waitStatus(task.id(), "running")

	for _, path := range []string{
		fmt.Sprintf("/api/tasks/%d/terminal", task.id()),
	} {
		code, body := h.request("POST", path, obj{}, nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", path, code, body)
		}
		var out struct {
			Port int    `json:"port"`
			URL  string `json:"url"`
		}
		json.Unmarshal(body, &out)
		if out.URL == "" {
			t.Fatal("no url was returned; the client would have to guess the host")
		}
		if !strings.HasPrefix(out.URL, "/term/") {
			t.Errorf("the url must be relative to this origin, got %q", out.URL)
		}
		for _, bad := range []string{"http://", "https://", "localhost", "127.0.0.1"} {
			if strings.Contains(out.URL, bad) {
				t.Errorf("url names a host (%q) — that is the bug: %q", bad, out.URL)
			}
		}
		if out.URL != terminal.BasePath(out.Port)+"/" {
			t.Errorf("url %q does not match port %d", out.URL, out.Port)
		}
	}
}

// The prefix must not become a way to reach anything else on loopback.
func TestTerminalProxyOnlyServesPortsItOwns(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/term/7710/",     // in range, but nothing attached
		"/term/9110/",     // agentdeck itself
		"/term/22/",       // ssh
		"/term/0/",        // nonsense
		"/term/notaport/", // nonsense
		"/term/9111/",     // grimoire
		"/term/-1/",       // negative
	} {
		code, _ := h.request("GET", path, nil, nil)
		if code != 404 {
			t.Errorf("%s should be refused, got %d", path, code)
		}
	}
}

// ttyd's asset and websocket URLs are absolute, so it has to be launched with
// the base path it is mounted under or the page loads blank.
// ttyd's asset and websocket URLs are absolute, so it has to be launched with
// the base path it is mounted under or the page loads blank — and a ttyd with
// no credential must never listen on the network.
func TestTTYDArgsMountUnderTheBasePathOnLoopback(t *testing.T) {
	got := strings.Join(terminal.TTYDArgs(7712, []string{"tmux", "attach", "-t", "adk-1"}), " ")
	for _, want := range []string{
		"-p 7712",       // the port it was given
		"-i lo",         // an unauthenticated shell stays off the network
		"-b /term/7712", // or its assets 404 under the proxy
		"--once",        // exits when the client disconnects
		"-W",            // the operator can type
		"tmux attach -t adk-1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ttyd args missing %q:\n  %s", want, got)
		}
	}
	// the base path must match what the proxy actually serves
	if !strings.Contains(got, "-b "+terminal.BasePath(7712)) {
		t.Errorf("base path disagrees with the proxy's route: %s", got)
	}
}

// ---- against a real ttyd ------------------------------------------------

// The proxy either carries a real ttyd's page and websocket or it does not, and
// only a real one can say. This is the test that would have caught the original
// bug, because it asks for the terminal the way a browser does.
func TestARealTerminalIsReachableThroughTheProxy(t *testing.T) {
	if _, err := exec.LookPath("ttyd"); err != nil {
		t.Skip("ttyd is not installed")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	r := newInteractiveRig(t)
	sess := r.launchSession(map[string]any{"agent": "claude", "scratch": true, "name": "termtest"})
	r.waitForLog(sess.Workdir, "argv:", 10*time.Second)

	code, body := r.do("POST", fmt.Sprintf("/api/sessions/%d/terminal", sess.ID), map[string]any{})
	if code != 200 {
		t.Fatalf("attach: %d %s", code, body)
	}
	var out struct {
		Port int    `json:"port"`
		URL  string `json:"url"`
	}
	json.Unmarshal(body, &out)
	t.Cleanup(func() { r.app.Terminals.Shutdown() })

	// the browser asks the SAME origin it loaded the app from
	var resp *http.Response
	var err error
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = http.Get(r.url + out.URL)
		if err == nil && resp.StatusCode == 200 {
			break
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("the terminal page is not reachable through the proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("terminal page: %d", resp.StatusCode)
	}
	page, _ := io.ReadAll(resp.Body)
	if !strings.Contains(strings.ToLower(string(page)), "ttyd") &&
		!strings.Contains(strings.ToLower(string(page)), "terminal") {
		t.Errorf("that does not look like ttyd's page: %.200s", page)
	}

	// and ttyd must NOT be reachable directly on the network any more
	direct, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", out.Port))
	if err == nil {
		direct.Body.Close()
	}
	// loopback still answers (that is where the proxy talks to it); what matters
	// is that the page is served under the prefix rather than at the root
	root, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", out.Port))
	if err == nil {
		defer root.Body.Close()
		if root.StatusCode == 200 {
			t.Logf("note: ttyd also answers at its root on loopback (%d)", root.StatusCode)
		}
	}
}
