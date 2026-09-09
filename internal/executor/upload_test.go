package executor

import (
	"bytes"
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

	"golang.org/x/crypto/ssh"
)

// Real SSH transport and a real shell, isolated on loopback. In particular this
// catches command/argv size limits that a scripted runner cannot reproduce.
func TestSSHUploadLargeBinary(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.MarshalPrivateKey(private, "test")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(key), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(ssh.ConnMetadata, ssh.PublicKey) (*ssh.Permissions, error) { return nil, nil }}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		server, channels, requests, err := ssh.NewServerConn(conn, cfg)
		if err != nil {
			return
		}
		defer server.Close()
		go func() {
			for req := range requests {
				req.Reply(true, nil)
			}
		}()
		for nc := range channels {
			ch, reqs, err := nc.Accept()
			if err != nil {
				continue
			}
			go func() {
				defer ch.Close()
				for req := range reqs {
					if req.Type != "exec" {
						req.Reply(false, nil)
						continue
					}
					var payload struct{ Command string }
					ssh.Unmarshal(req.Payload, &payload)
					req.Reply(true, nil)
					cmd := exec.Command("bash", "-c", payload.Command)
					cmd.Stdin = ch
					cmd.Stdout = ch
					cmd.Stderr = ch.Stderr()
					rc := uint32(0)
					if err := cmd.Run(); err != nil {
						rc = 1
					}
					ch.SendRequest("exit-status", false, ssh.Marshal(struct{ RC uint32 }{rc}))
					return
				}
			}()
		}
	}()
	ex := NewSSH("127.0.0.1", "test", listener.Addr().(*net.TCPAddr).Port, keyPath, "")
	defer func() { ex.Close(); <-done }()
	data := make([]byte, 3<<20)
	rand.Read(data)
	file := filepath.Join(t.TempDir(), "PDF's $(touch SHOULD_NOT_EXIST) 文档.pdf")
	if err := ex.WriteFile(context.Background(), file, data); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("bytes lost: %v", err)
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode: %v", info.Mode())
	}
	if err := ex.WriteFile(context.Background(), file+"/invalid", data); err == nil {
		t.Fatal("remote failure reported success")
	}
}

func TestWrappedUploadChunksSurviveRealShell(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a ' quote 文档.pdf")
	data := bytes.Repeat([]byte{0, 255, 1, 4, 10}, 80000)
	ex := NewSSH("unused", "test", 22, "", `bash -c 'echo {b64} | base64 -d | bash'`)
	local := NewLocal()
	ex.runner = func(ctx context.Context, cmd string, opts RunOpts) (Result, error) {
		if len(cmd) > 8191 {
			t.Fatalf("wrapped command exceeds Windows limit: %d", len(cmd))
		}
		return local.Run(ctx, cmd, opts)
	}
	if err := ex.WriteFile(context.Background(), file, data); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if !bytes.Equal(got, data) {
		t.Fatal("wrapped binary mismatch")
	}
	ex.runner = func(context.Context, string, RunOpts) (Result, error) { return Result{RC: 1, Stderr: "disk full"}, nil }
	if err := ex.WriteFile(context.Background(), file, data); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("failure lost: %v", err)
	}
	pct := NewPct("123")
	pct.runner = ex.runner
	if err := pct.WriteFile(context.Background(), file, data); err == nil {
		t.Fatal("pct failure lost")
	}
}
