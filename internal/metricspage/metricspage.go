// Copyright 2026 Eryx <evoruri at gmail dot com>, All rights reserved.
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

// Package metricspage serves the optional /metrics Prometheus endpoints.
// One shared HTTP route composes the payloads of
// every role active in the process: each role registers a Section that
// renders verbatim Prometheus text on scrape. Sections computing gauges
// from business memory caches use a FamilyWriter and deliberately bypass
// the inmetrics global registry (single-writer-per-metric rule), so
// entities gone from a cache disappear from the output and no cross-role
// name collision can happen in the shared process.
package metricspage

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	"github.com/hooto/httpsrv/v2"

	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inmetrics"
)

// Section contributes one role's metric payload to an endpoint. Render
// writes verbatim Prometheus text; gauge-only sections build a
// FamilyWriter, fill it and Flush it.
type Section struct {
	// Name labels the section in the payload comment header.
	Name string
	// Render emits the section's Prometheus text.
	Render func(w io.Writer)
}

var (
	mu sync.RWMutex

	enabled bool
	token   string

	mainSections    []Section
	replicaSections []Section
)

// Setup applies the [metrics] config. The server wires the routes only
// when the export is explicitly enabled; Setup(false) additionally keeps
// any mounted endpoint answering 404 (defense in depth for test harnesses
// and alternative wiring). A non-empty token requires Bearer
// authentication.
func Setup(on bool, authToken string) {
	mu.Lock()
	defer mu.Unlock()
	enabled = on
	token = authToken
}

// Register contributes a section to GET /metrics.
func Register(s Section) {
	addSection(&mainSections, s)
}

// RegisterReplicas contributes a section to GET /metrics/replicas (the
// opt-in zone-wide replica endpoint).
func RegisterReplicas(s Section) {
	addSection(&replicaSections, s)
}

func addSection(dst *[]Section, s Section) {
	if s.Name == "" || s.Render == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	*dst = append(*dst, s)
}

// RegisterRoutes mounts both endpoints on the shared HTTP router. Call once
// at startup; sections may register afterwards.
func RegisterRoutes(r httpsrv.Router) {
	r.Get("/metrics", http.HandlerFunc(serveMain))
	r.Get("/metrics/replicas", http.HandlerFunc(serveReplicas))
}

func serveMain(w http.ResponseWriter, req *http.Request) {
	serve(w, req, false)
}

func serveReplicas(w http.ResponseWriter, req *http.Request) {
	serve(w, req, true)
}

func serve(w http.ResponseWriter, req *http.Request, replicas bool) {

	mu.RLock()
	on, tok := enabled, token
	var sections []Section
	if replicas {
		sections = replicaSections
	} else {
		sections = mainSections
	}
	mu.RUnlock()

	if !on {
		http.NotFound(w, req)
		return
	}

	if tok != "" &&
		req.Header.Get("Authorization") != "Bearer "+tok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if len(sections) == 0 {
		http.Error(w, "no metrics sections registered", http.StatusNotFound)
		return
	}

	for _, s := range sections {
		fmt.Fprintf(w, "# section: %s\n", s.Name)
		s.Render(w)
	}
}

// FamilyWriter buffers Prometheus text samples per metric family, so a
// renderer may emit families in any interleaving order; Flush prints each
// family's HELP/TYPE header exactly once before its samples. Label pairs
// follow the inmetrics (name, item) convention, including its escaping.
type FamilyWriter struct {
	fams    []*family
	famsIdx map[string]*family
}

type family struct {
	metric string
	help   string
	prefix string // metric + "{" cached once per family
	lines  []string
}

// Gauge buffers one gauge sample.
func (fw *FamilyWriter) Gauge(metric, help, name, item string, v float64) {
	f := fw.famsIdx[metric]
	if f == nil {
		f = &family{
			metric: metric,
			help:   help,
			prefix: metric + "{",
		}
		if fw.famsIdx == nil {
			fw.famsIdx = map[string]*family{}
		}
		fw.famsIdx[metric] = f
		fw.fams = append(fw.fams, f)
	}
	f.lines = append(f.lines, f.prefix+
		"name=\""+inmetrics.EscapeLabel(name)+"\""+
		",item=\""+inmetrics.EscapeLabel(item)+"\"} "+
		strconv.FormatFloat(v, 'f', -1, 64))
}

// Flush writes the buffered families to w.
func (fw *FamilyWriter) Flush(w io.Writer) {
	for _, f := range fw.fams {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", f.metric, f.help, f.metric)
		for _, line := range f.lines {
			fmt.Fprintf(w, "%s\n", line)
		}
	}
}

// WriteReplicaGauges emits the per-replica usage gauges shared by the
// hostlet section (local containers) and the zonelet replicas section
// (zone-wide view): one scraper-facing metric contract, one writer. The
// derivations mirror the host-level window conventions (cpu-ms / 60 =
// millicores, 1-minute deltas / 60 = bytes per second).
func WriteReplicaGauges(fw *FamilyWriter, name string, m *inapi.NodeMetrics) {
	if m == nil || name == "" {
		return
	}
	fw.Gauge("innerstack_replica_cpu_millicores",
		"Replica CPU usage in millicores", name, "used",
		float64((m.CpuUser+m.CpuSys)/inutil.Window1Min))
	fw.Gauge("innerstack_replica_memory_bytes",
		"Replica memory usage in bytes", name, "used", float64(m.MemUsed))
	fw.Gauge("innerstack_replica_io_bytes_per_sec",
		"Replica IO bytes per second (1min window)", name, "net_recv",
		float64(m.NetRecvBytes/inutil.Window1Min))
	fw.Gauge("innerstack_replica_io_bytes_per_sec",
		"Replica IO bytes per second (1min window)", name, "net_sent",
		float64(m.NetSentBytes/inutil.Window1Min))
	fw.Gauge("innerstack_replica_io_bytes_per_sec",
		"Replica IO bytes per second (1min window)", name, "disk_read",
		float64(m.DiskReadBytes/inutil.Window1Min))
	fw.Gauge("innerstack_replica_io_bytes_per_sec",
		"Replica IO bytes per second (1min window)", name, "disk_write",
		float64(m.DiskWriteBytes/inutil.Window1Min))
}
