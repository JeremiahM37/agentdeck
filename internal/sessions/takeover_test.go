package sessions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExactResumeQuotesConversationID(t *testing.T) {
	// Execute the generated shell syntax with a fake tmux, so a hostile ID is
	// proven to remain one literal argument all the way into the nested command.
	dir := t.TempDir()
	bin := filepath.Join(dir, "tmux")
	os.WriteFile(bin, []byte("#!/bin/sh\nwhile [ \"$#\" -gt 0 ] && [ \"$1\" != -- ]; do shift; done\n[ \"$1\" = -- ] && shift\nexec \"$@\"\n"), 0755)
	for _, agent := range []string{"claude", "codex"} {
		spec, _ := Find(Builtins(), agent)
		spec.Command = "printf '%s\\n'"
		id := "thread with 'quotes'; $(touch owned)"
		cmd := spec.LaunchCommand(Start{Workdir: dir, TmuxName: "test", Resume: true, ResumeID: id})
		// The trailing interactive shell is not needed to inspect the emitted args.
		cmd = strings.Replace(cmd, "; exec bash", "", 1)
		c := exec.Command("sh", "-c", cmd)
		c.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("launch: %s %v", out, err)
		}
		if !strings.Contains(string(out), id) || strings.Contains(string(out), "--last") || strings.Contains(string(out), "--continue") {
			t.Fatalf("wrong resume args: %s", out)
		}
		if _, err := os.Stat(filepath.Join(dir, "owned")); !os.IsNotExist(err) {
			t.Fatal("conversation ID executed as code")
		}
	}
}
