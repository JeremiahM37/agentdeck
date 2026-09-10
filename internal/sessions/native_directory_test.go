package sessions

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNativeDirectoryOverrideRemainsAReusableTemplate(t *testing.T) {
	original := []string{"fork", "{id}", "--cd", "old", "-Cother", "--cd=elsewhere", "-C", "again", "--model", "chosen"}
	before := append([]string(nil), original...)
	got := nativeDirectoryArgs(original)
	want := []string{"fork", "{id}", "--model", "chosen", "--cd", "{dir}"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(original, before) {
		t.Fatalf("override changed source settings: %v", got)
	}
	root := t.TempDir()
	// Execute the generated nested shell quoting, with tiny local command
	// stand-ins that only return argv. No coding agent or tmux is invoked.
	for name, script := range map[string]string{
		"tmux":  "#!/bin/sh\nexec bash -c \"$5\"\n",
		"codex": "#!/bin/sh\nprintf '%s\\000' \"$@\"\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(script), 0755); err != nil {
			t.Fatal(err)
		}
	}
	spec := Spec{Name: "codex", Command: "codex", ForkArgs: got, ResumeIDArgs: nativeDirectoryArgs([]string{"resume", "{id}"})}
	for _, leaf := range []string{"first branch", "second's $(touch owned)"} {
		dir := filepath.Join(root, leaf)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		for _, resume := range []bool{false, true} {
			start := Start{Workdir: dir, TmuxName: "test"}
			if resume {
				start.ResumeID = "conversation"
			} else {
				start.ForkID = "conversation"
			}
			cmd := exec.Command("bash", "-c", spec.LaunchCommand(start))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSuffix(string(out), "\000"), "\000")
			expected := []string{"fork", "conversation", "--model", "chosen", "--cd", dir}
			if resume {
				expected = []string{"resume", "conversation", "--cd", dir}
			}
			if !reflect.DeepEqual(args, expected) {
				t.Fatalf("argv=%q want=%q", args, expected)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "owned")); !os.IsNotExist(err) {
		t.Fatal("shell substitution escaped directory")
	}
}
