#!/usr/bin/env bash
# Install the standalone local AgentDeck command.
#
# This uses a separate name only when the SSH client already owns `agentdeck`.
set -euo pipefail

source_dir=""
binary=""
prefix="${XDG_BIN_HOME:-${HOME:?HOME is required}/.local/bin}"
name=""

usage() {
  cat <<'EOF'
Usage: bash tools/install-local.sh [options]

Install the standalone local AgentDeck command from a checkout or binary.
The default command is `agentdeck`; if that name is already installed, the
installer uses `agentdeck-local` so the remote client keeps working.

Options:
  --source DIR    Build from this AgentDeck checkout
  --binary FILE   Install this already-built AgentDeck binary
  --prefix DIR    Install directory (default: ~/.local/bin)
  --name NAME     Command name (default: agentdeck, or agentdeck-local if busy)
  -h, --help      Show this help
EOF
}

while (($#)); do
  case "$1" in
    --source) source_dir=${2:?Missing source directory}; shift 2;;
    --binary) binary=${2:?Missing binary path}; shift 2;;
    --prefix) prefix=${2:?Missing install directory}; shift 2;;
    --name) name=${2:?Missing command name}; shift 2;;
    --help|-h) usage; exit 0;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2;;
  esac
done

case "$(uname -s)" in
  Linux|Darwin) ;;
  MINGW*|MSYS*|CYGWIN*)
    echo 'Run this installer inside WSL2 (Linux), not from Windows Git Bash.' >&2
    echo 'The local runtime uses Linux tmux and a WSL-visible agent CLI.' >&2
    exit 1
    ;;
  *)
    echo "Unsupported host OS: $(uname -s). Use Linux, macOS, or WSL2." >&2
    exit 1
    ;;
esac

for prerequisite in git tmux; do
  command -v "$prerequisite" >/dev/null || {
    echo "Missing prerequisite: $prerequisite (install it before local AgentDeck)." >&2
    exit 1
  }
done

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
if [[ -z $source_dir && -z $binary && -f "$script_dir/../go.mod" ]]; then
  source_dir=$(cd -- "$script_dir/.." && pwd)
fi
if [[ -n $source_dir ]]; then
  source_dir=$(cd -- "$source_dir" && pwd)
  [[ -f "$source_dir/go.mod" ]] || { echo "Not an AgentDeck checkout: $source_dir" >&2; exit 2; }
fi
if [[ -n $binary ]]; then
  [[ -f $binary ]] || { echo "Binary not found: $binary" >&2; exit 2; }
  binary=$(cd -- "$(dirname -- "$binary")" && pwd)/$(basename -- "$binary")
fi
if [[ -n $source_dir && -n $binary ]]; then
  echo 'Choose --source or --binary, not both.' >&2
  exit 2
fi

stage=$(mktemp -d "${TMPDIR:-/tmp}/agentdeck-local.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT
candidate="$stage/agentdeck"

if [[ -n $binary ]]; then
  install -m 755 "$binary" "$candidate"
elif [[ -n $source_dir ]]; then
  command -v go >/dev/null || {
    echo 'Source build needs Go 1.25 or newer; install Go or pass --binary.' >&2
    exit 1
  }
  echo "Building AgentDeck from $source_dir"
  (cd -- "$source_dir" && go build -trimpath -o "$candidate" ./cmd/agentdeck)
else
  echo 'Provide --source PATH or --binary FILE; no release assets are configured.' >&2
  exit 1
fi

[[ -x $candidate ]] || { echo 'The candidate is not executable.' >&2; exit 1; }
if ! "$candidate" version >/dev/null 2>&1; then
  echo 'The candidate did not answer `agentdeck version`; refusing to install it.' >&2
  exit 1
fi
if ! "$candidate" local --help >/dev/null 2>&1; then
  echo 'The candidate must also support `agentdeck local --help`.' >&2
  exit 1
fi

mkdir -p -- "$prefix"
if [[ -z $name ]]; then
  name=agentdeck
  if [[ -e "$prefix/$name" || -L "$prefix/$name" ]]; then
    name=agentdeck-local
  fi
fi
[[ $name =~ ^[a-zA-Z0-9._+-]+$ ]] || { echo 'Invalid command name' >&2; exit 2; }
destination="$prefix/$name"
if [[ -e $destination || -L $destination ]]; then
  backup_dir="${HOME:?HOME is required}/.local/state/agentdeck/local-backups/$(date +%Y%m%d-%H%M%S)"
  mkdir -p -- "$backup_dir"
  cp -p -- "$destination" "$backup_dir/$name"
  echo "Previous $name saved in $backup_dir/$name"
fi
install -m 755 "$candidate" "$destination"
echo "Installed $destination"
echo "Run: $name local"
case ":${PATH:-}:" in
  *":$prefix:"*) ;;
  *) echo "Add $prefix to PATH before using $name.";;
esac
