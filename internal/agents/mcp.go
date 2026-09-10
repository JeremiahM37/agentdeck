package agents

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// MCPPayload normalizes the project form into the standard Claude document.
func MCPPayload(mcp map[string]any) ([]byte, error) {
	if inner, ok := mcp["mcpServers"]; ok {
		return json.Marshal(map[string]any{"mcpServers": inner})
	}
	return json.Marshal(map[string]any{"mcpServers": mcp})
}

// CodexMCPArgs returns additive -c overrides. It deliberately does not alter
// CODEX_HOME: that directory owns auth, history, instructions and plugins.
func CodexMCPArgs(mcp map[string]any) ([]string, error) {
	if inner, ok := mcp["mcpServers"].(map[string]any); ok {
		mcp = inner
	}
	names := make([]string, 0, len(mcp))
	for name := range mcp {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []string
	for _, name := range names {
		if name == "" || strings.IndexFunc(name, func(r rune) bool {
			return !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') &&
				!(r >= '0' && r <= '9') && r != '_' && r != '-'
		}) >= 0 {
			return nil, fmt.Errorf("MCP server %q cannot be represented by Codex -c; use only letters, digits, '_' or '-'", name)
		}
		cfg, ok := mcp[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("MCP server %q must be an object", name)
		}
		keys := make([]string, 0, len(cfg))
		for key := range cfg {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if key == "type" || key == "transport" {
				continue
			}
			value, err := tomlValue(cfg[key])
			if err != nil {
				return nil, fmt.Errorf("MCP server %q field %q: %w", name, key, err)
			}
			field := key
			if key == "headers" {
				field = "http_headers"
			}
			path := `mcp_servers.` + tomlKey(name) + `.` + tomlKey(field)
			out = append(out, "-c", path+"="+value)
		}
	}
	return out, nil
}

// InteractiveMCPPrepareCommand creates a private, unique runtime leaf after
// checking existing parents without following symlinks.
func InteractiveMCPPrepareCommand(workdir, rel string) string {
	dir := workdir + "/" + strings.TrimSuffix(rel, "/mcp.json")
	parent := workdir + "/.agentdeck/interactive"
	return "for p in " + shellQuote(workdir+"/.agentdeck") + " " + shellQuote(parent) + "; do if [ -L \"$p\" ] || { [ -e \"$p\" ] && [ ! -d \"$p\" ]; }; then exit 73; fi; done; mkdir -p " + shellQuote(parent) + " && mkdir " + shellQuote(dir) + " && chmod 700 " + shellQuote(dir)
}

func InteractiveMCPPublishCommand(workdir, rel string) string {
	dir := workdir + "/" + strings.TrimSuffix(rel, "/mcp.json")
	tmp := dir + "/.mcp.tmp"
	// A hard link is atomic and fails for every existing destination, including
	// symlinks and directories; unlike mv -n it cannot silently publish inside a
	// foreign symlink-to-directory tree.
	return "chmod 600 " + shellQuote(tmp) + " && ln -T " + shellQuote(tmp) + " " + shellQuote(workdir+"/"+rel) + " && rm " + shellQuote(tmp)
}

func tomlKey(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-'
	}) < 0 {
		return s
	}
	return tomlString(s)
}

func tomlValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return tomlString(x), nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	case []any:
		parts := make([]string, len(x))
		for i, item := range x {
			p, err := tomlValue(item)
			if err != nil {
				return "", err
			}
			parts[i] = p
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case []string:
		parts := make([]string, len(x))
		for i, item := range x {
			parts[i] = tomlString(item)
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, key := range keys {
			p, err := tomlValue(x[key])
			if err != nil {
				return "", err
			}
			parts[i] = tomlKey(key) + " = " + p
		}
		return "{" + strings.Join(parts, ", ") + "}", nil
	case map[string]string:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, key := range keys {
			parts[i] = tomlKey(key) + " = " + tomlString(x[key])
		}
		return "{" + strings.Join(parts, ", ") + "}", nil
	default:
		return "", fmt.Errorf("unsupported value type %T", v)
	}
}

// tomlString emits a TOML basic string. strconv.Quote is a Go literal encoder
// and emits escapes such as \a, \v, and \xNN that TOML does not accept.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
