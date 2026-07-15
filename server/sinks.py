"""Notification sinks: web-push + Discord webhook + ntfy, fanned out best-effort.
ntfy approval notifications carry real approve/deny HTTP action buttons, so a
phone can decide without opening the app (and without VAPID setup).
Settings live in the DB (settings table) and are editable from the Targets tab.
"""
import json
import logging
import threading
import urllib.request

from . import config, db, push

log = logging.getLogger("agentdeck.sinks")

SETTING_KEYS = ("discord_webhook", "ntfy_server", "ntfy_topic")


def get_settings() -> dict:
    rows = {r["key"]: r["value"] for r in db.query("SELECT * FROM settings")}
    return {k: rows.get(k, "") for k in SETTING_KEYS}


def set_setting(key: str, value: str) -> None:
    db.execute("INSERT INTO settings(key, value) VALUES(?,?) "
               "ON CONFLICT(key) DO UPDATE SET value=excluded.value", (key, value))


def build_payloads(cfg: dict, title: str, body: str, url: str = "/",
                   extra: dict | None = None) -> list[tuple[str, str, dict]]:
    """Pure builder: (kind, post_url, json_payload) per configured sink."""
    out = []
    click = config.BASE_URL.rstrip("/") + url
    if cfg.get("discord_webhook"):
        out.append(("discord", cfg["discord_webhook"],
                    {"content": f"**{title}** — {body}"[:1900]}))
    if cfg.get("ntfy_server") and cfg.get("ntfy_topic"):
        msg: dict = {"topic": cfg["ntfy_topic"], "title": f"agentdeck: {title}",
                     "message": body[:800], "click": click}
        if (extra or {}).get("kind") == "approval":
            decision_url = (f"{config.BASE_URL.rstrip('/')}/api/approvals/"
                            f"{extra['approval_id']}/decision")
            msg["priority"] = 4
            msg["actions"] = [
                {"action": "http", "label": "✅ Approve", "url": decision_url,
                 "method": "POST", "headers": {"Content-Type": "application/json"},
                 "body": json.dumps({"decision": "approved"})},
                {"action": "http", "label": "⛔ Deny", "url": decision_url,
                 "method": "POST", "headers": {"Content-Type": "application/json"},
                 "body": json.dumps({"decision": "denied",
                                     "note": "denied from ntfy"})},
            ]
        out.append(("ntfy", cfg["ntfy_server"].rstrip("/"), msg))
    return out


def _post(kind: str, url: str, payload: dict) -> None:
    req = urllib.request.Request(url, method="POST",
                                 headers={"Content-Type": "application/json"},
                                 data=json.dumps(payload).encode())
    with urllib.request.urlopen(req, timeout=6):
        pass


def _send_all(payloads: list) -> None:
    for kind, url, payload in payloads:
        try:
            _post(kind, url, payload)
        except Exception as e:   # noqa: BLE001 — sinks must never take the app down
            log.warning("%s sink failed: %s", kind, e)


def notify(title: str, body: str, url: str = "/", extra: dict | None = None) -> None:
    push.send(title, body, url, extra)
    payloads = build_payloads(get_settings(), title, body, url, extra)
    if payloads:
        threading.Thread(target=_send_all, args=(payloads,), daemon=True).start()
