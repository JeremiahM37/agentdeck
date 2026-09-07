package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/JeremiahM37/agentdeck/internal/store"
)

// fakeTTYD stands in for the real binary: it records the port it was handed and
// stays alive, the way a bound ttyd does.
func fakeManager(t *testing.T) (*Manager, func() []int) {
	t.Helper()
	m := NewManager()
	m.LookPath = func(string) (string, error) { return "/usr/bin/ttyd", nil }
	var mu sync.Mutex
	var ports []int
	m.Spawn = func(port int, argv []string) (*exec.Cmd, error) {
		mu.Lock()
		ports = append(ports, port)
		mu.Unlock()
		cmd := exec.Command("sleep", "30")
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return cmd, nil
	}
	t.Cleanup(m.Shutdown)
	return m, func() []int {
		mu.Lock()
		defer mu.Unlock()
		out := append([]int(nil), ports...)
		return out
	}
}

func target(kind string) *store.Target {
	return &store.Target{Kind: kind, Host: "192.168.0.5", User: "admin", ID: 1}
}

// Two people clicking attach at the same moment is ordinary — the board shows a
// running attempt and a live session side by side. If both land on one port the
// second ttyd cannot bind and its terminal is dead on arrival.
func TestConcurrentAttachesGetDistinctPorts(t *testing.T) {
	m, spawned := fakeManager(t)
	const n = 6
	var wg sync.WaitGroup
	got := make([]int, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], errs[i] = m.Attach(context.Background(),
				Attachment{Key: fmt.Sprintf("attempt:%d", i), TmuxSession: "adk-1"}, target("local"))
		}(i)
	}
	wg.Wait()

	seen := map[int]int{}
	for i, port := range got {
		if errs[i] != nil {
			t.Fatalf("attach %d failed: %v", i, errs[i])
		}
		if prev, dup := seen[port]; dup {
			t.Fatalf("attachments %d and %d were both given port %d — the second ttyd "+
				"cannot bind, so that terminal is dead on arrival", prev, i, port)
		}
		seen[port] = i
	}
	if len(spawned()) != n {
		t.Errorf("spawned %d ttyds for %d attachments", len(spawned()), n)
	}
}

