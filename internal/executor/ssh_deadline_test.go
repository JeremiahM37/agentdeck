package executor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Real SSH transport: no injected runner can hide a blocking handshake/channel.
func deadlineSSH(t *testing.T, mode string) *SSH {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var connections []net.Conn
	var workers sync.WaitGroup
	stop := make(chan struct{})
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, raw)
			mu.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer raw.Close()
				if mode == "handshake" {
					<-stop
					return
				}
				conn, channels, requests, err := ssh.NewServerConn(raw, config)
				if err != nil {
					return
				}
				defer conn.Close()
				if mode == "cached" {
					go func() {
						for range requests {
						}
					}()
				} else {
					go ssh.DiscardRequests(requests)
				}
				opened := 0
				for incoming := range channels {
					opened++
					if mode == "cached" && opened > 1 {
						continue
					}
					if mode == "channel" {
						continue
					} // never acknowledge channel open
					channel, reqs, err := incoming.Accept()
					if err != nil {
						return
					}
					for req := range reqs {
						if req.Type == "exec" {
							_ = req.Reply(true, nil)
							_, _ = channel.Write([]byte("partial output\n"))
							if mode == "success" || mode == "cached" {
								_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
								_ = channel.Close()
							}
						} else {
							_ = req.Reply(false, nil)
						}
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		close(stop)
		mu.Lock()
		for _, conn := range connections {
			_ = conn.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	ex := NewSSH("127.0.0.1", "test", listener.Addr().(*net.TCPAddr).Port, path, "")
	t.Cleanup(func() { _ = ex.Close() })
	return ex
}

func TestSSHDeadlineIncludesHandshakeChannelAndOutput(t *testing.T) {
	for _, mode := range []string{"handshake", "channel", "command"} {
		t.Run(mode, func(t *testing.T) {
			ex := deadlineSSH(t, mode)
			start := time.Now()
			result, err := ex.Run(context.Background(), "ignored", RunOpts{Timeout: .15})
			if time.Since(start) > 2*time.Second {
				t.Fatal("deadline did not bound SSH setup/command")
			}
			if err == nil && result.RC != 124 {
				t.Fatalf("unexpected success: %+v", result)
			}
		})
	}
}

func TestSSHCancelledParentBoundsSetupAndDoesNotPoisonNextCall(t *testing.T) {
	ex := deadlineSSH(t, "success")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ex.Run(ctx, "ignored", RunOpts{}); err == nil {
		t.Fatal("canceled request ran")
	}
	for range 2 {
		result, err := ex.Run(context.Background(), "ignored", RunOpts{Timeout: 2})
		if err != nil || !result.OK() || result.Stdout != "partial output\n" {
			t.Fatalf("pooled command failed: %+v %v", result, err)
		}
	}
}

func TestSSHCachedStallIsBoundedAndNextRequestReconnects(t *testing.T) {
	ex := deadlineSSH(t, "cached")
	for cycle := 0; cycle < 2; cycle++ {
		if result, err := ex.Run(context.Background(), "first", RunOpts{Timeout: 2}); err != nil || !result.OK() {
			t.Fatalf("fresh connection failed: %+v %v", result, err)
		}
		start := time.Now()
		result, err := ex.Run(context.Background(), "stalled", RunOpts{Timeout: .15})
		if time.Since(start) > 2*time.Second || (err == nil && result.OK()) {
			t.Fatalf("cached connection ignored deadline: %+v %v", result, err)
		}
	}
}

func TestSSHConcurrentCommandsRemainIndependent(t *testing.T) {
	ex := deadlineSSH(t, "success")
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := ex.Run(context.Background(), "parallel", RunOpts{Timeout: 2})
			if err != nil || !result.OK() || result.Stdout != "partial output\n" {
				t.Errorf("parallel command failed: %+v %v", result, err)
			}
		}()
	}
	workers.Wait()
}
