#!/usr/bin/env bash
# Install the Linux client from a server you already trust through SSH.
set -euo pipefail
server=agentdeck
api=http://localhost:9110
while (($#)); do
  case "$1" in
    --server) server=${2:?Missing SSH alias}; shift 2;;
    --api) api=${2:?Missing API URL}; shift 2;;
    --help|-h) echo 'Usage: bash install-agentdeck-cli.sh --server SSH_ALIAS --api https://SERVER:PORT'; exit 0;;
    *) echo "Unknown option: $1" >&2; exit 2;;
  esac
done
[[ $server =~ ^[a-zA-Z0-9_@.:-]+$ && $server != -* ]] || { echo 'Invalid SSH alias' >&2; exit 2; }
[[ $api == http://* || $api == https://* ]] || { echo 'API must be an http(s) URL' >&2; exit 2; }
[[ $(uname -s) == Linux ]] || { echo 'This installer is for Linux. Use install-agentdeck-cli.ps1 on Windows.' >&2; exit 1; }
command -v ssh >/dev/null
command -v scp >/dev/null
remote_platform=$(ssh "$server" 'uname -s; uname -m')
[[ "$remote_platform" == "$(uname -s)"$'\n'"$(uname -m)" ]] || { echo 'Server/client architectures differ. Build agentdeck for your client architecture first.' >&2; exit 1; }
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
scp -q "$server:/usr/local/bin/agentdeck" "$stage/client"
chmod 700 "$stage/client"
"$stage/client" console --help >/dev/null
mkdir -p "$HOME/.local/bin" "$HOME/.local/lib/agentdeck"
# Preserve the previous client and launcher for rollback.
backup="$HOME/.local/state/agentdeck/cli-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$backup"
for file in "$HOME/.local/bin/agentdeck" "$HOME/.local/lib/agentdeck/client"; do
  [[ ! -f $file ]] || cp -p "$file" "$backup/$(basename "$file")"
done
install -m 755 "$stage/client" "$HOME/.local/lib/agentdeck/client.next"
mv "$HOME/.local/lib/agentdeck/client.next" "$HOME/.local/lib/agentdeck/client"
{
  echo '#!/usr/bin/env bash'
  printf 'export AGENTDECK_API=${AGENTDECK_API:-%q}\n' "$api"
  printf 'export AGENTDECK_ATTACH_HOST=${AGENTDECK_ATTACH_HOST:-%q}\n' "$server"
  echo 'if (($# == 0)); then set -- console; fi'
  printf 'exec %q "$@"\n' "$HOME/.local/lib/agentdeck/client"
} > "$stage/launcher"
install -m 755 "$stage/launcher" "$HOME/.local/bin/agentdeck"
echo 'Installed. Run agentdeck for the console, or agentdeck --help for commands.'
case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) echo 'Add ~/.local/bin to your shell PATH.';; esac
