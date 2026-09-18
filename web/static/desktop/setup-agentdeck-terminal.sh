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
  command=(env TERM=xterm-256color ssh -t -o StrictHostKeyChecking=yes agentdeck
    /usr/local/bin/agentdeck --hosted-attach attach "$attachment_kind" "$attachment_id")
  # Ask the desktop first. No terminal brand, theme, font or scrollback override.
  if command -v xdg-terminal-exec >/dev/null; then
    exec xdg-terminal-exec -- "${command[@]}"
  fi
  if command -v x-terminal-emulator >/dev/null; then
    exec x-terminal-emulator -e "${command[@]}"
  fi
  terminal=${TERMINAL:-}
  # Minimal window managers may provide neither default-terminal helper.
  # Use the terminal already installed, without requiring a particular product.
  if [[ -z $terminal ]]; then
    for candidate in foot kitty alacritty ghostty konsole gnome-terminal kgx xfce4-terminal mate-terminal wezterm xterm; do
      if command -v "$candidate" >/dev/null; then terminal=$candidate; break; fi
    done
  fi
  if [[ -z $terminal ]] || ! command -v "$terminal" >/dev/null; then
    echo 'No desktop terminal found. Install your desktop default-terminal helper (xdg-terminal-exec), or set TERMINAL to your terminal executable.' >&2
    exit 1
  fi
  case "${terminal##*/}" in
    kitty|foot) exec "$terminal" -- "${command[@]}" ;;
    gnome-terminal|kgx) exec "$terminal" -- "${command[@]}" ;;
    xfce4-terminal|mate-terminal) exec "$terminal" -x "${command[@]}" ;;
    wezterm) exec "$terminal" start -- "${command[@]}" ;;
    *) exec "$terminal" -e "${command[@]}" ;;
  esac
fi

command -v ssh >/dev/null
command -v python3 >/dev/null
command -v xdg-mime >/dev/null

python3 - "$0" <<'PY'
import datetime, os, pathlib, re, shutil
home = pathlib.Path.home()
launcher = home / '.local/bin/agentdeck-terminal'
desktop = home / '.local/share/applications/agentdeck-terminal.desktop'
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
                   'Comment=Attach an AgentDeck session in your terminal\n'
                   f'Exec={executable} %u\nIcon=utilities-terminal\nTerminal=false\n'
                   'NoDisplay=true\nMimeType=x-scheme-handler/agentdeck;\n')
print('Backup:', backup)
PY
xdg-mime default agentdeck-terminal.desktop x-scheme-handler/agentdeck
if command -v update-desktop-database >/dev/null; then
  update-desktop-database "$HOME/.local/share/applications"
fi
echo 'Desktop terminal links installed. Configure the agentdeck SSH alias and test: ssh agentdeck true'
echo 'Then use AgentDeck > Attach > Desktop > Open in terminal.'
