package api_test

// Live views widen what is reachable, so these tests are about the refusals:
// the forwarding itself is covered where a real target exists, in the forward
// package and the browser suite.

import (
	"strings"
	"testing"

	"github.com/JeremiahM37/agentdeck/internal/config"
)

func TestLiveViewsRefuseATargetWhoseLoopbackCannotBeReached(t *testing.T) {
	h := newHarness(t)
	// The scripted target has no network of its own to forward into.
	code, body := h.request("POST", "/api/live/ports", obj{"port": 8080}, nil)
	if code != 409 || !strings.Contains(string(body), "cannot forward ports") {
		t.Fatalf("port: %d %s", code, body)
	}
	code, body = h.request("POST", "/api/live/desktops", obj{"title": "x"}, nil)
	if code != 409 {
		t.Fatalf("desktop: %d %s", code, body)
	}
	if views := h.getList("/api/live"); len(views) != 0 {
		t.Errorf("a refused request left a view behind: %v", views)
	}
	if code := h.status("DELETE", "/api/live/1", nil); code != 404 {
		t.Errorf("stopping nothing: %d", code)
	}
}

func TestLiveViewsStayShutOnATokenProtectedServer(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.AuthToken = "secret123" })
	auth := map[string]string{"Authorization": "Bearer secret123"}
	// Even a caller holding the token is refused: the port it would open could
	// not ask the next caller for one.
	code, body := h.request("POST", "/api/live/ports", obj{"port": 8080}, auth)
	if code != 409 || !strings.Contains(string(body), "AGENTDECK_LIVE_UNAUTHENTICATED") {
		t.Fatalf("got %d %s", code, body)
	}
	if code, _ := h.request("POST", "/api/live/ports", obj{"port": 8080}, nil); code != 401 {
		t.Errorf("without the token the API itself must refuse first: %d", code)
	}
}
