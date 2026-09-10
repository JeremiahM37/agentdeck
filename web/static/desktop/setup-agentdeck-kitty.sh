#!/usr/bin/env bash
set -euo pipefail

# This same file is the installer (no arguments) and the installed URI handler.
if (($#)); then
  if (($# != 1)) || [[ ! $1 =~ ^agentdeck://attach/(session|attempt|project|session-shell|attempt-shell)/([1-9][0-9]*)/?$ ]]; then
    echo 'Invalid AgentDeck terminal link' >&2
    exit 2
  fi
  attachment_kind=${BASH_REMATCH[1]}
  attachment_id=${BASH_REMATCH[2]}
  exec kitty --title "AgentDeck · $attachment_kind $attachment_id" \
    --class agentdeck-kitty -o font_size=14 -o scrollback_lines=100000 \
    -o term=xterm-256color \
    ssh -t -o StrictHostKeyChecking=yes agentdeck \
    /usr/local/bin/agentdeck attach "$attachment_kind" "$attachment_id"
fi

command -v kitty >/dev/null
command -v ssh >/dev/null
command -v python3 >/dev/null
command -v xdg-mime >/dev/null

python3 - "$0" <<'PY'
import datetime, os, pathlib, re, shutil
home = pathlib.Path.home()
launcher = home / '.local/bin/agentdeck-kitty'
desktop = home / '.local/share/applications/agentdeck-kitty.desktop'
backup = home / '.local/state/agentdeck' / ('setup-' + datetime.datetime.now().strftime('%Y%m%d-%H%M%S'))
backup.mkdir(parents=True, mode=0o700)
for item in [launcher, desktop, home / '.config/mimeapps.list', home / '.local/share/applications/mimeapps.list']:
    if item.exists():
        dest = backup / item.relative_to(home)
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(item, dest)
launcher.parent.mkdir(parents=True, exist_ok=True)
desktop.parent.mkdir(parents=True, exist_ok=True)
import sys
source = pathlib.Path(sys.argv[1]).resolve()
if source != launcher.resolve():
    shutil.copyfile(source, launcher)
launcher.chmod(0o755)
# Desktop Entry Exec quoting is distinct from shell quoting.
quoted = str(launcher).replace('%', '%%')
for char in ['\\', '"', '`', '$']:
    quoted = quoted.replace(char, '\\' + char)
executable = str(launcher) if re.fullmatch(r'[/a-zA-Z0-9_.-]+', str(launcher)) else '"' + quoted + '"'
desktop.write_text('[Desktop Entry]\nType=Application\nName=AgentDeck Terminal\n'
                   'Comment=Attach an AgentDeck session in Kitty\n'
                   f'Exec={executable} %u\nIcon=kitty\nTerminal=false\n'
                   'NoDisplay=true\nMimeType=x-scheme-handler/agentdeck;\n')
print('Backup:', backup)
PY
xdg-mime default agentdeck-kitty.desktop x-scheme-handler/agentdeck
if command -v update-desktop-database >/dev/null; then
  update-desktop-database "$HOME/.local/share/applications"
fi
echo 'Kitty desktop links installed. Configure the agentdeck SSH alias and test: ssh agentdeck true'
echo 'Then use AgentDeck > Attach > Desktop > Open desktop terminal.'
