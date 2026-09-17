// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package status

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func TestReporterSetAndFlush(t *testing.T) {
	var (
		gotSecret string
		gotBody   inapi.InagentStatusReport
		hits      int32
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		gotSecret = r.Header.Get("X-Secret-Key")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer srv.Close()

	// Reset reporter state.
	resetState()

	SetBoot()
	SetSpecLoad(inapi.AppStageStateSuccess, "")
	SetTaskRun(inapi.AppStageStateRunning, "1/2 tasks running")

	Flush(&inapi.HostletStatusEndpoint{Url: srv.URL, SecretKey: "k1"},
		"myapp", 0)

	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("hits=%d want 1", hits)
	}
	if gotSecret != "k1" {
		t.Fatalf("secret=%q want k1", gotSecret)
	}
	if gotBody.InstanceName != "myapp" || gotBody.ReplicaId != 0 {
		t.Fatalf("identity=%+v", &gotBody)
	}
	if len(gotBody.Stages) != 3 {
		t.Fatalf("stages=%d want 3", len(gotBody.Stages))
	}

	// Dirty cleared after a successful flush: a second immediate flush should
	// not hit the server (heartbeat not elapsed).
	Flush(&inapi.HostletStatusEndpoint{Url: srv.URL, SecretKey: "k1"},
		"myapp", 0)
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("hits=%d want 1 (no resend when not dirty)", hits)
	}

	// A new transition re-dirties and flushes again.
	SetTaskRun(inapi.AppStageStateSuccess, "2/2 tasks done")
	Flush(&inapi.HostletStatusEndpoint{Url: srv.URL, SecretKey: "k1"},
		"myapp", 0)
	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("hits=%d want 2 after re-dirty", hits)
	}
}

func TestReporterNoEndpoint(t *testing.T) {
	resetState()
	SetBoot()
	// No endpoint / empty URL -> no panic, no send.
	Flush(nil, "myapp", 0)
	Flush(&inapi.HostletStatusEndpoint{Url: "", SecretKey: "x"}, "myapp", 0)
}

func TestReporterAuthFailureKeepsDirty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	resetState()

	SetSpecLoad(inapi.AppStageStateFailed, "bad")
	Flush(&inapi.HostletStatusEndpoint{Url: srv.URL, SecretKey: "k"}, "myapp", 0)

	// 401 -> dirty must remain so it retries next tick.
	mu.Lock()
	defer mu.Unlock()
	if !dirty {
		t.Fatal("dirty should remain true after non-200 response")
	}
}

func TestReporterSetRevisionResets(t *testing.T) {
	resetState()

	SetRevision(1)
	SetSpecLoad(inapi.AppStageStateSuccess, "")
	if len(stages) != 1 {
		t.Fatalf("stages=%d want 1", len(stages))
	}

	// A revision change clears prior stages.
	SetRevision(2)
	if len(stages) != 0 {
		t.Fatalf("stages=%d want 0 after revision change", len(stages))
	}

	// New stages are stamped with the new revision.
	SetSpecLoad(inapi.AppStageStateSuccess, "")
	if stages[0].Revision != 2 {
		t.Fatalf("revision=%d want 2", stages[0].Revision)
	}

	// Same revision is a no-op (stages preserved).
	SetRevision(2)
	if len(stages) != 1 {
		t.Fatalf("stages=%d want 1 (same revision)", len(stages))
	}
}

func TestRetryBackoff(t *testing.T) {
	tests := []struct {
		fails int
		want  time.Duration
	}{
		{-1, 0},
		{0, 0},
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 32 * time.Second},
		{7, 60 * time.Second}, // 64s clamped to the cap
		{20, 60 * time.Second},
	}
	for _, tt := range tests {
		if got := retryBackoff(tt.fails); got != tt.want {
			t.Errorf("retryBackoff(%d) = %v, want %v", tt.fails, got, tt.want)
		}
	}
}

func TestReporterFailureBackoff(t *testing.T) {
	var (
		hits    int32
		statusC int32 = http.StatusInternalServerError
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if atomic.LoadInt32(&statusC) == http.StatusOK {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ep := &inapi.HostletStatusEndpoint{Url: srv.URL, SecretKey: "k"}
	resetState()
	SetBoot()

	// First failure schedules a 1s backoff window.
	Flush(ep, "myapp", 0)
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("hits=%d want 1", hits)
	}

	// Still dirty, but inside the backoff window: the retry is suppressed.
	Flush(ep, "myapp", 0)
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("hits=%d want 1 (backoff suppresses immediate retry)", hits)
	}

	// Expiring the window lets the next failure double the wait.
	expireBackoff()
	Flush(ep, "myapp", 0)
	if atomic.LoadInt32(&hits) != 2 {
		t.Fatalf("hits=%d want 2 after window expiry", hits)
	}
	mu.Lock()
	curFails, wait := fails, time.Until(retryAt)
	mu.Unlock()
	if curFails != 2 || wait <= 0 || wait > 2*time.Second {
		t.Fatalf("fails=%d wait=%v, want fails=2 wait in (0, 2s]", curFails, wait)
	}

	// A long failure streak clamps the wait at the 60s cap.
	mu.Lock()
	fails = 9
	retryAt = time.Now().Add(-time.Second)
	mu.Unlock()
	Flush(ep, "myapp", 0)
	mu.Lock()
	wait = time.Until(retryAt)
	mu.Unlock()
	if wait <= 59*time.Second || wait > 61*time.Second {
		t.Fatalf("wait=%v, want ~60s cap", wait)
	}

	// Success clears the backoff so the next dirty flush is immediate.
	atomic.StoreInt32(&statusC, http.StatusOK)
	expireBackoff()
	Flush(ep, "myapp", 0)
	if atomic.LoadInt32(&hits) != 4 {
		t.Fatalf("hits=%d want 4", hits)
	}
	SetTaskRun(inapi.AppStageStateSuccess, "done")
	Flush(ep, "myapp", 0)
	if atomic.LoadInt32(&hits) != 5 {
		t.Fatalf("hits=%d want 5 (no wait after success reset)", hits)
	}
}

// resetState clears reporter state between tests.
func resetState() {
	mu.Lock()
	defer mu.Unlock()
	stages = nil
	dirty = false
	lastFlush = time.Time{}
	fails = 0
	retryAt = time.Time{}
	revision = 0
}

// expireBackoff moves the retry deadline into the past so the next Flush
// attempts a post immediately.
func expireBackoff() {
	mu.Lock()
	defer mu.Unlock()
	retryAt = time.Now().Add(-time.Second)
}
