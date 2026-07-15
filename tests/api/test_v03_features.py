"""v0.3: settings/sinks endpoints and the terminal-attach endpoint."""
from tests.conftest import wait_for


def test_settings_roundtrip(client):
    assert client.get("/api/settings").json() == {
        "discord_webhook": "", "ntfy_server": "", "ntfy_topic": ""}
    r = client.put("/api/settings", json={
        "discord_webhook": "https://discord/hook",
        "ntfy_server": "https://ntfy.sh", "ntfy_topic": "adk"})
    assert r.status_code == 200
    assert client.get("/api/settings").json()["ntfy_topic"] == "adk"
    # unknown keys rejected
    assert client.put("/api/settings", json={"evil": "x"}).status_code == 400
    assert client.put("/api/settings", json={"ntfy_topic": 5}).status_code == 400


def test_test_notification_endpoint(client, monkeypatch):
    sent = []
    from server import sinks
    monkeypatch.setattr(sinks, "_send_all", lambda p: sent.extend(p))
    client.put("/api/settings", json={"discord_webhook": "https://discord/hook"})
    r = client.post("/api/settings/test-notification")
    assert r.status_code == 200 and r.json()["sent"]
    wait_for(lambda: sent, timeout=5, msg="sink payload built")
    assert sent[0][0] == "discord"


def test_notify_fires_on_review(seeded, monkeypatch):
    c, pid = seeded["client"], seeded["project_id"]
    sent = []
    from server import sinks
    monkeypatch.setattr(sinks, "_send_all", lambda p: sent.extend(p))
    c.put("/api/settings", json={"ntfy_server": "https://ntfy.sh",
                                 "ntfy_topic": "adk"})
    t = c.post("/api/tasks", json={"project_id": pid, "title": "notify me",
                                   "prompt": "x"}).json()
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: any(k == "ntfy" and "Ready for review" in m["title"]
                         for k, _, m in sent), msg="ntfy payload for review")


def test_terminal_attach_endpoint(seeded, monkeypatch):
    c, pid = seeded["client"], seeded["project_id"]

    async def fake_spawn(attempt, target):
        assert attempt["tmux_session"].startswith("adk-")
        return 7777
    from server.terminal import terminals
    monkeypatch.setattr(terminals, "spawn", fake_spawn)

    t = c.post("/api/tasks", json={"project_id": pid, "title": "term",
                                   "prompt": "x [mock:slow]"}).json()
    # not running yet
    assert c.post(f"/api/tasks/{t['id']}/terminal").status_code == 409
    c.post(f"/api/tasks/{t['id']}/dispatch", json={})
    wait_for(lambda: c.get(f"/api/tasks/{t['id']}").json()["status"] == "running",
             msg="running")
    r = c.post(f"/api/tasks/{t['id']}/terminal")
    assert r.status_code == 200 and r.json()["port"] == 7777
    assert c.post("/api/tasks/9999/terminal").status_code == 404
