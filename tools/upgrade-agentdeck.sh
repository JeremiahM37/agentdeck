#!/usr/bin/env bash
# Prepare an AgentDeck upgrade. Dry-run is the default; --apply never restarts
# the service. The old database is exported before it can be migrated by the
# candidate, and the operator performs the first manual restart separately.
set -euo pipefail

apply=0
binary=""
while (($#)); do
  case "$1" in
    --apply) apply=1 ;;
    --binary) shift; binary=${1:?--binary requires a path} ;;
    -h|--help) sed -n '1,8p' "$0"; exit 0 ;;
    *) echo "usage: $0 [--apply] --binary PATH" >&2; exit 2 ;;
  esac
  shift
done
[[ -n "$binary" && -f "$binary" && -x "$binary" ]] || { echo "binary must be an executable file" >&2; exit 2; }

unit=agentdeck
live=$(systemctl show "$unit" -p ExecStart --value | sed -n 's/.*path=\([^ ;]*\).*/\1/p')
[[ -n "$live" ]] || live=/usr/local/bin/agentdeck
db=$(systemctl show "$unit" -p Environment --value | tr ' ' '\n' | sed -n 's/^AGENTDECK_DB=//p' | head -1)
[[ -n "$db" ]] || { echo "AGENTDECK_DB is not present in the unit environment" >&2; exit 1; }
main=$(systemctl show "$unit" -p MainPID --value)
cg=$(systemctl show "$unit" -p ControlGroup --value)
dropin=/etc/systemd/system/${unit}.service.d/10-session-persistence.conf
backup=/home/admin/backups/agentdeck-pre-upgrade-$(date +%Y%m%d-%H%M%S)
checkpoint="$backup/session-checkpoint.json"

echo "unit=$unit"
echo "live_binary=$live"
echo "database=$db"
echo "main_pid=$main"
echo "control_group=$cg"
echo "checkpoint_backup=$backup"
echo "checkpoint_manifest=$checkpoint"
if [[ -r /proc/$main/exe ]]; then
  printf 'main_exe='; readlink /proc/$main/exe
  printf 'main_starttime='; awk '{print $22}' /proc/$main/stat
fi
if [[ -r "/sys/fs/cgroup$cg/cgroup.procs" ]]; then
  while read -r pid; do
    [[ "$pid" == "$main" ]] && continue
    [[ -r /proc/$pid/exe ]] || continue
    printf 'owned_pid=%s exe=' "$pid"; readlink /proc/$pid/exe
    printf 'owned_starttime=%s\n' "$(awk '{print $22}' "/proc/$pid/stat")"
  done <"/sys/fs/cgroup$cg/cgroup.procs"
fi

if (( ! apply )); then
  echo "dry-run: no files, database, daemon state, or service state changed"
  exit 0
fi

install -d -m 700 "$backup"
# SQLite's online backup API gives us a consistent source copy while the live
# service is still running. A byte copy of a WAL database is not a backup.
python3 - "$db" "$backup/agentdeck.db" <<'PY'
import sqlite3, sys
src, dst = sys.argv[1:]
source = sqlite3.connect("file:" + src + "?mode=ro", uri=True)
target = sqlite3.connect(dst)
try:
    source.backup(target)
finally:
    target.close()
    source.close()
PY
chmod 600 "$backup/agentdeck.db"
cp -a "$live" "$backup/old-agentdeck"
if [[ -f "$dropin" ]]; then
  cp -a "$dropin" "$backup/old-session-persistence.conf"
else
  : >"$backup/old-session-persistence.conf.absent"
  chmod 600 "$backup/old-session-persistence.conf.absent"
fi

# Run the candidate against the old schema. The exporter opens the DB read
# only, probes each real target, and writes an atomic private manifest. No
# store.Open occurs on this path, so migrations cannot run before capture.
baseline_main_pid=$(systemctl show "$unit" -p MainPID --value)
baseline_main_starttime=""
if [[ "$baseline_main_pid" =~ ^[0-9]+$ && -r "/proc/$baseline_main_pid/stat" ]]; then
  baseline_main_starttime=$(awk '{print $22}' "/proc/$baseline_main_pid/stat")
fi
[[ -n "$baseline_main_starttime" ]] || {
  echo "cannot establish agentdeck MainPID starttime; refusing upgrade" >&2
  exit 1
}

