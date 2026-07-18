"""Web-push sink (VAPID). Gracefully a no-op until keys are configured.
Sink abstraction (Discord/ntfy) lands in v0.2 — see DESIGN.md §4.6.
"""
import json
import logging

from . import config, db

log = logging.getLogger("agentdeck.push")

try:
    from pywebpush import WebPushException, webpush  # optional dep
except ImportError:
    webpush = None


def subscribe(subscription: dict) -> None:
    db.execute(
        "INSERT INTO push_subscriptions(endpoint, keys_json, created_at) VALUES(?,?,?) "
        "ON CONFLICT(endpoint) DO UPDATE SET keys_json=excluded.keys_json",
        (subscription["endpoint"], db.j(subscription.get("keys", {})), db.now()))


def send(title: str, body: str, url: str = "/", extra: dict | None = None) -> None:
    if webpush is None or not config.VAPID_PRIVATE_KEY:
        log.debug("push (noop): %s — %s", title, body)
        return
    payload = json.dumps({"title": title, "body": body, "url": url, **(extra or {})})
    for sub in db.query("SELECT * FROM push_subscriptions"):
        try:
            webpush(
                subscription_info={"endpoint": sub["endpoint"],
                                   "keys": db.unj(sub["keys_json"])},
                data=payload,
                vapid_private_key=config.VAPID_PRIVATE_KEY,
                vapid_claims={"sub": f"mailto:{config.VAPID_CLAIMS_EMAIL}"})
        except WebPushException as e:
            if getattr(e, "response", None) is not None and e.response.status_code in (404, 410):
                db.execute("DELETE FROM push_subscriptions WHERE id=?", (sub["id"],))
            else:
                log.warning("push failed: %s", e)