// Attaching twice to the same thing must reuse the existing terminal rather than
// burning another port from a range of twenty.
func TestAttachingTwiceReusesTheSameTerminal(t *testing.T) {
	m, spawned := fakeManager(t)
	a := Attachment{Key: "session:3", TmuxSession: "adk-sess-3"}
	first, err := m.Attach(context.Background(), a, target("local"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Attach(context.Background(), a, target("local"))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("the same attachment got two ports: %d then %d", first, second)
	}
	if len(spawned()) != 1 {
		t.Errorf("spawned %d ttyds for one attachment", len(spawned()))
	}
}

// An attempt and a session must never share a ttyd — the keys are namespaced
// precisely so id 3 in one kind cannot collide with id 3 in the other.
func TestAttemptAndSessionKeysDoNotCollide(t *testing.T) {
	m, _ := fakeManager(t)
	x, err := m.Attach(context.Background(), Attachment{Key: "attempt:3", TmuxSession: "adk-3"}, target("local"))
	if err != nil {
		t.Fatal(err)
	}
	y, err := m.Attach(context.Background(), Attachment{Key: "session:3", TmuxSession: "adk-sess-3"}, target("local"))
	if err != nil {
		t.Fatal(err)
	}
	if x == y {
		t.Errorf("attempt:3 and session:3 shared port %d", x)
	}
}

func TestPortsAreReturnedWhenTerminalsAreShutDown(t *testing.T) {
	m, _ := fakeManager(t)
	for i := 0; i < 4; i++ {
		if _, err := m.Attach(context.Background(),
			Attachment{Key: fmt.Sprintf("attempt:%d", i)}, target("local")); err != nil {
			t.Fatal(err)
		}
	}
	m.Shutdown()
	m.mu.Lock()
	n := len(m.procs)
	m.mu.Unlock()
	if n != 0 {
		t.Errorf("%d terminals still tracked after shutdown", n)
	}
	// the range is only twenty wide, so a leak here exhausts it in a day's use
	if _, err := m.Attach(context.Background(),
		Attachment{Key: "attempt:99"}, target("local")); err != nil {
		t.Fatalf("could not attach after a shutdown: %v", err)
	}
}

func TestExhaustingTheRangeIsAClearError(t *testing.T) {
	m, _ := fakeManager(t)
	// Allocate until the range runs out rather than assuming how many are free:
	// the range is shared with anything else on this machine that happens to be
	// listening (the estate's temp terminals overlap it), and a test that pins an
	// exact count fails for reasons that have nothing to do with the code.
	got := 0
	var err error
	for i := 0; i <= PortHi-PortLo; i++ {
		if _, err = m.Attach(context.Background(),
			Attachment{Key: fmt.Sprintf("attempt:%d", i)}, target("local")); err != nil {
			break
		}
		got++
	}
	if got == 0 {
		t.Fatalf("could not allocate a single terminal: %v", err)
	}
	if err == nil {
		// the whole range was free, so ask for one more than it holds
		_, err = m.Attach(context.Background(),
			Attachment{Key: "one-too-many"}, target("local"))
	}
	if err == nil {
		t.Fatal("the range is full; attaching must fail rather than hand out a used port")
	}
	if !strings.Contains(err.Error(), "no free terminal port") {
		t.Errorf("the error should say the range is exhausted, got %q", err)
	}
	t.Logf("allocated %d of %d before the range ran out", got, PortHi-PortLo+1)
}

// The argv is what actually reaches the target. A wrong one fails at connect
// time in a browser tab, which is the worst place to discover it.
func TestAttachArgvPerTargetKind(t *testing.T) {
	for name, tc := range map[string]struct {
		a      Attachment
		target *store.Target
		want   []string
	}{
		"local": {
			Attachment{TmuxSession: "adk-7"},
			&store.Target{Kind: "local"},
			[]string{"tmux", "attach", "-t", "adk-7"},
		},
		"pct container": {
			Attachment{TmuxSession: "adk-7"},
			&store.Target{Kind: "pct", Host: "104"},
			[]string{"sudo", "pct", "exec", "104", "--", "tmux", "attach", "-t", "adk-7"},
		},
		"ephemeral sandbox beats the target kind": {
			Attachment{TmuxSession: "adk-7", SandboxVMID: "9001"},
			&store.Target{Kind: "sandbox", Host: "irrelevant"},
			[]string{"sudo", "pct", "exec", "9001", "--", "tmux", "attach", "-t", "adk-7"},
		},
		"ssh with a key": {
			Attachment{TmuxSession: "adk-7"},
			&store.Target{Kind: "ssh", Host: "100.75.49.118", User: "claude", KeyPath: "/home/admin/.ssh/id_ed25519"},
			[]string{"ssh", "-tt", "-o", "StrictHostKeyChecking=accept-new",
				"-i", "/home/admin/.ssh/id_ed25519", "claude@100.75.49.118", "tmux", "attach", "-t", "adk-7"},
		},
		"ssh defaults to root": {
			Attachment{TmuxSession: "adk-7"},
			&store.Target{Kind: "ssh", Host: "h"},
			[]string{"ssh", "-tt", "-o", "StrictHostKeyChecking=accept-new", "root@h", "tmux", "attach", "-t", "adk-7"},
		},
	} {
		got, err := AttachArgv(tc.a, tc.target)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Errorf("%s:\n got %v\nwant %v", name, got, tc.want)
		}
	}
}

// A sandbox attach that lost its vmid must not silently fall through to running
// tmux on the control plane itself.
func TestSandboxWithoutAVMIDDoesNotAttachToTheHost(t *testing.T) {
	got, err := AttachArgv(Attachment{TmuxSession: "adk-7"}, &store.Target{Kind: "sandbox"})
	if err == nil {
		t.Fatalf("a sandbox attach with no vmid must fail, got %v", got)
	}
	if strings.Join(got, " ") == "tmux attach -t adk-7" {
		t.Error("it fell through to the control plane's own tmux")
	}
	// and the failure must reach the caller rather than spawning anything
	m, spawned := fakeManager(t)
	if _, err := m.Attach(context.Background(),
		Attachment{Key: "attempt:1"}, &store.Target{Kind: "sandbox"}); err == nil {
		t.Error("Attach accepted a sandbox with no container id")
	}
	if len(spawned()) != 0 {
		t.Errorf("it spawned %d ttyds anyway", len(spawned()))
	}
}

func TestAttachFailsClearlyWithoutTTYD(t *testing.T) {
	m, _ := fakeManager(t)
	m.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	_, err := m.Attach(context.Background(), Attachment{Key: "attempt:1"}, target("local"))
	if err == nil || !strings.Contains(err.Error(), "ttyd is not installed") {
		t.Errorf("expected a clear missing-ttyd error, got %v", err)
	}
}
