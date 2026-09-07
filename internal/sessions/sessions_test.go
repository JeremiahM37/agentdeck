package sessions

import (
	"strings"
	"testing"
	"time"

	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/store"
)

func launcher() Launcher {
	return Launcher{ClaudeBin: "claude", CodexBin: "codex", GeminiBin: "gemini"}
}

func TestLaunchCommandIsInteractiveNotHeadless(t *testing.T) {
	cmd := launcher().LaunchCommand("claude", "/srv/repo", "adk-s7", "opus", false)
	if !strings.HasPrefix(cmd, "tmux new-session -d -s adk-s7 ") {
		t.Fatalf("prefix: %s", cmd)
	}
	// the whole point of a session is that a human is at the keyboard: no -p,
	// no stream-json, no exit-code file
	for _, unwanted := range []string{" -p ", "stream-json", "exit_code", "prompt.md"} {
		if strings.Contains(cmd, unwanted) {
			t.Errorf("interactive launch must not carry %q: %s", unwanted, cmd)
		}
	}
	if !strings.Contains(cmd, "cd /srv/repo") || !strings.Contains(cmd, "--model opus") {
		t.Errorf("launch: %s", cmd)
	}
	// the pane survives the agent exiting, so the scrollback is still there
	if !strings.Contains(cmd, "exec bash") {
		t.Errorf("pane should outlive the agent: %s", cmd)
	}
}

func TestLaunchCommandResumesPerAgent(t *testing.T) {
	if got := launcher().LaunchCommand("claude", "/r", "s", "", true); !strings.Contains(got, "--continue") {
		t.Errorf("claude resume: %s", got)
	}
	if got := launcher().LaunchCommand("codex", "/r", "s", "", true); !strings.Contains(got, "resume --last") {
		t.Errorf("codex resume: %s", got)
	}
	if got := launcher().LaunchCommand("claude", "/r", "s", "", false); strings.Contains(got, "--continue") {
		t.Errorf("a fresh session must not resume: %s", got)
	}
}

func TestLaunchCommandQuotesHostileWorkdirs(t *testing.T) {
	cmd := launcher().LaunchCommand("claude", "/srv/'; rm -rf /; '", "s", "", false)
	if strings.Count(cmd, "rm -rf /") != 1 || !strings.Contains(cmd, `'\''`) {
		t.Fatalf("hostile path was not quoted as one word: %s", cmd)
	}
}

func TestPollBatchesEverySessionIntoOneCommand(t *testing.T) {
	// one exec per target per tick, not one per session — over SSH the round
	// trip is what costs, not the capture
	cmd := PollCommand([]string{"adk-s1", "adk-s2", "adk-s3"})
	if n := strings.Count(cmd, "capture-pane"); n != 3 {
		t.Fatalf("expected 3 captures in one command, got %d", n)
	}
	for _, name := range []string{"adk-s1", "adk-s2", "adk-s3"} {
		if !strings.Contains(cmd, name) {
			t.Errorf("missing %s", name)
		}
	}
}

func TestParsePollSplitsPanesBackApart(t *testing.T) {
	raw := PollDelimiter + "adk-s1\nhello\nworld" + PollDelimiter + "adk-s2\nother"
	panes := ParsePoll(raw)
	if panes["adk-s1"] != "hello\nworld" {
		t.Errorf("first pane: %q", panes["adk-s1"])
	}
	if panes["adk-s2"] != "other" {
		t.Errorf("second pane: %q", panes["adk-s2"])
	}
}

func TestDeriveStatus(t *testing.T) {
	// an explicit interrupt hint is the agent telling us it is mid-turn, and it
	// beats every other signal
	if got := DeriveStatus("thinking...\n✻ Working… (esc to interrupt)", Hash("x")); got != StatusRunning {
		t.Errorf("busy marker: %s", got)
	}
	// a pane that moved is working, whatever it printed
	if got := DeriveStatus("some new output", "stale-hash"); got != StatusRunning {
		t.Errorf("changed pane: %s", got)
	}
	// unchanged and sitting at a prompt: it wants you
	prompt := "done.\n\n❯ "
	if got := DeriveStatus(prompt, Hash(prompt)); got != StatusWaiting {
		t.Errorf("prompt: %s", got)
	}
	// unchanged and unrecognisable: say idle rather than guess
	quiet := "some output with no prompt shape at all"
	if got := DeriveStatus(quiet, Hash(quiet)); got != StatusIdle {
		t.Errorf("quiet: %s", got)
	}
	if got := DeriveStatus("   \n ", ""); got != StatusStarting {
		t.Errorf("empty pane: %s", got)
	}
}

