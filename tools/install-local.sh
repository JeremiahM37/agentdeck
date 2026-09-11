#!/usr/bin/env bash
# Install the standalone local AgentDeck command.
#
# This intentionally installs a separate name (agentdeck-local) so a machine
# that already uses the SSH client installer keeps its `agentdeck` launcher.
set -euo pipefail

repo="JeremiahM37/agentdeck"
source_dir=""
binary=""
release_tag=""
prefix="${XDG_BIN_HOME:-${HOME:?HOME is required}/.local/bin}"
name="agentdeck-local"

usage() {
  cat <<'EOF'
Usage: bash tools/install-local.sh [options]

Install the standalone local AgentDeck command. From a checkout, the default
is a source build. Without a checkout, the installer looks for an exact
architecture-matched release asset and otherwise explains how to provide one.

Options:
  --source DIR    Build from this AgentDeck checkout
  --binary FILE   Install this already-built AgentDeck binary
  --version TAG   Use this exact GitHub release tag when fetching an asset
  --repo OWNER/REPO  Release repository (default: JeremiahM37/agentdeck)
  --prefix DIR    Install directory (default: ~/.local/bin)
  --name NAME     Command name (default: agentdeck-local)
  -h, --help      Show this help
EOF
}

while (($#)); do
  case "$1" in
    --source) source_dir=${2:?Missing source directory}; shift 2;;
    --binary) binary=${2:?Missing binary path}; shift 2;;
    --version) release_tag=${2:?Missing release tag}; shift 2;;
    --repo) repo=${2:?Missing repository}; shift 2;;
    --prefix) prefix=${2:?Missing install directory}; shift 2;;
    --name) name=${2:?Missing command name}; shift 2;;
    --help|-h) usage; exit 0;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2;;
  esac
done

[[ $name =~ ^[a-zA-Z0-9._+-]+$ ]] || { echo 'Invalid command name' >&2; exit 2; }
[[ $repo =~ ^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$ ]] || { echo 'Invalid repository' >&2; exit 2; }
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

platform=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
  x86_64|amd64) arch=amd64;;
  aarch64|arm64) arch=arm64;;
  armv7l|armv7) arch=armv7;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1;;
esac

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
  command -v curl >/dev/null || {
    echo 'No checkout or binary supplied, and curl is unavailable for release lookup.' >&2
    exit 1
  }
  command -v python3 >/dev/null || {
    echo 'No checkout or binary supplied. Install Python 3 for release lookup, or pass --source/--binary.' >&2
    exit 1
  }
  asset="agentdeck-${platform}-${arch}"
  [[ $release_tag == v* || -z $release_tag ]] || release_tag="v$release_tag"
  if [[ -n $release_tag ]]; then
    release_url="https://api.github.com/repos/$repo/releases/tags/$release_tag"
  else
    release_url="https://api.github.com/repos/$repo/releases/latest"
  fi
  release_json=$(curl -fsSL --retry 2 -- "$release_url") || {
    echo "Could not read release metadata from $release_url." >&2
    echo 'Provide --source PATH or --binary FILE for an offline/source install.' >&2
    exit 1
  }
  asset_url=$(python3 -c 'import json,sys
data=json.load(sys.stdin)
want=sys.argv[1]
for asset in data.get("assets", []):
    if asset.get("name") in (want, want+".tar.gz"):
        print(asset.get("browser_download_url", ""))
        break
' "$asset" <<<"$release_json")
  if [[ -z $asset_url ]]; then
    tag=$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("tag_name", "unknown"))' <<<"$release_json")
    echo "Release $tag has no exact asset $asset for $platform/$arch." >&2
    echo 'No binary was downloaded. Use --source PATH or --binary FILE.' >&2
    exit 1
  fi
  downloaded="$stage/download"
  curl -fL --retry 2 -- "$asset_url" -o "$downloaded"
  if [[ $asset_url == *.tar.gz ]]; then
    tar -xOzf "$downloaded" "agentdeck" > "$candidate"
    chmod 755 "$candidate"
  else
    install -m 755 "$downloaded" "$candidate"
  fi
fi

[[ -x $candidate ]] || { echo 'The candidate is not executable.' >&2; exit 1; }
if ! "$candidate" version >/dev/null 2>&1; then
  echo 'The candidate did not answer `agentdeck version`; refusing to install it.' >&2
  exit 1
fi

mkdir -p -- "$prefix"
destination="$prefix/$name"
if [[ -e $destination || -L $destination ]]; then
  backup_dir="${HOME:?HOME is required}/.local/state/agentdeck/local-backups/$(date +%Y%m%d-%H%M%S)"
  mkdir -p -- "$backup_dir"
  cp -p -- "$destination" "$backup_dir/$name"
  echo "Previous $name saved in $backup_dir/$name"
fi
install -m 755 "$candidate" "$destination"
echo "Installed $destination"
echo "Run: $name local <your-agent-command>"
case ":${PATH:-}:" in
  *":$prefix:"*) ;;
  *) echo "Add $prefix to PATH before using $name.";;
esac
