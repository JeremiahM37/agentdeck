package sessions

// Sessions created by earlier versions may contain these built-in helper bodies
// in their private launch snapshots. Upgrade only exact known built-ins;
// operator-defined trust commands keep their declared behavior.
const legacyClaudeTrust = `python3 - {dir} <<'ADKTRUST'
import json, os, sys, tempfile
path = os.path.expanduser("~/.claude.json")
try:
    with open(path) as f:
        doc = json.load(f)
except Exception:
    doc = {}
entry = doc.setdefault("projects", {}).setdefault(sys.argv[1], {})
if entry.get("hasTrustDialogAccepted") is not True:
    entry["hasTrustDialogAccepted"] = True
    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path) or ".")
    with os.fdopen(fd, "w") as f:
        json.dump(doc, f, indent=2)
    os.replace(tmp, path)
ADKTRUST`

const legacyCodexTrust = `f="$HOME/.codex/config.toml"; mkdir -p "$(dirname "$f")"; touch "$f"; grep -qF "[projects.\"{dir_raw}\"]" "$f" || printf '\n[projects."%s"]\ntrust_level = "trusted"\n' {dir} >> "$f"`

func currentTrustCommand(command string) string {
	switch command {
	case legacyClaudeTrust:
		return claudeTrust
	case legacyCodexTrust:
		return codexTrust
	default:
		return command
	}
}
