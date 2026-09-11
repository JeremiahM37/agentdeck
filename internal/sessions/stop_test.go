package sessions

import (
	"context"
	"github.com/JeremiahM37/agentdeck/internal/shellq"
	"github.com/JeremiahM37/agentdeck/internal/store"
	"github.com/JeremiahM37/agentdeck/internal/testutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestKillDoesNotCloseRecordWhenTmuxRefusesStop(t *testing.T) {
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	for _, code := range []string{"0", "1", "124"} {
		t.Run("exit-"+code, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "adk-kill-proof-")
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("TMUX_TMPDIR", dir)
			t.Setenv("TMUX", "")
			t.Cleanup(func() { testutil.CleanupTmux(t, dir); _ = os.RemoveAll(dir) })
			m, s := pollRig(t)
			if out, err := exec.Command(real, "-f", "/dev/null", "new-session", "-d", "-s", s.TmuxSession, "sleep 600").CombinedOutput(); err != nil {
				t.Fatalf("tmux: %s %v", out, err)
			}
			wrapper := "#!/bin/sh\nif [ \"$1\" = if-shell ]; then exit " + code + "; fi\nexec " + shellq.Quote(real) + " \"$@\"\n"
			if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(wrapper), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			err = m.Kill(context.Background(), s.ID)
			got, readErr := m.DB.Session(s.ID)
			if check := exec.Command(real, "has-session", "-t", "="+s.TmuxSession).Run(); check != nil {
				t.Fatal("fixture terminal unexpectedly stopped")
			}
			if err == nil || readErr != nil || got.EndedAt != nil {
				t.Fatalf("still-running terminal accepted as stopped: killError=%v readError=%v ended=%v", err, readErr, got.EndedAt)
			}
		})
	}
}

func TestKillVerifiesExactSessionAndPreservesEndedTime(t *testing.T) {
	m, row, real, _ := stopRig(t)
	neighbor := row.TmuxSession + "-other"
	if out, err := exec.Command(real, "new-session", "-d", "-s", neighbor, "sleep 600").CombinedOutput(); err != nil {
		t.Fatalf("neighbor: %s %v", out, err)
	}
	if err := m.Kill(context.Background(), row.ID); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(real, "has-session", "-t", "="+row.TmuxSession).Run(); err == nil {
		t.Fatal("original still running")
	}
	if err := exec.Command(real, "has-session", "-t", "="+neighbor).Run(); err != nil {
		t.Fatal("neighbor was stopped")
	}
	got, err := m.DB.Session(row.ID)
	if err != nil || got.EndedAt == nil || got.Status != StatusDead {
		t.Fatalf("stop not recorded: %+v %v", got, err)
	}
	ended := *got.EndedAt
	if err := m.Kill(context.Background(), row.ID); err != nil {
		t.Fatal(err)
	}
	got, err = m.DB.Session(row.ID)
	if err != nil || *got.EndedAt != ended {
		t.Fatal("repeated stop changed original timestamp")
	}
}

func TestKillRejectsChangedIdentityAndUnknownReleasedTerminal(t *testing.T) {
	for _, kind := range []string{"different", "changed-during-stop", "released-without-identity", "capture-refused"} {
		t.Run(kind, func(t *testing.T) {
			m, row, real, dir := stopRig(t)
			if kind == "different" {
				if err := m.DB.Update("sessions", row.ID, map[string]any{"tracking_identity": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}); err != nil {
					t.Fatal(err)
				}
				if err := exec.Command(real, "set-option", "-t", "="+row.TmuxSession+":", trackingOption, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb").Run(); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "released-without-identity" {
				if err := m.DB.Update("sessions", row.ID, map[string]any{"ended_at": store.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "changed-during-stop" || kind == "capture-refused" {
				action := "if [ \"$1\" = capture-pane ]; then exit 1; fi\n"
				if kind == "changed-during-stop" {
					action = "if [ \"$1\" = if-shell ]; then " + shellq.Quote(real) + " set-option -t " + shellq.Quote("="+row.TmuxSession+":") + " " + trackingOption + " bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb; fi\n"
				}
				wrapper := "#!/bin/sh\n" + action + "exec " + shellq.Quote(real) + " \"$@\"\n"
				if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(wrapper), 0755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			before, _ := m.DB.Session(row.ID)
			if err := m.Kill(context.Background(), row.ID); err == nil {
				t.Fatal("unsafe/unverified stop accepted")
			}
			got, err := m.DB.Session(row.ID)
			if err != nil || got.Status != before.Status || (got.EndedAt == nil) != (before.EndedAt == nil) {
				t.Fatalf("record changed: %+v %v", got, err)
			}
			if err := exec.Command(real, "has-session", "-t", "="+row.TmuxSession).Run(); err != nil {
				t.Fatal("current terminal was stopped")
			}
		})
	}
}

func stopRig(t *testing.T) (*Manager, *store.Session, string, string) {
	t.Helper()
	real, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("tmux unavailable")
	}
	dir, err := os.MkdirTemp("/tmp", "adk-stop-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	t.Cleanup(func() { testutil.CleanupTmux(t, dir); _ = os.RemoveAll(dir) })
	m, row := pollRig(t)
	if out, err := exec.Command(real, "-f", "/dev/null", "new-session", "-d", "-s", row.TmuxSession, "sleep 600").CombinedOutput(); err != nil {
		t.Fatalf("tmux: %s %v", out, err)
	}
	return m, row, real, dir
}
