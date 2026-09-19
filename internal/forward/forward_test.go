package forward

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func localDial(ctx context.Context, addr string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "tcp", addr)
}

func portOf(t *testing.T, url string) int {
	t.Helper()
	var port int
	if _, err := fmt.Sscanf(url[strings.LastIndex(url, ":")+1:], "%d", &port); err != nil {
		t.Fatal(err)
	}
	return port
}

func TestForwardCarriesARealApplicationUnmodified(t *testing.T) {
	// The reason this is a port forward and not a path proxy: absolute paths and
	// redirects have to keep working without the application knowing.
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "/assets/app.js", http.StatusFound)
		case "/assets/app.js":
			fmt.Fprintf(w, "host=%s", r.Host)
		default:
			http.NotFound(w, r)
		}
	}))
	defer app.Close()
	m := New("127.0.0.1", 39100, 39120)
	defer m.CloseAll()
	f, err := m.Open(localDial, Spec{Title: "dev server", Port: portOf(t, app.URL)})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", f.ListenPort))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	want := fmt.Sprintf("host=127.0.0.1:%d", f.ListenPort)
	if resp.StatusCode != 200 || string(body) != want {
		t.Fatalf("got %d %q, want %q — the redirect must resolve against the forwarded port", resp.StatusCode, body, want)
	}
	if got := m.List(); len(got) != 1 || got[0].Conns == 0 || got[0].Kind != "port" {
		t.Errorf("list: %+v", got)
	}
}

func TestForwardOnlyEverReachesTheTargetsLoopback(t *testing.T) {
	seen := make(chan string, 4)
	spy := func(ctx context.Context, addr string) (net.Conn, error) {
		seen <- addr
		return nil, fmt.Errorf("refused")
	}
	m := New("127.0.0.1", 39130, 39140)
	defer m.CloseAll()
	f, err := m.Open(spy, Spec{Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", f.ListenPort))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(conn) // closed by the forward once the dial fails
	conn.Close()
	if got := <-seen; got != "127.0.0.1:8080" || len(seen) != 0 {
		t.Fatalf("dialed %q, then %d more", got, len(seen))
	}
	for _, bad := range []int{0, -1, 70000} {
		if _, err := m.Open(spy, Spec{Port: bad}); err == nil {
			t.Errorf("port %d must be refused", bad)
		}
	}
}

func TestForwardsExpireEndWithTheirSessionAndRunTheirCleanup(t *testing.T) {
	m := New("127.0.0.1", 39150, 39170)
	defer m.CloseAll()
	cleaned := map[string]bool{}
	sid := int64(7)
	short, _ := m.Open(localDial, Spec{Title: "short", Port: 1, TTL: time.Minute, OnClose: func() { cleaned["short"] = true }})
	owned, _ := m.Open(localDial, Spec{Title: "owned", Port: 1, SessionID: &sid, OnClose: func() { cleaned["owned"] = true }})
	kept, _ := m.Open(localDial, Spec{Title: "kept", Port: 1})
	now := float64(time.Now().Unix())
	if gone := m.Reap(now, func(int64) bool { return false }); len(gone) != 0 {
		t.Fatalf("nothing is due yet: %v", gone)
	}
	gone := m.Reap(now+120, func(id int64) bool { return id == sid })
	if len(gone) != 2 || !cleaned["short"] || !cleaned["owned"] {
		t.Fatalf("gone %v cleaned %v", gone, cleaned)
	}
	if left := m.List(); len(left) != 1 || left[0].ID != kept.ID {
		t.Fatalf("left: %+v", left)
	}
	// A closed forward stops listening: the port is genuinely shut, not merely unlisted.
	for _, f := range []*Forward{short, owned} {
		if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", f.ListenPort), time.Second); err == nil {
			c.Close()
			t.Errorf("forward %d still accepts connections", f.ID)
		}
	}
	if m.Close(short.ID) {
		t.Error("closing twice must report that it was already gone")
	}
}

func TestPortRangeRunsOut(t *testing.T) {
	m := New("127.0.0.1", 39180, 39181)
	defer m.CloseAll()
	for i := 0; i < 2; i++ {
		if _, err := m.Open(localDial, Spec{Port: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Open(localDial, Spec{Port: 1}); err != ErrFull {
		t.Fatalf("want ErrFull, got %v", err)
	}
}
