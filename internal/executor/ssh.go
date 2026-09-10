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
// with bounded setup and command execution.
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

func (s *SSH) client(ctx context.Context) (*ssh.Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	cached := s.conn
	s.mu.Unlock()
	// Opening the command channel checks liveness under the caller's deadline.
	// A synchronous keepalive here could hang forever on a half-open connection.
	if cached != nil {
		return cached, nil
	}
	auth, err := s.authMethods()
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User: s.User, Auth: auth,
		// Preserve the target trust policy used by this executor.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	addr := net.JoinHostPort(s.Host, fmt.Sprint(s.Port))
	setup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := (&net.Dialer{}).DialContext(setup, "tcp", addr)
	if err != nil {
		return nil, Errf("ssh connect %s@%s:%d: %v", s.User, s.Host, s.Port, err)
	}
	stop := context.AfterFunc(setup, func() { _ = raw.Close() })
	transport, channels, requests, err := ssh.NewClientConn(raw, addr, cfg)
	stopped := stop()
	if err != nil || !stopped || setup.Err() != nil {
		_ = raw.Close()
		if err == nil {
			err = setup.Err()
		}
		return nil, Errf("ssh handshake %s@%s:%d: %v", s.User, s.Host, s.Port, err)
	}
	conn := ssh.NewClient(transport, channels, requests)
	s.mu.Lock()
	if s.conn == nil {
		s.conn = conn
		s.mu.Unlock()
		return conn, nil
	}
	cached = s.conn
	s.mu.Unlock()
	_ = conn.Close()
	return cached, nil
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

func (s *SSH) run(parent context.Context, cmd string, opts RunOpts, input io.Reader) (Result, error) {
	ctx, cancel := context.WithTimeout(parent, time.Duration(opts.timeoutOrDefault()*float64(time.Second)))
	defer cancel()
	full := s.buildCommand(cmd, opts.Cwd)
	if s.runner != nil {
		return s.runner(ctx, full, opts)
	}
	conn, err := s.client(ctx)
	if err != nil {
		return Result{}, err
	}
	// Channel setup and command I/O share the same total deadline. Closing a
	// timed-out transport also releases blocked channel requests and buffer writers.
	// Other in-flight commands on that pooled connection may need retrying.
	stop := context.AfterFunc(ctx, func() { s.dropClient(conn) })
	defer stop()
	sess, err := conn.NewSession()
	if err != nil {
		s.dropClient(conn)
		return Result{}, Errf("ssh session failed: %v", err)
	}
	defer sess.Close()
	var out, errb bytes.Buffer
	sess.Stdout, sess.Stderr = &out, &errb
	sess.Stdin = input
	done := make(chan error, 1)
	go func() { done <- sess.Run(full) }()
	err = <-done
	// sess.Run has joined the output-copy goroutines before buffers are read.
	if ctx.Err() != nil {
		if parent.Err() != nil {
			return Result{}, parent.Err()
		}
		return Result{124, out.String(), "command timed out"}, nil
	}
	rc := 0
	if err != nil {
		if exit, ok := err.(*ssh.ExitError); ok {
			rc = exit.ExitStatus()
		} else {
			s.dropClient(conn)
			return Result{}, Errf("ssh run failed: %v", err)
		}
	}
	return Result{rc, out.String(), errb.String()}, nil
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
	conn := s.conn
	s.conn = nil
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (s *SSH) dropClient(conn *ssh.Client) {
	s.mu.Lock()
	if s.conn == conn {
		s.conn = nil
	}
	s.mu.Unlock()
	_ = conn.Close()
}
