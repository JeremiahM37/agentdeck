package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

var (
	initLine = `{"type":"system","subtype":"init","session_id":"s-42","model":"claude-opus","tools":["Bash","Edit"]}`
	textLine = `{"type":"assistant","message":{"content":[{"type":"text","text":"Working on it."}]}}`
	toolLine = `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"ls"}}]}}`
	resLine  = `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":[{"type":"text","text":"file1\nfile2"}],"is_error":false}]}}`
	doneLine = `{"type":"result","subtype":"success","total_cost_usd":0.5,"duration_ms":1000,"num_turns":4,"result":"done","session_id":"s-42"}`
)

func types(events []Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event types: got %v want %v", got, want)
	}
}

func TestFullClaudeStream(t *testing.T) {
	buf := strings.Join([]string{initLine, textLine, toolLine, resLine, doneLine}, "\n") + "\n"
	events, rem := ParseStreamLines("claude", buf)
	if rem != "" {
		t.Fatalf("remainder should be empty, got %q", rem)
	}
	eq(t, types(events), []string{"init", "text", "tool_use", "tool_result", "result"})
	if events[0].Payload["session_id"] != "s-42" {
		t.Errorf("session id: %v", events[0].Payload["session_id"])
	}
	input := events[2].Payload["input"].(map[string]any)
	if input["command"] != "ls" {
		t.Errorf("tool input: %v", input)
	}
	if !strings.Contains(events[3].Payload["content"].(string), "file1") {
		t.Errorf("tool result: %v", events[3].Payload["content"])
	}
	if events[4].Payload["cost_usd"] != 0.5 {
		t.Errorf("cost: %v", events[4].Payload["cost_usd"])
	}
}

func TestPartialLineIsBuffered(t *testing.T) {
	events, rem := ParseStreamLines("claude", initLine+"\n"+textLine[:20])
	eq(t, types(events), []string{"init"})
	if rem != textLine[:20] {
		t.Fatalf("remainder %q", rem)
	}
}

func TestNoNewlineBuffersEverything(t *testing.T) {
	events, rem := ParseStreamLines("claude", initLine[:30])
	if len(events) != 0 || rem != initLine[:30] {
		t.Fatalf("got %v / %q", events, rem)
	}
}

func TestMalformedJSONSurvives(t *testing.T) {
	events, _ := ParseStreamLines("claude", "this is not json\n"+initLine+"\n")
	eq(t, types(events), []string{"raw", "init"})
}

func TestHousekeepingNoiseDropped(t *testing.T) {
	lines := []string{
		`{"type":"rate_limit_event","rate_limit_info":{}}`,
		`{"data":{"type":"rate_limit_event"},"uuid":"u1"}`,
		`{"type":"system","subtype":"compact_boundary"}`,
	}
	events, _ := ParseStreamLines("claude", strings.Join(lines, "\n")+"\n")
	if len(events) != 0 {
		t.Fatalf("protocol bookkeeping must not reach the timeline: %v", types(events))
	}
}

func TestUnknownTypeKeptRaw(t *testing.T) {
	events, _ := ParseStreamLines("claude", `{"type":"hologram","x":1}`+"\n")
	eq(t, types(events), []string{"raw"})
}

