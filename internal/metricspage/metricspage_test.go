// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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

package metricspage

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeDisabledAndAuth(t *testing.T) {
	// Disabled: 404 even with sections registered.
	mu.Lock()
	mainSections = []Section{{Name: "x", Render: func(w io.Writer) {}}}
	replicaSections = nil
	mu.Unlock()
	Setup(false, "")

	rec := httptest.NewRecorder()
	serveMain(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled endpoint code = %d", rec.Code)
	}

	// Enabled without token: 200.
	Setup(true, "")
	rec = httptest.NewRecorder()
	serveMain(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled endpoint code = %d", rec.Code)
	}

	// Enabled with token: missing/wrong -> 401, right -> 200.
	Setup(true, "s3cret")
	rec = httptest.NewRecorder()
	serveMain(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token code = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	serveMain(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token code = %d", rec.Code)
	}

	req.Header.Set("Authorization", "Bearer s3cret")
	rec = httptest.NewRecorder()
	serveMain(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("right token code = %d", rec.Code)
	}

	// No sections registered for the replicas endpoint: 404.
	rec = httptest.NewRecorder()
	serveReplicas(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("empty replicas endpoint code = %d", rec.Code)
	}
}

func TestServeSectionComposition(t *testing.T) {
	roleA := Section{
		Name: "role-a",
		Render: func(w io.Writer) {
			// Interleaved families must still print one header each.
			fw := &FamilyWriter{}
			fw.Gauge("m1", "help one", "n1", "i1", 1)
			fw.Gauge("m2", "help two", "n2", "i2", 2.5)
			fw.Gauge("m1", "help one", "n1", "i1b", -3)
			fw.Flush(w)
		},
	}
	roleB := Section{
		Name: "role-b",
		Render: func(w io.Writer) {
			fw := &FamilyWriter{}
			fw.Gauge("m3", "help three", `we"ird\name`, "v", 0)
			fw.Flush(w)
		},
	}
	mu.Lock()
	mainSections = []Section{roleA, roleB}
	replicaSections = nil
	mu.Unlock()
	Setup(true, "")

	rec := httptest.NewRecorder()
	serveMain(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	body := rec.Body.String()
	want := []string{
		"# section: role-a\n",
		"# HELP m1 help one\n# TYPE m1 gauge\n",
		"m1{name=\"n1\",item=\"i1\"} 1\n",
		"m1{name=\"n1\",item=\"i1b\"} -3\n",
		"# HELP m2 help two\n# TYPE m2 gauge\n",
		"m2{name=\"n2\",item=\"i2\"} 2.5\n",
		"# section: role-b\n",
		"# HELP m3 help three\n# TYPE m3 gauge\n",
		"m3{name=\"we\\\"ird\\\\name\",item=\"v\"} 0\n",
	}
	for _, s := range want {
		if !strings.Contains(body, s) {
			t.Fatalf("body missing %q:\n%s", s, body)
		}
	}
	if strings.Count(body, "# HELP m1 ") != 1 {
		t.Fatalf("m1 header emitted %d times:\n%s",
			strings.Count(body, "# HELP m1 "), body)
	}
}