func TestContextPctIsReadNotInvented(t *testing.T) {
	pane := "footer · Context left until auto-compact: 17%"
	pct := ContextPct(pane)
	if pct == nil || *pct != 17 {
		t.Fatalf("got %v", pct)
	}
	// agents only surface the gauge when it starts to matter; absent must stay
	// absent rather than becoming a made-up number
	if got := ContextPct("no gauge here"); got != nil {
		t.Fatalf("expected nil, got %v", *got)
	}
	if got := ContextPct("Context left until auto-compact: 900%"); got != nil {
		t.Fatal("an impossible reading must be rejected")
	}
}

func TestPreviewKeepsTheTail(t *testing.T) {
	pane := "one\n\ntwo\n   \nthree\nfour"
	got := Preview(pane, 2)
	if got != "three\nfour" {
		t.Fatalf("preview: %q", got)
	}
}

func TestSendTextGoesThroughABufferNotSendKeys(t *testing.T) {
	// send-keys -l would re-interpret newlines as submissions and quotes as
	// shell syntax; a buffer paste delivers the text exactly as written
	cmd := SendTextCommand("adk-s2", "/tmp/stage")
	for _, want := range []string{"load-buffer", "paste-buffer", "send-keys -t adk-s2 Enter", "rm -f"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %q: %s", want, cmd)
		}
	}
}

func TestSendKeyIsAnAllowlist(t *testing.T) {
	if _, ok := SendKeyCommand("s", "escape"); !ok {
		t.Error("escape is how you interrupt a turn")
	}
	if _, ok := SendKeyCommand("s", "rm -rf /"); ok {
		t.Fatal("this is a raw input channel — only named keys may pass")
	}
}

// Discovery is the half a task board misses: the sessions you care about most
// were started by hand, in a terminal, weeks ago.
func TestParseDiscoverJoinsPanesToProcessesByTTY(t *testing.T) {
	out := "claude-2\t/dev/pts/13\t/home/me/proj\n" +
		"plain-shell\t/dev/pts/4\t/home/me\n" +
		DiscoverDelimiter +
		"pts/13   claude --continue --model opus\n" +
		"pts/4    bash\n" +
		"pts/99   vim notes.md\n"
	got := ParseDiscover(out)
	if len(got) != 1 {
		t.Fatalf("expected one agent, got %+v", got)
	}
	c := got[0]
	if c.TmuxSession != "claude-2" || c.Agent != "claude" || c.Model != "opus" {
		t.Errorf("candidate: %+v", c)
	}
	if c.Workdir != "/home/me/proj" {
		t.Errorf("workdir: %q", c.Workdir)
	}
}

// pane_current_command reports the login shell for an agent started from one,
// which is exactly why discovery joins on the tty instead.
func TestParseDiscoverFindsAgentsUnderALoginShell(t *testing.T) {
	out := "wrapped\t/dev/pts/1\t/srv/x\n" + DiscoverDelimiter +
		"pts/1    bash -lc /home/me/.local/bin/claude --dangerously-skip-permissions\n" +
		"pts/1    /home/me/.local/bin/claude --dangerously-skip-permissions\n"
	got := ParseDiscover(out)
	if len(got) != 1 || got[0].Agent != "claude" {
		t.Fatalf("candidates: %+v", got)
	}
}

func TestMatchProjectPrefersTheLongestRepo(t *testing.T) {
	repos := map[int64]string{1: "/home/me", 2: "/home/me/projects/app"}
	// a repo nested inside another must win over its parent
	if id, ok := MatchProject("/home/me/projects/app/internal", repos); !ok || id != 2 {
		t.Fatalf("got %d %v", id, ok)
	}
	if id, ok := MatchProject("/home/me/other", repos); !ok || id != 1 {
		t.Fatalf("got %d %v", id, ok)
	}
	if _, ok := MatchProject("/srv/elsewhere", repos); ok {
		t.Fatal("an unrelated path must not be claimed by a project")
	}
	// a prefix that is not a path boundary is not a match
	if _, ok := MatchProject("/home/mexico", map[int64]string{1: "/home/me"}); ok {
		t.Fatal("/home/me must not claim /home/mexico")
	}
}

