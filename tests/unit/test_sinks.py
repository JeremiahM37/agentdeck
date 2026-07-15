import json

from server.sinks import build_payloads


def test_no_sinks_configured_builds_nothing():
    assert build_payloads({}, "t", "b") == []
    assert build_payloads({"discord_webhook": "", "ntfy_server": "", "ntfy_topic": ""},
                          "t", "b") == []


def test_discord_payload():
    out = build_payloads({"discord_webhook": "https://discord/hook"}, "Title", "Body")
    assert out == [("discord", "https://discord/hook",
                    {"content": "**Title** — Body"})]


def test_ntfy_requires_server_and_topic():
    assert build_payloads({"ntfy_server": "https://ntfy.sh"}, "t", "b") == []
    assert build_payloads({"ntfy_topic": "adk"}, "t", "b") == []


def test_ntfy_plain_payload():
    (kind, url, msg), = build_payloads(
        {"ntfy_server": "https://ntfy.sh/", "ntfy_topic": "adk"},
        "Ready for review", "Fix the bug", url="/#task/3")
    assert kind == "ntfy" and url == "https://ntfy.sh"
    assert msg["topic"] == "adk"
    assert msg["title"].endswith("Ready for review")
    assert msg["click"].endswith("/#task/3")
    assert "actions" not in msg


def test_ntfy_approval_carries_action_buttons():
    (_, _, msg), = build_payloads(
        {"ntfy_server": "https://ntfy.sh", "ntfy_topic": "adk"},
        "Approval needed", "Bash: rm -rf build/",
        extra={"kind": "approval", "approval_id": 42})
    labels = [a["label"] for a in msg["actions"]]
    assert any("Approve" in lbl for lbl in labels)
    assert any("Deny" in lbl for lbl in labels)
    for a in msg["actions"]:
        assert a["url"].endswith("/api/approvals/42/decision")
        assert a["method"] == "POST"
        body = json.loads(a["body"])
        assert body["decision"] in ("approved", "denied")
    assert msg["priority"] == 4


def test_long_bodies_truncated():
    out = build_payloads({"discord_webhook": "x"}, "T", "y" * 5000)
    assert len(out[0][2]["content"]) <= 1900
