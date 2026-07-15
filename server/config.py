"""agentdeck configuration — all via environment, sane defaults for homelab dev."""
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

DB_PATH = Path(os.environ.get("AGENTDECK_DB", ROOT / "agentdeck.db"))


def diff_dir() -> Path:
    """Where captured diffs live — derived from the DB path so each database
    (incl. every test's tmp DB) gets its own store. Computed lazily so tests that
    monkeypatch DB_PATH are honored, and so mock runs never clobber the real
    production diffs (they once did — attempt ids collide across databases)."""
    return DB_PATH.parent / "agentdeck-diffs"
PORT = int(os.environ.get("AGENTDECK_PORT", "9110"))
HOST = os.environ.get("AGENTDECK_HOST", "0.0.0.0")

# Base URL targets use to reach the control plane (hook callbacks).
BASE_URL = os.environ.get("AGENTDECK_BASE_URL", f"http://127.0.0.1:{PORT}")

# Mock mode: MockExecutor everywhere, no real git/tmux/claude. Powers tests + UI demo.
MOCK = os.environ.get("AGENTDECK_MOCK", "") == "1"

# Optional single bearer token for the API/PWA ("none" auth when empty).
AUTH_TOKEN = os.environ.get("AGENTDECK_AUTH_TOKEN", "")

# Scheduler cadence (seconds). Tests crank this down.
TICK_SECONDS = float(os.environ.get("AGENTDECK_TICK", "2.0"))

# Approval long-poll ceiling per request; hook script loops until decided/expired.
APPROVAL_POLL_SECONDS = float(os.environ.get("AGENTDECK_APPROVAL_POLL", "25"))
APPROVAL_EXPIRE_SECONDS = float(os.environ.get("AGENTDECK_APPROVAL_EXPIRE", "900"))

# worktrees of done/cancelled tasks older than this are swept (0 disables)
JANITOR_DAYS = float(os.environ.get("AGENTDECK_JANITOR_DAYS", "7"))

VAPID_PRIVATE_KEY = os.environ.get("AGENTDECK_VAPID_PRIVATE", "")
VAPID_PUBLIC_KEY = os.environ.get("AGENTDECK_VAPID_PUBLIC", "")
VAPID_CLAIMS_EMAIL = os.environ.get("AGENTDECK_VAPID_EMAIL", "admin@homelab.internal")

WEB_DIR = ROOT / "web"
HOOKS_DIR = ROOT / "hooks"

CLAUDE_BIN = os.environ.get("AGENTDECK_CLAUDE_BIN", "claude")
