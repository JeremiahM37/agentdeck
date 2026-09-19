// Package forward carries TCP from a port on the control plane to a service
// that only listens on a target's localhost.
//
// An agent's dev server, a database console, a noVNC desktop: all of them bind
// to loopback on the machine the agent runs on, which is not the machine the
// operator is sitting at. A path-prefix HTTP proxy breaks real applications —
// absolute asset paths, redirects, websockets — so this forwards the port
// itself, and every protocol passes through unmodified.
//
// A forward makes a loopback-only service reachable by whoever can reach the
// control plane, so it is always explicit, always listed, and always temporary.
package forward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Dial opens a connection to addr as the target sees it.
type Dial func(ctx context.Context, addr string) (net.Conn, error)

// Forward is one open port.
type Forward struct {
	ID         int64   `json:"id"`
	Kind       string  `json:"kind"` // port | desktop
	Title      string  `json:"title"`
	TargetID   int64   `json:"target_id"`
	TargetName string  `json:"target_name"`
	SessionID  *int64  `json:"session_id"`
	Port       int     `json:"port"`        // on the target's loopback
	ListenPort int     `json:"listen_port"` // on the control plane
	CreatedAt  float64 `json:"created_at"`
	ExpiresAt  float64 `json:"expires_at"`
	Conns      int64   `json:"connections"` // a snapshot; the live count is conns
	// Detail carries what a kind needs shown beside it, e.g. a desktop's DISPLAY.
	Detail map[string]string `json:"detail,omitempty"`

	listener net.Listener
	cancel   context.CancelFunc
	onClose  func()
	conns    atomic.Int64
}

// snapshot copies what a caller may read while connections come and go.
func (f *Forward) snapshot() *Forward {
	return &Forward{ID: f.ID, Kind: f.Kind, Title: f.Title, TargetID: f.TargetID, TargetName: f.TargetName,
		SessionID: f.SessionID, Port: f.Port, ListenPort: f.ListenPort, CreatedAt: f.CreatedAt,
		ExpiresAt: f.ExpiresAt, Conns: f.conns.Load(), Detail: f.Detail}
}

// Spec is what to open.
type Spec struct {
	Kind       string
	Title      string
	TargetID   int64
	TargetName string
	SessionID  *int64
	Port       int
	TTL        time.Duration
	Detail     map[string]string
	// OnClose runs once when the forward goes away, however it goes away.
	OnClose func()
}

// Manager owns every open forward. They live in memory: a restart closes them
// all, which is the right default for something that widens what is reachable.
type Manager struct {
	BindHost string
	PortLo   int
	PortHi   int

	mu       sync.Mutex
	next     int64
	forwards map[int64]*Forward
}

// New builds a manager listening on bindHost within [lo, hi].
func New(bindHost string, lo, hi int) *Manager {
	return &Manager{BindHost: bindHost, PortLo: lo, PortHi: hi, forwards: map[int64]*Forward{}}
}

// ErrFull means every port in the range is taken.
var ErrFull = errors.New("no free forwarding port; stop one that is no longer needed")

// Open starts listening and carries each connection to 127.0.0.1:spec.Port on
// the target. The destination is always loopback: this exists to reach what a
// target keeps to itself, not to make the control plane a way into its network.
func (m *Manager) Open(dial Dial, spec Spec) (*Forward, error) {
	if spec.Port < 1 || spec.Port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535")
	}
	if spec.TTL <= 0 {
		spec.TTL = 4 * time.Hour
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var ln net.Listener
	for p := m.PortLo; p <= m.PortHi; p++ {
		l, err := net.Listen("tcp", net.JoinHostPort(m.BindHost, strconv.Itoa(p)))
		if err == nil {
			ln = l
			break
		}
	}
	if ln == nil {
		return nil, ErrFull
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := float64(time.Now().UnixNano()) / 1e9
	m.next++
	f := &Forward{ID: m.next, Kind: spec.Kind, Title: spec.Title, TargetID: spec.TargetID,
		TargetName: spec.TargetName, SessionID: spec.SessionID, Port: spec.Port,
		ListenPort: ln.Addr().(*net.TCPAddr).Port, CreatedAt: now, ExpiresAt: now + spec.TTL.Seconds(),
		Detail: spec.Detail, listener: ln, cancel: cancel, onClose: spec.OnClose}
	if f.Kind == "" {
		f.Kind = "port"
	}
	m.forwards[f.ID] = f
	go f.serve(ctx, dial)
	return f.snapshot(), nil
}

func (f *Forward) serve(ctx context.Context, dial Dial) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(f.Port))
	for {
		client, err := f.listener.Accept()
		if err != nil {
			return // the listener was closed
		}
		f.conns.Add(1)
		go func() {
			defer client.Close()
			dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			upstream, err := dial(dctx, addr)
			cancel()
			if err != nil {
				return
			}
			defer upstream.Close()
			done := make(chan struct{}, 2)
			pipe := func(dst, src net.Conn) {
				_, _ = io.Copy(dst, src)
				// Half-close lets the other direction drain instead of cutting a
				// response off when the request side finishes first.
				if c, ok := dst.(interface{ CloseWrite() error }); ok {
					_ = c.CloseWrite()
				}
				done <- struct{}{}
			}
			go pipe(upstream, client)
			go pipe(client, upstream)
			select {
			case <-done:
				select {
				case <-done:
				case <-ctx.Done():
				case <-time.After(30 * time.Second):
				}
			case <-ctx.Done():
			}
		}()
	}
}

// List is every open forward, oldest first.
func (m *Manager) List() []*Forward {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Forward, 0, len(m.forwards))
	for _, f := range m.forwards {
		out = append(out, f.snapshot())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Close stops one forward and reports whether it existed.
func (m *Manager) Close(id int64) bool {
	m.mu.Lock()
	f, ok := m.forwards[id]
	delete(m.forwards, id)
	m.mu.Unlock()
	if !ok {
		return false
	}
	f.cancel()
	_ = f.listener.Close()
	if f.onClose != nil {
		f.onClose()
	}
	return true
}

// Reap closes what has expired and whatever belongs to a session that ended.
// sessionEnded is asked only about forwards that name a session.
func (m *Manager) Reap(now float64, sessionEnded func(id int64) bool) []int64 {
	var gone []int64
	for _, f := range m.List() {
		if now >= f.ExpiresAt || (f.SessionID != nil && sessionEnded != nil && sessionEnded(*f.SessionID)) {
			if m.Close(f.ID) {
				gone = append(gone, f.ID)
			}
		}
	}
	return gone
}

// CloseAll stops everything; the server calls it on the way down.
func (m *Manager) CloseAll() {
	for _, f := range m.List() {
		m.Close(f.ID)
	}
}
