package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func proxyRoute(path, backend string) *inapi.GatewayIngressDeploy_HttpRoute {
	return routeWithBackend(gatewayIngressTypeProxy, path, backend)
}

// stubProxyTunnelRoute installs a proxy route for host "example.com"; see
// stubDomainRoute for the cfg swap constraints.
func stubProxyTunnelRoute(t *testing.T, path string, backendURL string) {
	t.Helper()

	u, err := url.Parse(backendURL)
	if err != nil {
		t.Fatalf("parse backend url: %v", err)
	}
	rp := newReverseProxy(u)
	rp.FlushInterval = -1

	stubDomainRoute(t, &DomainEntryRoute{
		Type:         gatewayIngressTypeProxy,
		Path:         path,
		reverseProxy: []*httputil.ReverseProxy{rp},
	})
}

// dialGatewayRaw serves rootHandler over real TCP, sends rawReq and reads
// the response head; conn and reader stay usable for raw bytes after a 101.
func dialGatewayRaw(t *testing.T, rawReq string) (*http.Response, net.Conn, *bufio.Reader) {
	t.Helper()

	gateway := httptest.NewServer(http.HandlerFunc(rootHandler))
	t.Cleanup(gateway.Close)

	conn, err := net.Dial("tcp", strings.TrimPrefix(gateway.URL, "http://"))
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	if _, err := fmt.Fprint(conn, rawReq); err != nil {
		t.Fatalf("write raw request: %v", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp, conn, br
}

func TestParseProxyTarget(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		want    string // normalized URL string, empty means error
	}{
		{"bare host port", "127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"bare ipv6", "[::1]:8080", "http://[::1]:8080"},
		{"http scheme", "http://127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"https scheme", "https://example.com:8443", "https://example.com:8443"},
		{"path prefix", "http://127.0.0.1:8080/base", "http://127.0.0.1:8080/base"},
		{"spaces trimmed", "  127.0.0.1:8080  ", "http://127.0.0.1:8080"},
		{"empty", "", ""},
		{"missing host", "http://:8080", ""},
		{"bad scheme", "ftp://127.0.0.1:8080", ""},
		{"no port no scheme", "localhost", "http://localhost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := parseProxyTarget(tt.backend)
			if tt.want == "" {
				if err == nil {
					t.Fatalf("parseProxyTarget(%q) = %v, want error", tt.backend, u)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseProxyTarget(%q) error: %v", tt.backend, err)
			}
			if u.String() != tt.want {
				t.Fatalf("parseProxyTarget(%q) = %q, want %q", tt.backend, u.String(), tt.want)
			}
		})
	}
}

// TestUpgradeRequestType pins RFC 7230: Upgrade counts only when Connection
// carries the "upgrade" token.
func TestUpgradeRequestType(t *testing.T) {
	tests := []struct {
		name       string
		connection string
		upgrade    string
		want       string
	}{
		{"plain upgrade", "Upgrade", "websocket", "websocket"},
		{"lowercase token", "upgrade", "websocket", "websocket"},
		{"token in list", "keep-alive, Upgrade", "websocket", "websocket"},
		{"token with spaces", "keep-alive ,  upgrade", "h2c", "h2c"},
		{"no connection", "", "websocket", ""},
		{"connection without token", "keep-alive", "websocket", ""},
		{"connection without upgrade header", "Upgrade", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.connection != "" {
				h.Set("Connection", tt.connection)
			}
			if tt.upgrade != "" {
				h.Set("Upgrade", tt.upgrade)
			}
			if got := upgradeRequestType(h); got != tt.want {
				t.Fatalf("upgradeRequestType(%q, %q) = %q, want %q",
					tt.connection, tt.upgrade, got, tt.want)
			}
		})
	}
}

// TestProxyRouteNonUpgradeHeaders: a plain request reaches the backend with
// forwarding headers and the full original path.
func TestProxyRouteNonUpgradeHeaders(t *testing.T) {

	var (
		gotPath   string
		gotHost   string
		gotHeader http.Header
	)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotHost, gotHeader = r.URL.RequestURI(), r.Host, r.Header.Clone()
		w.Write([]byte("backend-ok"))
	}))
	defer backend.Close()

	stubProxyTunnelRoute(t, "/tunnel", backend.URL)

	req := httptest.NewRequest("GET", "http://example.com/tunnel/sub?a=1", nil)
	rec := httptest.NewRecorder()
	rootHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "backend-ok" {
		t.Fatalf("body = %q, want backend-ok", rec.Body.String())
	}

	if gotPath != "/tunnel/sub?a=1" {
		t.Fatalf("backend request URI = %q, want /tunnel/sub?a=1", gotPath)
	}
	if gotHost != "example.com" {
		t.Fatalf("backend Host = %q, want example.com", gotHost)
	}
	// httptest.NewRequest RemoteAddr is 192.0.2.1:1234.
	if v := gotHeader.Get("X-Real-IP"); v != "192.0.2.1" {
		t.Fatalf("X-Real-IP = %q, want 192.0.2.1", v)
	}
	if v := gotHeader.Get("X-Forwarded-For"); v != "192.0.2.1" {
		t.Fatalf("X-Forwarded-For = %q, want 192.0.2.1", v)
	}
	if v := gotHeader.Get("X-Forwarded-Proto"); v != "http" {
		t.Fatalf("X-Forwarded-Proto = %q, want http", v)
	}
}

