package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// recordingWriter is the underlying client writer for rootHandler tests; it
// has no timeout/deadline support, matching real writers loosely, and can
// observe writes as they happen (onWrite runs on the handler goroutine).
type recordingWriter struct {
	header  http.Header
	body    bytes.Buffer
	code    int
	onWrite func([]byte)
}

func (w *recordingWriter) Header() http.Header { return w.header }

func (w *recordingWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	if w.onWrite != nil {
		w.onWrite(p)
	}
	return w.body.Write(p)
}

func (w *recordingWriter) Flush() {}

func testRespWriter(respMaxSize int64, gzipAccept bool) (*respWriter, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	w := &respWriter{
		ResponseWriter: rec,
		requestPath:    "/api/data",
		respMaxSize:    respMaxSize,
		gzipAccept:     gzipAccept,
	}
	return w, rec
}

// TestRespWriterGzipDecision covers which responses get compressed once the
// streaming threshold is crossed.
func TestRespWriterGzipDecision(t *testing.T) {
	payload := strings.Repeat("a", gzipMinSize*2)

	cases := []struct {
		name        string
		requestPath string
		accept      bool
		setHeader   func(h http.Header)
		wantCE      string
	}{
		{
			name:        "html accepted",
			requestPath: "/api/data",
			accept:      true,
			setHeader: func(h http.Header) {
				h.Set("Content-Type", "text/html")
				h.Set("X-Proxy", "upstream-forged")
			},
			wantCE: "gzip",
		},
		{
			name:        "client does not accept gzip",
			requestPath: "/api/data",
			accept:      false,
			setHeader:   func(h http.Header) { h.Set("Content-Type", "text/html") },
		},
		{
			name:        "image content type",
			requestPath: "/api/data",
			accept:      true,
			setHeader:   func(h http.Header) { h.Set("Content-Type", "image/png") },
		},
		{
			name:        "compressed file extension",
			requestPath: "/assets/bundle.zip",
			accept:      true,
			setHeader:   func(h http.Header) { h.Set("Content-Type", "application/zip") },
		},
		{
			name:        "already encoded by upstream",
			requestPath: "/api/data",
			accept:      true,
			setHeader: func(h http.Header) {
				h.Set("Content-Type", "text/html")
				h.Set("Content-Encoding", "br")
			},
			// an upstream-supplied encoding is passed through untouched
			wantCE: "br",
		},
		{
			name:        "range response",
			requestPath: "/api/data",
			accept:      true,
			setHeader: func(h http.Header) {
				h.Set("Content-Type", "text/html")
				h.Set("Content-Range", "bytes 0-1/2")
			},
		},
		{
			name:        "server-sent events",
			requestPath: "/api/stream",
			accept:      true,
			setHeader:   func(h http.Header) { h.Set("Content-Type", "text/event-stream") },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, rec := testRespWriter(0, tc.accept)
			w.requestPath = tc.requestPath
			w.WriteHeader(http.StatusOK)
			tc.setHeader(w.Header())

			if _, err := w.Write([]byte(payload)); err != nil {
				t.Fatalf("write: %v", err)
			}
			w.finish()

			if ce := rec.Header().Get("Content-Encoding"); ce != tc.wantCE {
				t.Fatalf("Content-Encoding = %q, want %q", ce, tc.wantCE)
			}

			var got []byte
			if tc.wantCE == "gzip" {
				if cl := rec.Header().Get("Content-Length"); cl != "" {
					t.Fatalf("Content-Length = %q on gzip stream, want none", cl)
				}
				zr, err := gzip.NewReader(rec.Body)
				if err != nil {
					t.Fatalf("gzip reader: %v", err)
				}
				got, err = io.ReadAll(zr)
				if err != nil {
					t.Fatalf("gunzip: %v", err)
				}
			} else {
				got = rec.Body.Bytes()
			}
			if string(got) != payload {
				t.Fatalf("body roundtrip mismatch: got %d bytes, want %d", len(got), len(payload))
			}

			// the gateway badge overrides any upstream-supplied value
			if got := rec.Header().Get("X-Proxy"); got != "InnerStack/"+version {
				t.Fatalf("X-Proxy = %q, want %q", got, "InnerStack/"+version)
			}
		})
	}
}

