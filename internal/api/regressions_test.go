package api_test

// Tripwires on the things that boot fine and break silently.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeremiahM37/agentdeck/internal/version"
	"github.com/JeremiahM37/agentdeck/web"
)

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(raw)
}

// The image must build and ship the binary; a Dockerfile that merely runs is not
// evidence it ships a working one.
func TestDockerfileBuildsAndShipsTheBinary(t *testing.T) {
	df := repoFile(t, "deploy/Dockerfile")
	for _, want := range []string{"go build", "./cmd/agentdeck", "COPY --from=build"} {
		if !strings.Contains(df, want) {
			t.Errorf("Dockerfile is missing %q", want)
		}
	}
	// CGO off keeps the pure-Go sqlite driver working on a slim base image
	if !strings.Contains(df, "CGO_ENABLED=0") {
		t.Error("the build must stay CGO-free or the slim image cannot run it")
	}
}

// The runtime image needs the tools a dispatch actually shells out to.
func TestDockerfileInstallsRuntimeTools(t *testing.T) {
	df := repoFile(t, "deploy/Dockerfile")
	for _, tool := range []string{"git", "tmux", "openssh-client", "ttyd"} {
		if !strings.Contains(df, tool) {
			t.Errorf("Dockerfile is missing runtime tool %q", tool)
		}
	}
}

func TestDockerfileHasHealthcheck(t *testing.T) {
	df := repoFile(t, "deploy/Dockerfile")
	if !strings.Contains(df, "HEALTHCHECK") || !strings.Contains(df, "/api/health") {
		t.Fatal("orchestrators need a liveness probe")
	}
}

// The systemd unit runs the compiled binary, not a source tree.
func TestSystemdUnitRunsTheBinary(t *testing.T) {
	unit := repoFile(t, "deploy/agentdeck.service")
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/agentdeck") {
		t.Error("the unit should exec the installed binary")
	}
	// agent binaries in ~/.local/bin are why a probe reports a missing codex
	if !strings.Contains(unit, "Environment=PATH=") {
		t.Error("the unit must set an explicit PATH")
	}
}

// One version string, reported by the API and shown in the UI.
func TestVersionIsDeclaredOnce(t *testing.T) {
	h := newHarness(t)
	if got := h.get("/api/health").str("version"); got != version.Version {
		t.Fatalf("/api/health reports %q, the package says %q", got, version.Version)
	}
	if !strings.Contains(repoFile(t, "README.md"), version.Version) {
		t.Errorf("README does not mention the current version %s", version.Version)
	}
}

// The PWA is embedded, so a missing asset is a build-time fact, not a 404 that
// only a phone discovers.
func TestWebAssetsAreEmbedded(t *testing.T) {
	if len(web.IndexHTML) == 0 {
		t.Fatal("index.html was not embedded")
	}
	for _, name := range []string{
		"static/app.js", "static/style.css", "static/sw.js", "static/icon.svg",
		"static/manifest.webmanifest", "static/fonts.css",
		"static/fonts/inter-latin.woff2",
	} {
		if _, err := web.Assets.ReadFile(name); err != nil {
			t.Errorf("%s is not embedded: %v", name, err)
		}
	}
}

// The service worker must skip non-GET requests: the cache API rejects them
// outright, so swallowing one would break every POST the page makes.
func TestServiceWorkerSkipsNonGET(t *testing.T) {
	sw, err := web.Assets.ReadFile("static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sw), `e.request.method !== "GET"`) {
		t.Fatal("sw.js must return early for non-GET requests")
	}
	// and it must never cache the API, which is live state
	if !strings.Contains(string(sw), "/api/") {
		t.Error("sw.js must exclude the API from caching")
	}
}

// The agent-side kit is embedded too — there is no hooks/ directory to deploy.
func TestAgentHooksAreEmbedded(t *testing.T) {
	h := newHarness(t)
	h.run(h.seededProjectID(), "hooks", "x", nil)
	if !strings.Contains(string(h.staged("/.agentdeck/adk.py")), "add-task") {
		t.Error("the task-filing kit was not staged")
	}
	if !strings.Contains(string(h.staged("/.agentdeck/env")), "ADK_TOKEN=") {
		t.Error("the per-attempt token was not staged")
	}
}