func TestHandoffPromptAsksForAFileNotAChatReply(t *testing.T) {
	p := HandoffPrompt("/tmp/agentdeck-handoff-4.md")
	if !strings.Contains(p, "/tmp/agentdeck-handoff-4.md") {
		t.Error("the path must be explicit")
	}
	if !strings.Contains(p, "do not print it here") {
		t.Error("a wrap printed into the transcript is not durable")
	}
	for _, section := range []string{"WHERE WE ARE", "NEXT", "DECISIONS", "GOTCHAS", "STATE"} {
		if !strings.Contains(p, section) {
			t.Errorf("prompt is missing the %s section", section)
		}
	}
}

func TestResumePromptCarriesTheWrapAndHoldsOff(t *testing.T) {
	p := ResumePrompt("sglang", "we were mid-refactor", "known: the fan is loud")
	if !strings.Contains(p, "sglang") || !strings.Contains(p, "we were mid-refactor") {
		t.Errorf("prompt: %s", p)
	}
	if !strings.Contains(p, "known: the fan is loud") {
		t.Error("project knowledge should reach the successor too")
	}
	// a fresh context confirming state before changing things is the whole
	// safety property of a handoff
	if !strings.Contains(p, "Do not start changing things") {
		t.Error("the successor must confirm state before acting")
	}
}

func TestIdleForFallsBackToCreation(t *testing.T) {
	s := &store.Session{CreatedAt: store.Now() - 30}
	if d := IdleFor(s); d < 25*time.Second || d > 40*time.Second {
		t.Fatalf("a session that never moved has been idle since it started: %s", d)
	}
	recent := store.Now() - 5
	s.LastActivityAt = &recent
	if d := IdleFor(s); d > 10*time.Second {
		t.Fatalf("idle should track last activity: %s", d)
	}
}

// The mock reproduces the wire format by hand because the sessions package
// depends on the executor package, not the other way round. If these ever drift,
// every session test would pass against a protocol the real target never speaks.
func TestMockDelimitersMatchSessions(t *testing.T) {
	m := executor.NewMock(0)
	_ = m
	if executor.MockPaneDelimiter != PollDelimiter {
		t.Errorf("pane delimiter drifted: %q vs %q", executor.MockPaneDelimiter, PollDelimiter)
	}
	if executor.MockDiscoverDelimiter != DiscoverDelimiter {
		t.Errorf("discover delimiter drifted: %q vs %q",
			executor.MockDiscoverDelimiter, DiscoverDelimiter)
	}
}

// A delimiter travels inside a shell command line. exec() truncates argv at the
// first NUL, so a NUL delimiter silently cuts the command in half on a real
// target — while every mock test still passes.
func TestDelimitersSurviveAShellCommandLine(t *testing.T) {
	for name, d := range map[string]string{
		"poll": PollDelimiter, "discover": DiscoverDelimiter,
	} {
		if strings.ContainsRune(d, 0) {
			t.Errorf("%s delimiter contains a NUL: exec would truncate the command", name)
		}
		if strings.ContainsAny(d, "\n'\"\\") {
			t.Errorf("%s delimiter contains shell-significant characters: %q", name, d)
		}
	}
	// and the built commands must be NUL-free end to end
	if strings.ContainsRune(PollCommand([]string{"a", "b"}), 0) {
		t.Error("the poll command carries a NUL")
	}
	if strings.ContainsRune(DiscoverCommand(), 0) {
		t.Error("the discover command carries a NUL")
	}
}

func TestParseTimesReadsTmuxsOwnClock(t *testing.T) {
	created, activity, ok := ParseTimes(" 1788336741 1788733986 \n")
	if !ok || created != 1788336741 || activity != 1788733986 {
		t.Fatalf("got %v %v %v", created, activity, ok)
	}
	for _, bad := range []string{"", "not numbers", "1788336741"} {
		if _, _, ok := ParseTimes(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
}