// TestRespWriterSmallResponseBuffered: below the threshold the body stays
// buffered and finishes with an exact Content-Length.
func TestRespWriterSmallResponseBuffered(t *testing.T) {
	w, rec := testRespWriter(0, false)
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/html")

	payload := strings.Repeat("x", 512)
	if _, err := w.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body delivered before finish: %d bytes", rec.Body.Len())
	}

	w.finish()

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if cl := rec.Header().Get("Content-Length"); cl != strconv.Itoa(len(payload)) {
		t.Fatalf("Content-Length = %q, want %d", cl, len(payload))
	}
	if rec.Body.String() != payload {
		t.Fatal("body mismatch")
	}
	if w.writeSize != int64(len(payload)) || w.compSize != int64(len(payload)) {
		t.Fatalf("size accounting: raw = %d, comp = %d", w.writeSize, w.compSize)
	}
}

// TestRespWriterStreamsLargeResponse: past the threshold the response commits
// immediately and streams through instead of accumulating to the end.
func TestRespWriterStreamsLargeResponse(t *testing.T) {
	w, rec := testRespWriter(0, false)
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", "2048")

	chunk := strings.Repeat("b", 1024)
	if _, err := w.Write([]byte(chunk)); err != nil {
		t.Fatalf("write: %v", err)
	}

	// committed and delivered without waiting for finish
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want committed 200", rec.Code)
	}
	if rec.Body.Len() != len(chunk) {
		t.Fatalf("streamed bytes = %d, want %d", rec.Body.Len(), len(chunk))
	}

	if _, err := w.Write([]byte(chunk)); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.finish()

	if rec.Body.Len() != 2048 {
		t.Fatalf("body = %d bytes, want 2048", rec.Body.Len())
	}
	// pass-through keeps the upstream-declared length
	if cl := rec.Header().Get("Content-Length"); cl != "2048" {
		t.Fatalf("Content-Length = %q, want 2048", cl)
	}
}

// TestRespWriterFlushCommitsPassThrough: a Flush before the gzip threshold
// (server-sent events) commits in pass-through mode and delivers immediately.
func TestRespWriterFlushCommitsPassThrough(t *testing.T) {
	w, rec := testRespWriter(0, true)
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/event-stream")

	if _, err := w.Write([]byte("data: first\n\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("bytes delivered before Flush: %d", rec.Body.Len())
	}

	w.Flush()

	if !rec.Flushed {
		t.Fatal("underlying writer not flushed")
	}
	if rec.Body.String() != "data: first\n\n" {
		t.Fatalf("event not delivered on flush: %q", rec.Body.String())
	}
	if ce := rec.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding = %q, want none", ce)
	}

	if _, err := w.Write([]byte("data: second\n\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.finish()

	if want := "data: first\n\ndata: second\n\n"; rec.Body.String() != want {
		t.Fatalf("body = %q, want %q", rec.Body.String(), want)
	}
}

// TestRespWriterSizeCapAborts: exceeding the hard cap panics with
// http.ErrAbortHandler so net/http closes the connection mid-transfer.
func TestRespWriterSizeCapAborts(t *testing.T) {
	w, _ := testRespWriter(1024, false)
	w.WriteHeader(http.StatusOK)

	// filling up to exactly the cap is allowed
	if _, err := w.Write([]byte(strings.Repeat("c", 1024))); err != nil {
		t.Fatalf("write at cap: %v", err)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected abort panic")
		}
		if r != http.ErrAbortHandler {
			t.Fatalf("panic value = %v, want http.ErrAbortHandler", r)
		}
	}()
	w.Write([]byte("x")) // one byte over
	t.Fatal("expected panic")
}

// TestRespWriterEventStreamExemptFromCap: event streams legitimately
// accumulate bytes over their lifetime and must not be killed by the cap.
func TestRespWriterEventStreamExemptFromCap(t *testing.T) {
	w, rec := testRespWriter(int64(gzipMinSize), false)
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "text/event-stream")

	total := 0
	for range 8 {
		if _, err := w.Write([]byte(strings.Repeat("e", gzipMinSize/2))); err != nil {
			t.Fatalf("write: %v", err)
		}
		total += gzipMinSize / 2
	}
	w.finish()

	if total <= int(w.respMaxSize) {
		t.Fatalf("test payload %d does not exceed cap %d", total, w.respMaxSize)
	}
	if rec.Body.Len() != total {
		t.Fatalf("body = %d bytes, want %d", rec.Body.Len(), total)
	}
}

