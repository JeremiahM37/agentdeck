"""Regression tripwires for issues found during production-readiness testing."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def test_dockerfile_ships_every_runtime_directory():
    """The image must COPY every directory the server needs at runtime —
    a missing COPY boots fine but breaks features silently."""
    df = (ROOT / "deploy" / "Dockerfile").read_text()
    for required in ("COPY server", "COPY web", "COPY hooks"):
        assert required in df, f"Dockerfile is missing '{required}'"


def test_dockerfile_installs_all_runtime_deps():
    df = (ROOT / "deploy" / "Dockerfile").read_text()
    for dep in ("fastapi", "uvicorn", "asyncssh", "httpx", "pywebpush"):
        assert dep in df, f"Dockerfile is missing runtime dep '{dep}'"


def test_dockerfile_has_healthcheck():
    """Added after the container audit — orchestrators need liveness."""
    df = (ROOT / "deploy" / "Dockerfile").read_text()
    assert "HEALTHCHECK" in df and "/api/health" in df


def test_version_is_one_point_oh():
    """Ship as v1.0.0 — pyproject and the FastAPI app must agree."""
    py = (ROOT / "pyproject.toml").read_text()
    assert 'version = "1.0.0"' in py
    app = (ROOT / "server" / "app.py").read_text()
    assert 'version="1.0.0"' in app
