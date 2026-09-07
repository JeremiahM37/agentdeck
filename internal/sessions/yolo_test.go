package sessions

import (
	"strings"
	"testing"
)

// Yolo is on by default for interactive sessions, which makes it worth being
// precise about: each CLI spells it differently, and passing the wrong flag
// either fails to launch or — worse — silently leaves the prompts on.

func TestYoloUsesEachAgentsOwnFlag(t *testing.T) {
	want := map[string]string{
		"claude": "--permission-mode bypassPermissions",
		"codex":  "--dangerously-bypass-approvals-and-sandbox",
		"gemini": "--yolo",
	}
	for _, spec := range Builtins() {
		cmd := spec.LaunchCommand(Start{Workdir: "/r", TmuxName: "s", Yolo: true})
		if !strings.Contains(cmd, want[spec.Name]) {
			t.Errorf("%s yolo: wanted %q in\n  %s", spec.Name, want[spec.Name], cmd)
		}
	}
}

func TestYoloOffMeansTheAgentStillAsks(t *testing.T) {
	for _, spec := range Builtins() {
		cmd := spec.LaunchCommand(Start{Workdir: "/r", TmuxName: "s", Yolo: false})
		for _, flag := range spec.YoloArgs {
			if strings.Contains(cmd, flag) {
				t.Errorf("%s: %q leaked in with yolo off:\n  %s", spec.Name, flag, cmd)
			}
		}
	}
}

// An agent the operator defined that has no such mode must not be handed a flag
// it does not understand — that would fail to launch at all.
func TestAnAgentWithoutAYoloModeIsNotGivenOne(t *testing.T) {
	spec := Spec{Name: "custom", Command: "mytool"}
	cmd := spec.LaunchCommand(Start{Workdir: "/r", TmuxName: "s", Yolo: true})
	if strings.Contains(cmd, "--yolo") || strings.Contains(cmd, "bypass") {
		t.Errorf("a flag was invented for an agent that has no yolo mode: %s", cmd)
	}
	if !strings.Contains(cmd, "mytool") {
		t.Errorf("the agent should still launch: %s", cmd)
	}
}

// Yolo and resume are independent axes and must compose.
func TestYoloComposesWithResumeAndModel(t *testing.T) {
	claude, _ := Find(Builtins(), "claude")
	cmd := claude.LaunchCommand(Start{Workdir: "/r", TmuxName: "s",
		Model: "opus", Resume: true, Yolo: true})
	for _, want := range []string{"--continue", "bypassPermissions", "--model opus"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %q in\n  %s", want, cmd)
		}
	}
}