func TestEmptyTextBlocksSkipped(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"  "}]}}`
	events, _ := ParseStreamLines("claude", line+"\n")
	if len(events) != 0 {
		t.Fatalf("whitespace-only text is not a timeline entry: %v", events)
	}
}

// codexStream was captured from a REAL `codex exec --json` run (codex-cli
// 0.147.0). Synthetic fixtures hid two things: item.started exists, and codex
// prints a plain-text banner before its JSONL.
var codexStream = strings.Join([]string{
	"Reading additional input from stdin...",
	`{"type":"thread.started","thread_id":"th_1"}`,
	`{"type":"turn.started"}`,
	`{"type":"item.completed","item":{"type":"agent_message","id":"m1","text":"Planning the change."}}`,
	`{"type":"item.started","item":{"type":"command_execution","id":"c1","command":"pytest -q","aggregated_output":"","exit_code":null,"status":"in_progress"}}`,
	`{"type":"item.completed","item":{"type":"command_execution","id":"c1","command":"pytest -q","aggregated_output":"3 passed","exit_code":0,"status":"completed"}}`,
	`{"type":"item.started","item":{"type":"file_change","id":"f1","status":"in_progress","changes":[{"path":"app.py","kind":"update"}]}}`,
	`{"type":"item.completed","item":{"type":"file_change","id":"f1","status":"completed","changes":[{"path":"app.py","kind":"update"}]}}`,
	`{"type":"turn.completed","usage":{"output_tokens":420}}`,
}, "\n") + "\n"

func TestCodexParserMapsRealThreadEvents(t *testing.T) {
	events, rem := ParseStreamLines("codex", codexStream)
	if rem != "" {
		t.Fatalf("remainder %q", rem)
	}
	// the stdin banner is dropped, not surfaced as a junk 'raw' card
	eq(t, types(events), []string{"init", "text", "tool_use", "tool_result",
		"tool_use", "tool_result", "result"})
	if events[0].Payload["session_id"] != "th_1" {
		t.Errorf("thread id: %v", events[0].Payload["session_id"])
	}
	// tool_use comes from item.started, so the command shows while it runs
	if events[2].Payload["input"].(map[string]any)["command"] != "pytest -q" {
		t.Errorf("in-flight command: %v", events[2].Payload)
	}
	if events[3].Payload["content"] != "3 passed" {
		t.Errorf("output: %v", events[3].Payload["content"])
	}
	if events[3].Payload["is_error"] != false {
		t.Errorf("exit 0 is not an error")
	}
	if !strings.Contains(events[4].Payload["input"].(map[string]any)["file_path"].(string), "app.py") {
		t.Errorf("file change: %v", events[4].Payload)
	}
	if events[6].Payload["tokens"] != float64(420) {
		t.Errorf("tokens: %v", events[6].Payload["tokens"])
	}
}

func TestCodexFailedCommandMarksError(t *testing.T) {
	line := `{"type":"item.completed","item":{"type":"command_execution","id":"c9","command":"pytest","aggregated_output":"1 failed","exit_code":1,"status":"completed"}}`
	events, _ := ParseStreamLines("codex", line+"\n")
	if events[0].Payload["is_error"] != true {
		t.Fatal("a non-zero exit must read as an error")
	}
}

func TestCodexInflightCommandIsNotAnError(t *testing.T) {
	// exit_code is null while running — treating that as failure once marked
	// every in-flight command red
	line := `{"type":"item.started","item":{"type":"command_execution","id":"c9","command":"sleep 1","exit_code":null,"status":"in_progress"}}`
	events, _ := ParseStreamLines("codex", line+"\n")
	eq(t, types(events), []string{"tool_use"})
}

func TestCodexParserToleratesGarbage(t *testing.T) {
	events, _ := ParseStreamLines("codex", "not json\n"+`{"type":"mystery"}`+"\n")
	eq(t, types(events), []string{"raw", "raw"})
}

func TestGeminiPlaintextLinesBecomeTimeline(t *testing.T) {
	events, rem := ParseStreamLines("gemini", "Reading files...\n\nDone, updated app.py\npartial")
	if len(events) != 2 ||
		events[0].Payload["text"] != "Reading files..." ||
		events[1].Payload["text"] != "Done, updated app.py" {
		t.Fatalf("events: %+v", events)
	}
	if rem != "partial" {
		t.Fatalf("remainder %q", rem)
	}
}

func TestTruncateBoundsAPayload(t *testing.T) {
	big := map[string]any{"blob": strings.Repeat("x", 5000)}
	out := truncate(big, 2000)
	raw, _ := json.Marshal(out)
	if len(raw) > 2100 {
		t.Fatalf("oversized payload was not bounded: %d bytes", len(raw))
	}
	if _, ok := out.(map[string]any)["_truncated"]; !ok {
		t.Fatalf("truncation must be visible, got %v", out)
	}
}
