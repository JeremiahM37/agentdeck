package sessions

import (
	"strings"
	"testing"
)

func TestForkCommandUsesExactConversationWithoutResumingParent(t *testing.T) {
	for _, name := range []string{"claude", "codex"} {
		spec, _ := Find(Builtins(), name)
		command := spec.LaunchCommand(Start{Workdir: "/tmp/work space", TmuxName: "fork-proof", ForkID: "11111111-1111-4111-8111-111111111111"})
		if strings.Contains(command, "--last") || strings.Contains(command, "--continue") {
			t.Fatal(command)
		}
		if name == "claude" && (!strings.Contains(command, "--fork-session") || !strings.Contains(command, "--resume 11111111")) {
			t.Fatal(command)
		}
		if name == "codex" && !strings.Contains(command, "codex fork 11111111") {
			t.Fatal(command)
		}
	}
}
