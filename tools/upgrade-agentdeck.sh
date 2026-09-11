#!/usr/bin/env bash
# Prepare an AgentDeck upgrade. Dry-run is the default; --apply never restarts
# the service. The operator reviews the checkpoint and then performs the first
# manual restart separately.
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

echo "unit=$unit"
echo "live_binary=$live"
echo "database=$db"
echo "main_pid=$main"
echo "control_group=$cg"
echo "checkpoint_backup=$backup"
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
sqlite3 "$db" ".backup '$backup/agentdeck.db'"
chmod 600 "$backup/agentdeck.db"
dir=$(dirname "$live")
tmp=$(mktemp "$dir/.agentdeck.new.XXXXXX")
trap 'rm -f "$tmp"' EXIT
install -m 755 "$binary" "$tmp"
mv -fT "$tmp" "$live"
install -d -m 755 "$(dirname "$dropin")"
install -m 644 /dev/stdin "$dropin" <<'EOF'
[Service]
KillMode=process
EOF
systemctl daemon-reload
echo "prepared; service was not restarted"
