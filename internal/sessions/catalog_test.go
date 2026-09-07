package sessions

import (
	"strings"
	"testing"
)

// The model catalog is the agent's shape, not ours, and it will change without
// telling us. Parsing has to be defensive: a bad read must yield no suggestions,
// never a wrong list and never a panic.

// realCodexCatalog is trimmed from the actual `codex debug models` output.
const realCodexCatalog = `{"models":[
{"slug":"gpt-6-astra","display_name":"GPT-6-Astra","description":"Our most capable model.",
 "visibility":"list","supported_in_api":true,"priority":1,
 "supported_reasoning_levels":[{"effort":"low"},{"effort":"max"}]},
{"slug":"gpt-reserve","display_name":"reserve","visibility":"hide","priority":3},
{"slug":"gpt-5.6-sol","display_name":"Sol","visibility":"list","priority":6},
{"slug":"gpt-5.3-codex-spark","display_name":"Spark","visibility":"list","supported_in_api":false}
]}`

func TestParsesTheRealCodexCatalog(t *testing.T) {
	got := ParseModelCatalog(realCodexCatalog)
	want := []string{"gpt-6-astra", "gpt-5.6-sol", "gpt-5.3-codex-spark"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
	// the model the picker hides is one its own UI does not offer either
	for _, m := range got {
		if m == "gpt-reserve" {
			t.Error("a hidden model was suggested")
		}
	}
}

func TestCatalogParsingIsDefensive(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":            "",
		"not json":         "command not found",
		"an error object":  `{"error":"unauthorized"}`,
		"null":             "null",
		"an empty catalog": `{"models":[]}`,
		"wrong types":      `{"models":[1,2,{"slug":null}]}`,
		"deeply nested":    `{"a":{"b":{"c":{"d":{"models":[{"slug":"x"}]}}}}}`,
	} {
		got := ParseModelCatalog(raw)
		if len(got) != 0 {
			t.Errorf("%s: expected no suggestions, got %v", name, got)
		}
	}
}

// Other CLIs will not use codex's shape, so the common ones are read too.
func TestCatalogAcceptsOtherPlausibleShapes(t *testing.T) {
	for name, tc := range map[string]struct{ raw, want string }{
		"a bare array of strings": {`["a-model","b-model"]`, "a-model,b-model"},
		"objects with id":         {`[{"id":"m1"},{"id":"m2"}]`, "m1,m2"},
		"objects with name":       {`{"data":[{"name":"n1"}]}`, "n1"},
		"duplicates collapse":     {`["x","x","y"]`, "x,y"},
	} {
		if got := strings.Join(ParseModelCatalog(tc.raw), ","); got != tc.want {
			t.Errorf("%s: got %q want %q", name, got, tc.want)
		}
	}
}

// The probe command is built from the resolved binary, so an agent installed
// somewhere unusual is still asked at the path it actually lives at.
func TestModelsProbeUsesTheResolvedBinary(t *testing.T) {
	l := Launcher{CodexBin: "/home/admin/.local/bin/codex"}
	var codex Spec
	for _, s := range Builtins() {
		if s.Name == "codex" {
			codex = s
		}
	}
	if codex.ModelsCommand == "" {
		t.Fatal("codex can be asked for its models; it should say so")
	}
	got := l.resolve(codex).ModelsProbe()
	if got != "/home/admin/.local/bin/codex debug models" {
		t.Errorf("probe: %q", got)
	}
	// an agent with nothing to ask must produce no command at all, not a
	// half-built one that would run the wrong thing
	if probe := (Spec{Name: "claude", Command: "claude"}).ModelsProbe(); probe != "" {
		t.Errorf("expected no probe, got %q", probe)
	}
}
