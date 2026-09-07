package executor

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSH is a remote target reached over SSH. Key auth only, one reused connection
// with keepalive.
type SSH struct {
	Host    string
	User    string
	Port    int
	KeyPath string

	mu   sync.Mutex
	conn *ssh.Client

	// runner is the command path, injectable so tests can assert on the exact
	// shell command an operation builds without needing a live target.
	runner func(context.Context, string, RunOpts) (Result, error)
}

// NewSSH builds an SSH executor for a target.
func NewSSH(host, user string, port int, keyPath string) *SSH {
	if user == "" {
		user = "root"
	}
	if port == 0 {
		port = 22
	}
	return &SSH{Host: host, User: user, Port: port, KeyPath: keyPath}
}

func (s *SSH) client() (*ssh.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		// cheap liveness probe: a dead connection fails here rather than halfway
		// through a dispatch
		if _, _, err := s.conn.SendRequest("keepalive@openssh.com", true, nil); err == nil {
			return s.conn, nil
		}
		s.conn.Close()
		s.conn = nil
	}
	auth, err := s.authMethods()
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User: s.User,
		Auth: auth,
		// agentdeck addresses its own homelab targets by Tailscale IP; pinning
		// host keys here would break every legitimate re-provision of a container.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := net.JoinHostPort(s.Host, fmt.Sprint(s.Port))
	conn, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, Errf("ssh connect %s@%s:%d: %v", s.User, s.Host, s.Port, err)
	}
	s.conn = conn
	return conn, nil
}

func (s *SSH) authMethods() ([]ssh.AuthMethod, error) {
	paths := []string{}
	if s.KeyPath != "" {
		paths = append(paths, s.KeyPath)
	} else if home, err := os.UserHomeDir(); err == nil {
		for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
			paths = append(paths, home+"/.ssh/"+name)
		}
	}
	var methods []ssh.AuthMethod
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		signer, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			continue
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	if len(methods) == 0 {
		return nil, Errf("no usable ssh key for %s@%s", s.User, s.Host)
	}
	return methods, nil
}

// Run executes a command over a fresh SSH channel on the pooled connection.
func (s *SSH) Run(ctx context.Context, cmd string, opts RunOpts) (Result, error) {
	if s.runner != nil {
		return s.runner(ctx, cmd, opts)
	}
	conn, err := s.client()
	if err != nil {
		return Result{}, err
	}
	full := cmd
	if opts.Cwd != "" {
		full = "cd " + ShellQuote(opts.Cwd) + " && " + cmd
	}
	sess, err := conn.NewSession()
	if err != nil {
		s.drop()
		return Result{}, Errf("ssh session failed: %v", err)
	}
	defer sess.Close()
	var out, errb bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &errb

	done := make(chan error, 1)
	go func() { done <- sess.Run(full) }()
	timeout := time.After(time.Duration(opts.timeoutOrDefault() * float64(time.Second)))
	select {
	case err := <-done:
		rc := 0
		if err != nil {
			var ee *ssh.ExitError
			if e, ok := err.(*ssh.ExitError); ok {
				ee = e
				rc = ee.ExitStatus()
			} else {
				s.drop()
				return Result{}, Errf("ssh run failed: %v", err)
			}
		}
		return Result{rc, out.String(), errb.String()}, nil
	case <-timeout:
		_ = sess.Signal(ssh.SIGKILL)
		return Result{124, out.String(), "timeout: " + cmd}, nil
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		return Result{}, ctx.Err()
	}
}

// ReadFile tails from a byte offset.
//
// `tail -c +N` is 1-indexed and streams in big blocks. The obvious `dd bs=1`
// spelling costs one syscall per byte, which is pathological when a growing
// agent log is re-read on every poll.
func (s *SSH) ReadFile(ctx context.Context, path string, offset int64) ([]byte, error) {
	r, err := s.Run(ctx, fmt.Sprintf("tail -c +%d %s 2>/dev/null || true",
		offset+1, ShellQuote(path)), RunOpts{Timeout: 60})
	if err != nil {
		return nil, err
	}
	return []byte(r.Stdout), nil
}

// WriteFile uploads via a base64 heredoc — no SFTP subsystem required, and it
// behaves identically on the pct executor.
func (s *SSH) WriteFile(ctx context.Context, path string, data []byte) error {
	_, err := s.Run(ctx, writeFileCommand(path, data), RunOpts{Timeout: 120})
	return err
}

// Close drops the pooled connection.
func (s *SSH) Close() error {
	s.drop()
	return nil
}

func (s *SSH) drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
}