# Local/sandbox sessions have pane processes in this namespace. Remote target
# panes are checked by the candidate's target executor; retaining these local
# starttimes catches a pane replacement while the export is in progress.
declare -A baseline_panes=()
if command -v tmux >/dev/null 2>&1; then
  while IFS= read -r tmux_name; do
    [[ -n "$tmux_name" ]] || continue
    while IFS= read -r pane_pid; do
      [[ "$pane_pid" =~ ^[0-9]+$ && -r "/proc/$pane_pid/stat" ]] || continue
      pane_starttime=$(awk '{print $22}' "/proc/$pane_pid/stat")
      [[ -n "$pane_starttime" ]] && baseline_panes["$pane_pid"]="$pane_starttime"
    done < <(env -u TMUX tmux list-panes -t "=$tmux_name" -F '#{pane_pid}' 2>/dev/null || true)
  done < <(python3 - "$db" <<'PY'
import sqlite3, sys
db = sqlite3.connect("file:" + sys.argv[1] + "?mode=ro", uri=True)
cols = {row[1] for row in db.execute("PRAGMA table_info(sessions)")}
where = ["s.ended_at IS NULL"] if "ended_at" in cols else ["1"]
if "archived_at" in cols:
    where.append("s.archived_at IS NULL")
for (name,) in db.execute("SELECT s.tmux_session FROM sessions s JOIN targets t ON t.id=s.target_id WHERE " + " AND ".join(where) + " AND t.kind IN ('local','sandbox')"):
    if name:
        print(name)
db.close()
PY
  )
fi

env -u TMUX AGENTDECK_DB="$db" "$binary" recovery-checkpoint export "$checkpoint"

current_main_pid=$(systemctl show "$unit" -p MainPID --value)
current_main_starttime=""
if [[ "$current_main_pid" =~ ^[0-9]+$ && -r "/proc/$current_main_pid/stat" ]]; then
  current_main_starttime=$(awk '{print $22}' "/proc/$current_main_pid/stat")
fi
if [[ "$current_main_pid" != "$baseline_main_pid" || "$current_main_starttime" != "$baseline_main_starttime" ]]; then
  echo "agentdeck service process changed during checkpoint export; refusing upgrade" >&2
  exit 1
fi
for pane_pid in "${!baseline_panes[@]}"; do
  current_pane_starttime=""
  if [[ -r "/proc/$pane_pid/stat" ]]; then
    current_pane_starttime=$(awk '{print $22}' "/proc/$pane_pid/stat")
  fi
  if [[ "$current_pane_starttime" != "${baseline_panes[$pane_pid]}" ]]; then
    echo "AgentDeck pane process $pane_pid changed during checkpoint export; refusing upgrade" >&2
    exit 1
  fi
done

# Replacement is unsafe unless every live source row has a native identity.
# Keep this check outside the Go exporter so the rollout tool has an explicit,
# reviewable stop condition and reports the exact uncovered rows.
python3 - "$checkpoint" "$db" <<'PY'
import json, sqlite3, sys
manifest, dbpath = sys.argv[1:]
with open(manifest) as f:
    data = json.load(f)
states = {int(s["id"]): s for s in data.get("sessions", [])}
conn = sqlite3.connect("file:" + dbpath + "?mode=ro", uri=True)
columns = {row[1] for row in conn.execute("PRAGMA table_info(sessions)")}
where = ["ended_at IS NULL"]
if "archived_at" in columns:
    where.append("archived_at IS NULL")
rows = conn.execute("SELECT id FROM sessions WHERE " + " AND ".join(where)).fetchall()
conn.close()
ids = {int(r[0]) for r in rows}
missing = sorted(ids - set(states))
unknown = sorted(i for i in ids if i in states and (states[i].get("identity_state") == "unknown" or not states[i].get("native_recovery_cid")))
if missing or unknown or set(states) != ids:
    print("unsupported checkpoint coverage; candidate binary was not installed", file=sys.stderr)
    if missing: print("missing session ids: " + ",".join(map(str, missing)), file=sys.stderr)
    if unknown: print("unknown native identity session ids: " + ",".join(map(str, unknown)), file=sys.stderr)
    extra = sorted(set(states) - ids)
    if extra: print("manifest has no-longer-live session ids: " + ",".join(map(str, extra)), file=sys.stderr)
    raise SystemExit(1)
print("checkpoint coverage: " + str(len(ids)) + " live sessions")
PY

dir=$(dirname "$live")
tmp=$(mktemp "$dir/.agentdeck.new.XXXXXX")
trap 'rm -f "$tmp"' EXIT
install -m 755 "$binary" "$tmp"
mv -fT "$tmp" "$live"
install -d -m 755 "$(dirname "$dropin")"
install -m 644 /dev/stdin "$dropin" <<EOF
[Service]
KillMode=process
Environment=AGENTDECK_CHECKPOINT=$checkpoint
EOF
systemctl daemon-reload
echo "prepared; checkpoint captured and binary installed; service was not restarted"
echo "refresh checkpoint before the first manual restart if sessions changed: env -u TMUX AGENTDECK_DB=$db $live recovery-checkpoint export $checkpoint"