// TestRespWriterRedirect models the Redirect route branch: Location with an
// explicit 3xx status and no body finishes verbatim.
func TestRespWriterRedirect(t *testing.T) {
	w, rec := testRespWriter(0, false)
	w.Header().Set("Location", "https://example.com/")
	w.WriteHeader(http.StatusFound)
	w.finish()

	if rec.Code != http.StatusFound {
		t.Fatalf("code = %d, want 302", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("redirect body = %d bytes, want 0", rec.Body.Len())
	}
}

// TestRespWriterLocationStatusForwarded: a proxied response carrying Location
// (e.g. 201 Created) keeps its status and body; only the Redirect route type
// produces 302s.
func TestRespWriterLocationStatusForwarded(t *testing.T) {
	w, rec := testRespWriter(0, false)
	w.Header().Set("Location", "/widgets/123")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write([]byte(`{"id":123}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.finish()

	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201 (status must not be coerced to 302)", rec.Code)
	}
	if rec.Body.String() != `{"id":123}` {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

// stubDomainRoute installs a single route for host "example.com", keyed by
// route.Path, and restores cfg afterwards.
//
// The swap is unsynchronized: it is safe only while no test in this package
// uses t.Parallel or starts the configRefresh / ipLimiterCleaner goroutines.
func stubDomainRoute(t *testing.T, route *DomainEntryRoute) {
	t.Helper()

	entry := &DomainEntry{
		Domain:      &inapi.GatewayIngressDeploy{Domain: "example.com"},
		indexRoutes: map[string]*DomainEntryRoute{route.Path: route},
	}

	oldLimit, oldServer, oldIndex := cfg.Limit, cfg.Server, cfg.indexDomains
	cfg.Limit = ConfigLimit{Rate: 1 << 20, Burst: 1 << 20}
	cfg.Server.MaxBodySize = 16 << 20
	cfg.Server.WriteByteTimeout = 60
	cfg.indexDomains = map[string]*DomainEntry{"example.com": entry}
	t.Cleanup(func() {
		cfg.Limit, cfg.Server, cfg.indexDomains = oldLimit, oldServer, oldIndex
	})
}

// stubProxyRoute installs a single proxied route for host "example.com" whose
// ReverseProxy uses rt instead of dialing; see stubDomainRoute for the cfg
// swap constraints.
func stubProxyRoute(t *testing.T, rt http.RoundTripper) {
	t.Helper()

	u, err := url.Parse("http://upstream.invalid")
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	rp := newReverseProxy(u)
	rp.Transport = rt

	stubDomainRoute(t, &DomainEntryRoute{
		Type:         inapi.GatewayIngressType_Instance,
		Path:         "/",
		Urls:         []*url.URL{u},
		reverseProxy: []*httputil.ReverseProxy{rp},
	})
}

func stubUpstreamResponse(
	r *http.Request,
	status int,
	contentType string,
	body io.ReadCloser,
	length int64,
) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Status:        strconv.Itoa(status) + " OK",
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{contentType}},
		Body:          body,
		ContentLength: length,
		Request:       r,
	}
}

// TestRootHandlerStreamsProxiedResponse drives rootHandler end to end with a
// stubbed upstream: the first upstream chunk must reach the client while the
// upstream response is still open, proving the proxy path streams instead of
// buffering the whole body.
func TestRootHandlerStreamsProxiedResponse(t *testing.T) {
	const (
		firstEvent  = "data: first\n\n"
		secondEvent = "data: second\n\n"
	)

	pr, pw := io.Pipe()
	// unblock a parked driver write if the handler path stops consuming
	t.Cleanup(func() { pw.CloseWithError(errors.New("test done")) })
	delivered := make(chan struct{}, 1)
	driverErr := make(chan error, 1)

	go func() {
		if _, err := pw.Write([]byte(firstEvent)); err != nil {
			driverErr <- err
			return
		}
		// hold the upstream response open until the gateway forwarded the
		// first chunk; under full buffering this times out and fails the test
		select {
		case <-delivered:
		case <-time.After(5 * time.Second):
			pw.Close()
			driverErr <- errors.New("first chunk not forwarded while upstream response still open")
			return
		}
		if _, err := pw.Write([]byte(secondEvent)); err != nil {
			driverErr <- err
			return
		}
		pw.Close()
	}()

	stubProxyRoute(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		// unknown length + event stream: ReverseProxy flushes per chunk
		return stubUpstreamResponse(r, http.StatusOK, "text/event-stream", pr, -1), nil
	}))

	rw := &recordingWriter{
		header: http.Header{},
		onWrite: func(p []byte) {
			if bytes.Contains(p, []byte(firstEvent)) {
				select {
				case delivered <- struct{}{}:
				default:
				}
			}
		},
	}

	req := httptest.NewRequest("GET", "http://example.com/api/stream", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rootHandler(rw, req)

	select {
	case err := <-driverErr:
		t.Fatalf("upstream driver: %v", err)
	default:
	}

	if rw.code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rw.code)
	}
	if got := rw.body.String(); got != firstEvent+secondEvent {
		t.Fatalf("body = %q, want both events in order", got)
	}
	// event streams must not be compressed (would delay or swallow events)
	if ce := rw.header.Get("Content-Encoding"); ce != "" {
		t.Fatalf("Content-Encoding = %q, want none", ce)
	}
	if got := rw.header.Get("X-Proxy"); got != "InnerStack/"+version {
		t.Fatalf("X-Proxy = %q", got)
	}
}

// TestRootHandlerAbortsOversizedResponse drives the size cap end to end: the
// handler panics with http.ErrAbortHandler so net/http closes the connection
// mid-transfer instead of delivering a truncated body as complete.
func TestRootHandlerAbortsOversizedResponse(t *testing.T) {
	const total = 8 << 10

	stubProxyRoute(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := io.NopCloser(strings.NewReader(strings.Repeat("z", total)))
		return stubUpstreamResponse(r, http.StatusOK, "text/plain", body, total), nil
	}))
	// set after stubProxyRoute so its cfg snapshot restores the prior value
	cfg.Server.MaxRespSize = 4 << 10

	rw := &recordingWriter{header: http.Header{}}
	req := httptest.NewRequest("GET", "http://example.com/api/data", nil)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected abort panic")
		}
		if r != http.ErrAbortHandler {
			t.Fatalf("panic value = %v, want http.ErrAbortHandler", r)
		}
		if rw.body.Len() > 4<<10 {
			t.Fatalf("delivered %d bytes, cap is %d", rw.body.Len(), 4<<10)
		}
	}()
	rootHandler(rw, req)
	t.Fatal("expected panic")
}

// TestRootHandlerGzipUnknownLength: ReverseProxy flushes responses of unknown
// Content-Length immediately and per chunk; the first Flush must commit under
// the normal gzip rules instead of locking the response into pass-through,
// or every chunked/dynamic upstream response would cross the wire raw.
func TestRootHandlerGzipUnknownLength(t *testing.T) {
	payload := strings.Repeat("g", 100<<10)

	pr, pw := io.Pipe()
	t.Cleanup(func() { pw.CloseWithError(errors.New("test done")) })
	delivered := make(chan struct{}, 1)
	driverErr := make(chan error, 1)

	go func() {
		if _, err := pw.Write([]byte(payload[:64<<10])); err != nil {
			driverErr <- err
			return
		}
		// hold the response open until the gateway forwarded compressed bytes
		select {
		case <-delivered:
		case <-time.After(5 * time.Second):
			driverErr <- errors.New("no compressed bytes forwarded while upstream response still open")
			return
		}
		if _, err := pw.Write([]byte(payload[64<<10:])); err != nil {
			driverErr <- err
			return
		}
		pw.Close()
	}()

	stubProxyRoute(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return stubUpstreamResponse(r, http.StatusOK, "text/html", pr, -1), nil
	}))

	rw := &recordingWriter{
		header: http.Header{},
		onWrite: func(p []byte) {
			select {
			case delivered <- struct{}{}:
			default:
			}
		},
	}

	req := httptest.NewRequest("GET", "http://example.com/api/data", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rootHandler(rw, req)

	select {
	case err := <-driverErr:
		t.Fatalf("upstream driver: %v", err)
	default:
	}

	if ce := rw.header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip for unknown-length response", ce)
	}
	zr, err := gzip.NewReader(&rw.body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("body roundtrip mismatch: got %d bytes, want %d", len(got), len(payload))
	}
	if rw.body.Len() >= len(payload) {
		t.Fatalf("compressed size %d not smaller than raw %d", rw.body.Len(), len(payload))
	}
}
