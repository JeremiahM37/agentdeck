package executor

import (
	"context"
	"strings"
	"testing"
)

func TestPctWrapQuoting(t *testing.T) {
	cmd := Wrap("101", "git -C '/root/adk demo' diff", "/root")
	if !strings.HasPrefix(cmd, "sudo pct exec 101 -- bash -c ") {
		t.Fatalf("prefix: %s", cmd)
	}
	if !strings.Contains(cmd, "cd /root") {
		t.Errorf("cwd missing: %s", cmd)
	}
	// the single-quoted payload survives the shell quoting round trip
	if !strings.Contains(cmd, "adk demo") {
		t.Errorf("payload lost: %s", cmd)
	}
}

// recordingRunner captures the command a ReadFile turns into.
type recordingRunner struct{ last string }

func (r *recordingRunner) Run(_ context.Context, cmd string, _ RunOpts) (Result, error) {
	r.last = cmd
	return Result{0, "data", ""}, nil
}

// TestReadFileUsesFastTail guards a real performance bug: `dd bs=1` costs one
// syscall per byte, so re-reading a growing agent log on every poll crawled.
func TestReadFileUsesFastTail(t *testing.T) {
	ssh := NewSSH("h", "root", 22, "")
	rec := &recordingRunner{}
	ssh.runner = rec.Run
	if _, err := ssh.ReadFile(context.Background(), "/log", 100); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.last, "tail -c +101") || strings.Contains(rec.last, "dd ") {
		t.Fatalf("ssh read command: %s", rec.last)
	}

	pct := NewPct("9")
	rec2 := &recordingRunner{}
	pct.runner = rec2.Run
	if _, err := pct.ReadFile(context.Background(), "/log", 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec2.last, "tail -c +1") || strings.Contains(rec2.last, "dd ") {
		t.Fatalf("pct read command: %s", rec2.last)
	}
}

func TestWriteFileCommandIsIdenticalEverywhere(t *testing.T) {
	// one recipe for every remote kind, so a staged file lands byte-identically
	cmd := writeFileCommand("/a/b/c.json", []byte(`{"x":1}`))
	if !strings.Contains(cmd, "mkdir -p /a/b") || !strings.Contains(cmd, "base64 -d > /a/b/c.json") {
		t.Fatalf("write command: %s", cmd)
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	if got := ShellQuote(`it's`); got != `'it'\''s'` {
		t.Fatalf("got %s", got)
	}
}
