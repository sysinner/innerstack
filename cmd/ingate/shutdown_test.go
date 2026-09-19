package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestServerShutdownBounded verifies serverShutdown returns once the
// shutdown deadline expires even while a connection stays active: a handler
// holding an open stream must not hang the process past the configured
// bound (the pre-fix Shutdown(context.Background()) never returned).
func TestServerShutdownBounded(t *testing.T) {
	started := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		})}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			t.Errorf("serve: %v", err)
		}
	}()

	// Hold an active connection for the whole test.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			"http://"+ln.Addr().String()+"/", nil)
		if err != nil {
			t.Errorf("new request: %v", err)
			return
		}
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			res.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}

	oldTimeout := cfg.Server.ShutdownTimeout
	cfg.Server.ShutdownTimeout = 1
	t.Cleanup(func() { cfg.Server.ShutdownTimeout = oldTimeout })

	tn := time.Now()
	serverShutdown(srv, "test")
	elapsed := time.Since(tn)

	// Release the handler and the client goroutine.
	cancel()
	<-clientDone

	if elapsed < 900*time.Millisecond {
		t.Fatalf("shutdown returned in %v, want it to wait for the 1s deadline", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("shutdown took %v, want it bounded near the 1s deadline", elapsed)
	}
}
