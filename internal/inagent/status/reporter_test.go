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
	mu.Lock()
	stages = nil
	dirty = false
	lastFlush = zeroTime()
	mu.Unlock()

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
	mu.Lock()
	stages = nil
	dirty = false
	mu.Unlock()
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

	mu.Lock()
	stages = nil
	dirty = false
	lastFlush = zeroTime()
	mu.Unlock()

	SetSpecLoad(inapi.AppStageStateFailed, "bad")
	Flush(&inapi.HostletStatusEndpoint{Url: srv.URL, SecretKey: "k"}, "myapp", 0)

	// 401 -> dirty must remain so it retries next tick.
	mu.Lock()
	defer mu.Unlock()
	if !dirty {
		t.Fatal("dirty should remain true after non-200 response")
	}
}

// zeroTime returns a zero time so the first Flush always treats the heartbeat
// as elapsed.
func zeroTime() time.Time { return time.Time{} }

func TestReporterSetRevisionResets(t *testing.T) {
	mu.Lock()
	stages = nil
	dirty = false
	revision = 0
	mu.Unlock()

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
