"""SQLite persistence. Plain SQL, WAL, one lock — homelab scale, zero magic."""
import json
import sqlite3
import threading
import time
from pathlib import Path

from . import config

_conn: sqlite3.Connection | None = None
_lock = threading.Lock()

SCHEMA = """
CREATE TABLE IF NOT EXISTS targets(
  id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL,
  kind TEXT NOT NULL DEFAULT 'ssh',            -- local | ssh | mock
  host TEXT DEFAULT '', port INTEGER DEFAULT 22, user TEXT DEFAULT 'root',
  key_path TEXT DEFAULT '', workroot TEXT DEFAULT '',
  max_concurrent INTEGER DEFAULT 4, sandbox INTEGER DEFAULT 0,
  status TEXT DEFAULT 'unknown', info_json TEXT DEFAULT '{}',
  created_at REAL
);
CREATE TABLE IF NOT EXISTS projects(
  id INTEGER PRIMARY KEY, name TEXT NOT NULL,
  target_id INTEGER NOT NULL REFERENCES targets(id),
  repo_path TEXT NOT NULL, default_base_branch TEXT DEFAULT 'main',
  workroot_override TEXT DEFAULT '', policy_json TEXT DEFAULT '{}',
  verify_cmd TEXT DEFAULT '', keep_worktrees INTEGER DEFAULT 0,
  review_gate INTEGER DEFAULT 0, env_json TEXT DEFAULT '{}',
  created_at REAL
);
CREATE TABLE IF NOT EXISTS tasks(
  id INTEGER PRIMARY KEY, project_id INTEGER NOT NULL REFERENCES projects(id),
  title TEXT NOT NULL, prompt TEXT DEFAULT '',
  status TEXT NOT NULL DEFAULT 'backlog',
  priority INTEGER DEFAULT 2, labels_json TEXT DEFAULT '[]',
  agent TEXT DEFAULT 'claude', model TEXT DEFAULT '',
  permission_mode TEXT DEFAULT 'acceptEdits', base_branch TEXT DEFAULT '',
  parent_task_id INTEGER, created_by TEXT DEFAULT 'user',
  created_by_attempt INTEGER,
  created_at REAL, updated_at REAL
);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE TABLE IF NOT EXISTS attempts(
  id INTEGER PRIMARY KEY, task_id INTEGER NOT NULL REFERENCES tasks(id),
  n INTEGER NOT NULL, status TEXT NOT NULL DEFAULT 'queued',
  token TEXT NOT NULL DEFAULT '',
  prompt TEXT DEFAULT '', resume_session TEXT DEFAULT '', model TEXT DEFAULT '',
  sandbox_vmid TEXT DEFAULT '',
  worktree_path TEXT DEFAULT '', branch TEXT DEFAULT '', tmux_session TEXT DEFAULT '',
  session_id TEXT DEFAULT '', log_offset INTEGER DEFAULT 0,
  started_at REAL, finished_at REAL, exit_code INTEGER,
  result_json TEXT DEFAULT '{}', diff_stat_json TEXT DEFAULT '{}',
  verify_json TEXT DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_attempts_task ON attempts(task_id);
CREATE TABLE IF NOT EXISTS events(
  id INTEGER PRIMARY KEY, attempt_id INTEGER NOT NULL REFERENCES attempts(id),
  seq INTEGER NOT NULL, ts REAL, type TEXT NOT NULL, payload_json TEXT DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_events_attempt ON events(attempt_id, seq);
CREATE TABLE IF NOT EXISTS approvals(
  id INTEGER PRIMARY KEY, attempt_id INTEGER NOT NULL REFERENCES attempts(id),
  tool_name TEXT NOT NULL, input_json TEXT DEFAULT '{}',
  status TEXT NOT NULL DEFAULT 'pending',      -- pending|approved|denied|expired
  decided_by TEXT DEFAULT '', note TEXT DEFAULT '',
  created_at REAL, decided_at REAL
);
CREATE INDEX IF NOT EXISTS idx_approvals_status ON approvals(status);
CREATE TABLE IF NOT EXISTS push_subscriptions(
  id INTEGER PRIMARY KEY, endpoint TEXT UNIQUE NOT NULL, keys_json TEXT NOT NULL,
  created_at REAL
);
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE IF NOT EXISTS memories(
  id INTEGER PRIMARY KEY, project_id INTEGER NOT NULL REFERENCES projects(id),
  note TEXT NOT NULL, created_by_attempt INTEGER, created_at REAL
);
CREATE INDEX IF NOT EXISTS idx_memories_project ON memories(project_id);
"""


def init(path: Path | None = None) -> None:
    global _conn
    p = path or config.DB_PATH
    p.parent.mkdir(parents=True, exist_ok=True)
    _conn = sqlite3.connect(str(p), check_same_thread=False)
    _conn.row_factory = sqlite3.Row
    _conn.execute("PRAGMA journal_mode=WAL")
    _conn.execute("PRAGMA foreign_keys=ON")
    _conn.executescript(SCHEMA)
    # additive migrations for DBs created before these columns existed
    for mig in (
        "ALTER TABLE projects ADD COLUMN verify_cmd TEXT DEFAULT ''",
        "ALTER TABLE projects ADD COLUMN keep_worktrees INTEGER DEFAULT 0",
        "ALTER TABLE attempts ADD COLUMN verify_json TEXT DEFAULT '{}'",
        "ALTER TABLE projects ADD COLUMN review_gate INTEGER DEFAULT 0",
        "ALTER TABLE tasks ADD COLUMN parent_task_id INTEGER",
        "ALTER TABLE tasks ADD COLUMN created_by TEXT DEFAULT 'user'",
        "ALTER TABLE tasks ADD COLUMN created_by_attempt INTEGER",
        "ALTER TABLE attempts ADD COLUMN model TEXT DEFAULT ''",
        "ALTER TABLE attempts ADD COLUMN sandbox_vmid TEXT DEFAULT ''",
        "ALTER TABLE projects ADD COLUMN env_json TEXT DEFAULT '{}'",
    ):
        try:
            _conn.execute(mig)
        except sqlite3.OperationalError:
            pass  # column already exists
    _conn.commit()


def close() -> None:
    global _conn
    if _conn is not None:
        _conn.close()
        _conn = None


def query(sql: str, params: tuple = ()) -> list[dict]:
    with _lock:
        rows = _conn.execute(sql, params).fetchall()
    return [dict(r) for r in rows]


def one(sql: str, params: tuple = ()) -> dict | None:
    rows = query(sql, params)
    return rows[0] if rows else None


def execute(sql: str, params: tuple = ()) -> int:
    """Run a write; returns lastrowid."""
    with _lock:
        cur = _conn.execute(sql, params)
        _conn.commit()
        return cur.lastrowid


def insert(table: str, data: dict) -> int:
    keys = ", ".join(data)
    marks = ", ".join("?" for _ in data)
    return execute(f"INSERT INTO {table}({keys}) VALUES({marks})", tuple(data.values()))


def update(table: str, row_id: int, data: dict) -> None:
    sets = ", ".join(f"{k}=?" for k in data)
    execute(f"UPDATE {table} SET {sets} WHERE id=?", (*data.values(), row_id))


def now() -> float:
    return time.time()


def j(obj) -> str:
    return json.dumps(obj, ensure_ascii=False)


def unj(s: str | None, default=None):
    if not s:
        return default if default is not None else {}
    try:
        return json.loads(s)
    except ValueError:
        return default if default is not None else {}
