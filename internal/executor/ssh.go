package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
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
	// Wrapper, when set, transforms every command before it is sent. It is how a
	// host whose SSH lands somewhere other than the work becomes an ordinary
	// target. Two placeholders:
	//
	//	{cmd}  the command, POSIX shell-quoted
	//	{b64}  the command, base64-encoded
	//
	// {b64} is the one to reach for on a Windows host: its SSH server hands the
	// line to cmd.exe, which does not understand POSIX quoting, and anything
	// that survives that still gets $-expanded by the outer bash. Base64 is
	// alphanumeric, so it passes through both untouched:
	//
	//	wsl -e bash -lc "echo {b64} | base64 -d | bash"
	//
	// A wrapper with no placeholder is treated as a prefix and given the
	// shell-quoted command, which is what a POSIX host wants.
	Wrapper string

	mu   sync.Mutex
	conn *ssh.Client

	// runner is the command path, injectable so tests can assert on the exact
	// shell command an operation builds without needing a live target.
	runner func(context.Context, string, RunOpts) (Result, error)
}

// NewSSH builds an SSH executor for a target.
func NewSSH(host, user string, port int, keyPath, wrapper string) *SSH {
	if user == "" {
		user = "root"
	}
	if port == 0 {
		port = 22
	}
	return &SSH{Host: host, User: user, Port: port, KeyPath: keyPath, Wrapper: wrapper}
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

// buildCommand renders what actually goes over the wire: the command, its
// working directory, and any target wrapper — in that order, so the wrapper
// receives the whole thing as one argument.
func (s *SSH) buildCommand(cmd, cwd string) string {
	full := cmd
	if cwd != "" {
		full = "cd " + ShellQuote(cwd) + " && " + cmd
	}
	if s.Wrapper == "" {
		return full
	}
	switch {
	case strings.Contains(s.Wrapper, "{b64}"):
		return strings.ReplaceAll(s.Wrapper, "{b64}",
			base64.StdEncoding.EncodeToString([]byte(full)))
	case strings.Contains(s.Wrapper, "{cmd}"):
		return strings.ReplaceAll(s.Wrapper, "{cmd}", ShellQuote(full))
	default:
		return s.Wrapper + " " + ShellQuote(full)
	}
}

// Run executes a command over a fresh SSH channel on the pooled connection.
func (s *SSH) Run(ctx context.Context, cmd string, opts RunOpts) (Result, error) {
	return s.run(ctx, cmd, opts, nil)
}

func (s *SSH) run(ctx context.Context, cmd string, opts RunOpts, input io.Reader) (Result, error) {
	full := s.buildCommand(cmd, opts.Cwd)
	// the seam receives the FULLY BUILT command, so a test asserting on it is
	// checking what the target would really see rather than a reimplementation
	if s.runner != nil {
		return s.runner(ctx, full, opts)
	}
	conn, err := s.client()
	if err != nil {
		return Result{}, err
	}
	sess, err := conn.NewSession()
	if err != nil {
		s.drop()
		return Result{}, Errf("ssh session failed: %v", err)
	}
	defer sess.Close()
	var out, errb bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &errb
	sess.Stdin = input

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
		return Result{124, out.String(), "command timed out"}, nil
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

// WriteFile streams file bytes on native SSH targets. Wrapped targets may consume
// stdin themselves (e.g. Windows -> WSL), so use bounded commands there.
func (s *SSH) WriteFile(ctx context.Context, path string, data []byte) error {
	if s.Wrapper != "" || s.runner != nil {
		return writeFileChunks(ctx, s.Run, path, data, 2048)
	}
	r, err := s.run(ctx, streamFileCommand(path), RunOpts{Timeout: 120}, bytes.NewReader(data))
	return fileWriteResult(r, err)
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
