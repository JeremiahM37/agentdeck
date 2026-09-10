package skills

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeremiahM37/agentdeck/internal/executor"
	"github.com/JeremiahM37/agentdeck/internal/store"
	"golang.org/x/crypto/ssh"
)

func TestLocalSkillLifecycleAndForeignCollision(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	source := filepath.Join(repo, ".agents", "skills", "lint")
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "sub", ".agents", "skills", "lint")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "SKILL.md"), []byte("name: nested\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: lint\ndescription: check\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if out := exec.Command("git", "init", "-q", repo).Run(); out != nil {
		t.Fatal(out)
	}
	if err := os.MkdirAll(work, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "init", "-q", work).Run(); err != nil {
		t.Fatal(err)
	}
	p := &store.Project{RepoPath: filepath.Join(repo, "sub")}
	ex := executor.NewLocal()
	ctx := context.Background()
	xs, err := Discover(ctx, ex, p, "codex")
	if err != nil {
		t.Fatal(err)
	}
	var x Skill
	seenLint := map[string]bool{}
	for _, candidate := range xs {
		if candidate.EntryName == "lint" && strings.HasPrefix(candidate.Source, "repo:") {
			seenLint[candidate.SourcePath] = true
		}
		if candidate.EntryName == "lint" && strings.HasPrefix(candidate.Source, "repo:") {
			x = candidate
		}
	}
	if x.EntryName != "lint" {
		t.Fatalf("discover=%+v", xs)
	}
	if len(seenLint) != 2 {
		t.Fatalf("nested and root repo roots collapsed: %v", seenLint)
	}
	p.RepoPath = repo
	dst, _, err := Materialize(ctx, ex, p, x, "codex", work, 7, false)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(dst)
	if err != nil || target != source {
		t.Fatalf("link=%q err=%v", target, err)
	}
	if _, _, err := Materialize(ctx, ex, p, x, "codex", work, 8, false); err == nil || !strings.Contains(err.Error(), "destination already exists") {
		t.Fatalf("unrecorded existing link was adopted: %v", err)
	}
	exclude, err := os.ReadFile(filepath.Join(work, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(exclude), "# agentdeck-owned-skill:7:") || !strings.Contains(string(exclude), "/.agents/skills/lint\n") {
		t.Fatalf("exclude=%q", exclude)
	}
	if err := os.WriteFile(filepath.Join(work, "foreign.txt"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	ax := &store.ProjectSkill{SourcePath: source, TargetRel: ".agents/skills/lint", ExcludeMarker: "# agentdeck-owned-skill:7"}
	if err := Remove(ctx, ex, p, ax, work); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(work, "foreign.txt")); err != nil {
		t.Fatal("foreign content removed", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatalf("skill link remains: %v", err)
	}
}

func TestConfiguredSourceDiscovery(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "configured", "skill")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("name: x\n"), 0644)
	p := &store.Project{RepoPath: filepath.Join(root, "missing"), SkillSourcesJSON: store.J([]string{filepath.Dir(src)})}
	xs, err := Discover(context.Background(), executor.NewLocal(), p, "codex")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range xs {
		if strings.HasPrefix(x.Source, "configured:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("configured skill missing: %+v", xs)
	}
}

func TestSSHSkillLifecycleUsesRealTransport(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.MarshalPrivateKey(private, "skill-test")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(key), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, _ ssh.PublicKey) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		raw, e := ln.Accept()
		if e != nil {
			return
		}
		conn, chans, reqs, e := ssh.NewServerConn(raw, cfg)
		if e != nil {
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(reqs)
		for nc := range chans {
			ch, reqs, e := nc.Accept()
			if e != nil {
				continue
			}
			go func() {
				defer ch.Close()
				for req := range reqs {
					if req.Type != "exec" {
						_ = req.Reply(false, nil)
						continue
					}
					var p struct{ Command string }
					ssh.Unmarshal(req.Payload, &p)
					_ = req.Reply(true, nil)
					c := exec.Command("bash", "-c", p.Command)
					c.Stdin = ch
					c.Stdout = ch
					c.Stderr = ch.Stderr()
					rc := uint32(0)
					if c.Run() != nil {
						rc = 1
					}
					_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ RC uint32 }{rc}))
					return
				}
			}()
		}
	}()
	ex := executor.NewSSH("127.0.0.1", "test", ln.Addr().(*net.TCPAddr).Port, keyPath, "")
	defer func() { _ = ex.Close(); <-done }()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	src := filepath.Join(root, "src", "remote")
	work := filepath.Join(root, "work")
	for _, d := range []string{filepath.Join(src), work} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "init", "-q", repo).Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("name: remote\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "init", "-q", work).Run(); err != nil {
		t.Fatal(err)
	}
	p := &store.Project{RepoPath: repo, SkillSourcesJSON: store.J([]string{filepath.Dir(src)})}
	xs, err := Discover(context.Background(), ex, p, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(xs) == 0 {
		t.Fatal("remote skill not discovered")
	}
	if _, _, err := Materialize(context.Background(), ex, p, xs[0], "codex", work, 11, false); err != nil {
		t.Fatal(err)
	}
}
