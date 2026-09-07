package api

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"

	"github.com/JeremiahM37/agentdeck/internal/terminal"
)

// termProxy serves an attached terminal on agentdeck's own origin.
//
// ttyd runs on the control plane, on loopback, on a port from a small range.
// The browser, however, may have reached agentdeck through the nginx vhost, a
// tailnet hostname, or a bare IP — and it used to build the terminal's URL from
// whatever hostname it happened to be using. On any path that is not the
// control plane itself that named the wrong machine (the reverse proxy, which
// runs no ttyd) and the tab simply refused to connect.
//
// Proxying it here means one origin for everything: no second port to expose,
// no mixed-content block when the page is https, and no unauthenticated shell
// listening on the network.
func (s *Server) termProxy(w http.ResponseWriter, r *http.Request) {
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// only ports this process actually spawned, so the prefix cannot be aimed at
	// some other service listening on loopback
	if port < terminal.PortLo || port > terminal.PortHi || !s.Terminals.Owns(port) {
		http.Error(w, "no terminal is attached on that port", http.StatusNotFound)
		return
	}
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	proxy := &httputil.ReverseProxy{
		// ttyd is mounted with --base-path, so it expects the prefix to arrive
		// intact; the path is passed through rather than stripped.
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
		// a terminal closing is ordinary, not an error worth a 502 page
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			s.Log.Info("terminal proxy ended", "port", port, "err", err)
			http.Error(w, "the terminal has closed", http.StatusGone)
		},
	}
	// ReverseProxy carries the websocket upgrade through on its own; it only
	// needs the hop-by-hop headers left alone, which it does when Upgrade is set
	proxy.ServeHTTP(w, r)
}

// terminalURL is the path the UI opens for an attached terminal. Relative on
// purpose: whatever origin reached agentdeck is the origin that works.
func terminalURL(port int) string { return terminal.BasePath(port) + "/" }

// stripTrailing keeps /term/7710 and /term/7710/ equivalent, because a user who
// types the first should not get a blank page.
func normalizeTermPath(p string) string { return strings.TrimSuffix(p, "/") }
