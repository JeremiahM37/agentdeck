"""Health, push subscription, and notification-sink settings endpoints."""
from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

from .. import config, db, push, sinks

router = APIRouter(prefix="/api")


@router.get("/health")
def health():
    return {
        "ok": True, "mock": config.MOCK,
        "tasks": {r["status"]: r["c"] for r in
                  db.query("SELECT status, COUNT(*) c FROM tasks GROUP BY status")},
        "pending_approvals": db.one(
            "SELECT COUNT(*) c FROM approvals WHERE status='pending'")["c"],
    }


@router.get("/settings")
def get_settings():
    return sinks.get_settings()


@router.put("/settings")
def put_settings(body: dict):
    for key, value in body.items():
        if key not in sinks.SETTING_KEYS:
            raise HTTPException(400, f"unknown setting {key!r}")
        if not isinstance(value, str):
            raise HTTPException(400, f"{key} must be a string")
        sinks.set_setting(key, value.strip())
    return sinks.get_settings()


@router.post("/settings/test-notification")
def test_notification():
    sinks.notify("Test notification", "agentdeck sinks are wired up 🎛", url="/")
    return {"sent": True}


@router.get("/templates")
def get_templates():
    row = db.one("SELECT value FROM settings WHERE key='templates'")
    return db.unj(row["value"] if row else "", []) or []


@router.put("/templates")
def put_templates(templates: list[dict]):
    for t in templates:
        if not isinstance(t.get("name"), str) or not t["name"]:
            raise HTTPException(400, "each template needs a name")
    sinks.set_setting("templates", db.j(templates))
    return templates


@router.get("/stats")
def stats():
    week_ago = db.now() - 7 * 86400
    rows = db.query(
        "SELECT p.name, a.result_json, a.finished_at FROM attempts a "
        "JOIN tasks t ON t.id=a.task_id JOIN projects p ON p.id=t.project_id "
        "WHERE a.result_json != '{}'")
    total, week, by_project = 0.0, 0.0, {}
    for r in rows:
        cost = db.unj(r["result_json"]).get("cost_usd") or 0
        total += cost
        if (r["finished_at"] or 0) > week_ago:
            week += cost
        by_project[r["name"]] = by_project.get(r["name"], 0.0) + cost
    tasks_done = db.one("SELECT COUNT(*) c FROM tasks WHERE status='done'")["c"]
    return {"total_cost_usd": round(total, 4), "last_7d_usd": round(week, 4),
            "tasks_done": tasks_done,
            "by_project": [{"name": k, "cost_usd": round(v, 4)}
                           for k, v in sorted(by_project.items(),
                                              key=lambda x: -x[1])]}


class JanitorIn(BaseModel):
    days: float | None = None


@router.post("/admin/janitor")
async def run_janitor(body: JanitorIn | None = None):
    from ..scheduler import scheduler
    return await scheduler.janitor(body.days if body else None)


@router.get("/push/vapid")
def vapid_key():
    if not config.VAPID_PUBLIC_KEY:
        raise HTTPException(404, "push not configured (set AGENTDECK_VAPID_PUBLIC/PRIVATE)")
    return {"key": config.VAPID_PUBLIC_KEY}


class SubscriptionIn(BaseModel):
    endpoint: str
    keys: dict = {}


@router.post("/push/subscribe", status_code=201)
def subscribe(sub: SubscriptionIn):
    push.subscribe(sub.model_dump())
    return {"ok": True}
