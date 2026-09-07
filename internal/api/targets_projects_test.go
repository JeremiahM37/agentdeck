package api_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestTargetCRUDAndProbe(t *testing.T) {
	h := newHarness(t)
	created := h.post("/api/targets",
		obj{"name": "lxc-104", "kind": "mock", "host": "192.0.2.20", "user": "dev"}, 201)
	tid := created.id()

	if code := h.status("POST", "/api/targets", obj{"name": "lxc-104"}); code != 409 {
		t.Errorf("a duplicate name must conflict, got %d", code)
	}

	probed := h.post(fmt.Sprintf("/api/targets/%d/check", tid), nil, 200)
	if probed.str("status") != "online" {
		t.Errorf("probe status: %v", probed)
	}
	if !strings.Contains(probed.str("info_json"), "mock") {
		t.Errorf("probe info: %v", probed.str("info_json"))
	}

	proj := h.post("/api/projects",
		obj{"name": "demo", "target_id": tid, "repo_path": "/opt/demo"}, 201)
	if code := h.status("POST", "/api/projects",
		obj{"name": "x", "target_id": 999, "repo_path": "/x"}); code != 400 {
		t.Errorf("a project on a nonexistent target must be rejected, got %d", code)
	}

	// deletion guards: a target with projects, and a project with tasks, stay put
	if code := h.status("DELETE", fmt.Sprintf("/api/targets/%d", tid), nil); code != 409 {
		t.Errorf("deleting a target that has projects: %d", code)
	}
	if code := h.status("DELETE", fmt.Sprintf("/api/projects/%d", proj.id()), nil); code != 204 {
		t.Errorf("deleting an empty project: %d", code)
	}
	if code := h.status("DELETE", fmt.Sprintf("/api/targets/%d", tid), nil); code != 204 {
		t.Errorf("deleting a now-empty target: %d", code)
	}
}

func TestMockSeedPresent(t *testing.T) {
	h := newHarness(t)
	names := map[string]bool{}
	for _, tgt := range h.getList("/api/targets") {
		names[tgt.str("name")] = true
	}
	if !names["lxc-101-project-env"] || !names["aiserver-local"] {
		t.Fatalf("mock mode should seed a demo board: %v", names)
	}
	if len(h.getList("/api/projects")) < 2 {
		t.Fatal("mock mode should seed demo projects")
	}
}

func TestUnknownTargetKindRejected(t *testing.T) {
	h := newHarness(t)
	if code := h.status("POST", "/api/targets", obj{"name": "bad", "kind": "warp"}); code != 422 {
		t.Fatalf("an unknown kind must fail schema validation, got %d", code)
	}
	if code := h.status("POST", "/api/targets",
		obj{"name": "lxc-105", "kind": "pct", "host": "105"}); code != 201 {
		t.Fatalf("pct is a real target kind, got %d", code)
	}
}

func TestProjectPatchVerifyCmd(t *testing.T) {
	h := newHarness(t)
	pid := h.seededProjectID()
	var patched obj
	h.decode("PATCH", fmt.Sprintf("/api/projects/%d", pid),
		obj{"verify_cmd": "mockverify-pass"}, 200, &patched)
	if patched.str("verify_cmd") != "mockverify-pass" {
		t.Fatalf("patch: %v", patched)
	}
	if code := h.status("PATCH", "/api/projects/9999", obj{}); code != 404 {
		t.Errorf("patching a nonexistent project: %d", code)
	}
}

func TestProjectNotesEndpoint(t *testing.T) {
	h := newHarness(t)
	pid := h.seededProjectID()
	if got := h.getList(fmt.Sprintf("/api/projects/%d/notes", pid)); len(got) != 0 {
		t.Fatalf("a new project has no notes: %v", got)
	}
}

// Import derives a project's name from its directory, and a directory name is
// not always the project's name — /opt/docker is "the compose stack".
func TestProjectCanBeRenamed(t *testing.T) {
	h := newHarness(t)
	p := h.project("docker", nil)
	var renamed obj
	h.decode("PATCH", fmt.Sprintf("/api/projects/%d", p.id()),
		obj{"name": "docker-stack"}, 200, &renamed)
	if renamed.str("name") != "docker-stack" {
		t.Fatalf("renamed: %v", renamed)
	}
	// an empty name is ignored rather than blanking the card
	h.decode("PATCH", fmt.Sprintf("/api/projects/%d", p.id()), obj{"name": "  "}, 200, &renamed)
	if renamed.str("name") != "docker-stack" {
		t.Errorf("an empty rename should be a no-op: %v", renamed)
	}
}

// Machines move. Before this, a target that changed address had to be deleted
// and recreated — which is blocked while it has projects, so a LAN renumber
// meant re-pointing every project by hand.
func TestTargetConnectionDetailsArePatchable(t *testing.T) {
	h := newHarness(t)
	tgt := h.post("/api/targets",
		obj{"name": "moved", "kind": "ssh", "host": "192.168.1.182", "user": "root"}, 201)
	// it has a project, so deleting and recreating is not available
	h.post("/api/projects",
		obj{"name": "onmoved", "target_id": tgt.id(), "repo_path": "/srv/x"}, 201)
	if code := h.status("DELETE", fmt.Sprintf("/api/targets/%d", tgt.id()), nil); code != 409 {
		t.Fatalf("a target with projects should not be deletable: %d", code)
	}

	var moved obj
	h.decode("PATCH", fmt.Sprintf("/api/targets/%d", tgt.id()),
		obj{"host": "100.118.152.78", "user": "admin", "port": 2222,
			"name": "lxc-101"}, 200, &moved)
	if moved.str("host") != "100.118.152.78" || moved.str("user") != "admin" ||
		moved.num("port") != 2222 || moved.str("name") != "lxc-101" {
		t.Fatalf("patched: %v", moved)
	}
	// a rename must not collide with an existing target
	other := h.post("/api/targets", obj{"name": "taken", "kind": "mock"}, 201)
	if code := h.status("PATCH", fmt.Sprintf("/api/targets/%d", other.id()),
		obj{"name": "lxc-101"}); code != 409 {
		t.Errorf("duplicate rename: %d", code)
	}
}
