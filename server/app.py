"""App factory. Run: python -m server (or uvicorn server.app:create_app --factory)."""
import contextlib
import logging

from fastapi import FastAPI, Request
from fastapi.responses import FileResponse, JSONResponse
from fastapi.staticfiles import StaticFiles

from . import broker, config, db
from .routers import approvals, core, misc, tasks
from .scheduler import scheduler

log = logging.getLogger("agentdeck")


def seed_demo_data() -> None:
    """Mock mode ships with a ready board so the UI/e2e has something to show."""
    if db.one("SELECT id FROM targets LIMIT 1"):
        return
    t1 = db.insert("targets", {"name": "lxc-101-project-env", "kind": "mock",
                               "host": "192.0.2.10", "status": "online",
                               "max_concurrent": 4, "created_at": db.now()})
    t2 = db.insert("targets", {"name": "aiserver-local", "kind": "mock",
                               "host": "", "status": "online",
                               "max_concurrent": 8, "created_at": db.now()})
    db.insert("projects", {"name": "demo-app", "target_id": t1,
                           "repo_path": "/mock/demo-app", "created_at": db.now()})
    db.insert("projects", {"name": "homelab-api", "target_id": t2,
                           "repo_path": "/mock/homelab-api", "created_at": db.now()})


def create_app() -> FastAPI:
    @contextlib.asynccontextmanager
    async def lifespan(app: FastAPI):
        db.init()
        if config.MOCK:
            seed_demo_data()
        broker.reset()
        scheduler.start()
        yield
        from .terminal import terminals
        await terminals.shutdown()
        await scheduler.stop()
        db.close()

    app = FastAPI(title="agentdeck", version="1.0.0", lifespan=lifespan)

    if config.AUTH_TOKEN:
        @app.middleware("http")
        async def auth(request: Request, call_next):
            path = request.url.path
            if path.startswith("/api") and not path.startswith("/api/hook/"):
                supplied = request.headers.get("authorization", "").removeprefix("Bearer ")
                if supplied != config.AUTH_TOKEN and \
                        request.query_params.get("token") != config.AUTH_TOKEN:
                    return JSONResponse({"detail": "unauthorized"}, status_code=401)
            return await call_next(request)

    for r in (core, tasks, approvals, misc):
        app.include_router(r.router)

    @app.get("/")
    def index():
        return FileResponse(config.WEB_DIR / "index.html")

    app.mount("/", StaticFiles(directory=config.WEB_DIR), name="web")
    return app