// TestProxyRouteBadBackend: a route with no valid target answers 502 on
// both the upgrade and the plain path.
func TestProxyRouteBadBackend(t *testing.T) {

	stubDomainRoute(t, &DomainEntryRoute{
		Type: gatewayIngressTypeProxy,
		Path: "/tunnel",
	})

	// Upgrade request: handled early by proxyTunnelServe.
	req := httptest.NewRequest("GET", "http://example.com/tunnel", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	rootHandler(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("upgrade request code = %d, want 502", rec.Code)
	}

	// Plain request: handled by the regular proxied path.
	req2 := httptest.NewRequest("GET", "http://example.com/tunnel", nil)
	rec2 := httptest.NewRecorder()
	rootHandler(rec2, req2)
	if rec2.Code != http.StatusBadGateway {
		t.Fatalf("plain request code = %d, want 502", rec2.Code)
	}
}

// TestRootHandlerStripsHostPort: a port in the Host header must still match
// the domain index instead of answering a bogus 403.
func TestRootHandlerStripsHostPort(t *testing.T) {

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("backend-ok"))
	}))
	defer backend.Close()

	stubProxyTunnelRoute(t, "/tunnel", backend.URL)

	req := httptest.NewRequest("GET", "http://example.com/tunnel/x", nil)
	req.Host = "example.com:443"
	rec := httptest.NewRecorder()
	rootHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (port-suffixed Host must still route)", rec.Code)
	}
	if rec.Body.String() != "backend-ok" {
		t.Fatalf("body = %q, want backend-ok", rec.Body.String())
	}
}

// TestProxyRouteUpgradeNeedsHijackableWriter: an upgrade on a writer
// without Hijack (HTTP/2) answers 501, not a silent empty 200.
func TestProxyRouteUpgradeNeedsHijackableWriter(t *testing.T) {

	// Valid target so check order is pinned; ResponseRecorder has no Hijack.
	stubProxyTunnelRoute(t, "/tunnel", "http://127.0.0.1:1")

	req := httptest.NewRequest("GET", "http://example.com/tunnel", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	rootHandler(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("code = %d, want 501", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "HTTP/1.1") {
		t.Fatalf("body = %q, want a message mentioning HTTP/1.1", rec.Body.String())
	}
}

// TestProxyRouteUpstreamUnreachable: a refused upstream answers 502 on both
// paths, no panic.
func TestProxyRouteUpstreamUnreachable(t *testing.T) {

	// Grab a port that is guaranteed closed.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	backendURL := "http://" + l.Addr().String()
	l.Close()

	stubProxyTunnelRoute(t, "/tunnel", backendURL)

	// Plain path.
	req := httptest.NewRequest("GET", "http://example.com/tunnel", nil)
	rec := httptest.NewRecorder()
	rootHandler(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("plain request code = %d, want 502", rec.Code)
	}

	// Upgrade path.
	resp, _, _ := dialGatewayRaw(t,
		"GET /tunnel HTTP/1.1\r\n"+
			"Host: example.com\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n\r\n")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("upgrade request status = %d, want 502", resp.StatusCode)
	}
}

// TestProxyRouteUpgradeTunnel proves the tunnel end to end over real TCP:
// upgrade forwarded, 101, raw bytes flow both directions.
func TestProxyRouteUpgradeTunnel(t *testing.T) {

	var (
		gotPath    string
		gotUpgrade string
		gotRealIP  string
		gotProxyAU string
	)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUpgrade = r.Header.Get("Upgrade")
		gotRealIP = r.Header.Get("X-Real-IP")
		gotProxyAU = r.Header.Get("Proxy-Authorization")

		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "not an upgrade", http.StatusBadRequest)
			return
		}

		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		conn, brw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()

		// 101 with the same upgrade token, as a WebSocket server would.
		if _, err := brw.WriteString(
			"HTTP/1.1 101 Switching Protocols\r\n" +
				"Upgrade: websocket\r\n" +
				"Connection: Upgrade\r\n\r\n"); err != nil {
			return
		}
		if err := brw.Flush(); err != nil {
			return
		}

		// Raw echo.
		io.Copy(conn, conn)
	}))
	defer backend.Close()

	stubProxyTunnelRoute(t, "/tunnel", backend.URL)

	resp, conn, br := dialGatewayRaw(t,
		"GET /tunnel/ws HTTP/1.1\r\n"+
			"Host: example.com\r\n"+
			"Upgrade: websocket\r\n"+
			"Connection: Upgrade\r\n"+
			"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n"+
			"Sec-WebSocket-Version: 13\r\n"+
			"Sec-WebSocket-Extensions: permessage-deflate; client_max_window_bits\r\n"+
			"Proxy-Authorization: Basic dXNlcjpwYXNz\r\n\r\n")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}

	const payload = "tunnel-payload"
	if _, err := conn.Write([]byte(payload)); err != nil {
		t.Fatalf("write tunnel payload: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatalf("read tunnel echo: %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("echo = %q, want %q", buf, payload)
	}

	if gotPath != "/tunnel/ws" {
		t.Fatalf("backend path = %q, want /tunnel/ws", gotPath)
	}
	if gotUpgrade != "websocket" {
		t.Fatalf("backend Upgrade = %q, want websocket", gotUpgrade)
	}
	// The raw client dialed 127.0.0.1, so X-Real-IP is the loopback address.
	ip, _, _ := net.SplitHostPort(conn.LocalAddr().String())
	if gotRealIP != ip {
		t.Fatalf("backend X-Real-IP = %q, want %q", gotRealIP, ip)
	}
	// Proxy-Authorization is hop-by-hop and stripped by ReverseProxy even
	// for upgrades (unlike nginx); authenticated upstreams need another
	// mechanism.
	if gotProxyAU != "" {
		t.Fatalf("backend Proxy-Authorization = %q, want stripped", gotProxyAU)
	}
}
