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

// Package metrics holds the process-owned metrics of the control plane:
// gRPC call counters and latency from the interceptor, Go runtime gauges
// refreshed on a background beat, the zonelet role gauge and the audit
// counters. They live in the inmetrics global registry (single writer per
// metric, updated at instrumentation or refresh points) and reach /metrics
// through the process section, which dumps the registry on scrape.
package metrics

import (
	"io"

	"github.com/sysinner/innerstack/v2/internal/audit"
	"github.com/sysinner/innerstack/v2/internal/metricspage"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inmetrics"
)

var (
	rpcTotal = inmetrics.RegisterCounterMap(
		"innerstack_rpc_total",
		"gRPC unary calls by method and result")

	rpcLatency = inmetrics.RegisterHistogramMap(
		"innerstack_rpc_latency_seconds",
		"gRPC unary latency in seconds",
		inmetrics.NewBuckets(0.0005, 1.5, 24))

	goGoroutines = inmetrics.RegisterGaugeMap(
		"innerstack_go_goroutines", "Number of goroutines")

	goHeapBytes = inmetrics.RegisterGaugeMap(
		"innerstack_go_heap_bytes", "Go heap allocation in bytes")

	goGCCycles = inmetrics.RegisterCounterMap(
		"innerstack_go_gc_cycles_total", "Completed GC cycles")

	auditRecords = inmetrics.RegisterCounterMap(
		"innerstack_audit_records_total", "Audit records written or dropped")

	roleGauge = inmetrics.RegisterGaugeMap(
		"innerstack_role", "Zonelet role of this process (1 = active)")
)

// RegisterProcessSection contributes the process section to GET /metrics:
// role and audit values are synced here (render-time single writer, same
// source as audit.Manager.Stats) and the whole inmetrics registry is
// dumped, so every process-owned counter appears with its true type.
func RegisterProcessSection() {
	metricspage.Register(metricspage.Section{
		Name: "process",
		Render: func(w io.Writer) {
			renderProcess()
			inmetrics.WriteTo(w)
		},
	})
}

func renderProcess() {
	if status.IsZoneletLeader() {
		roleGauge.Set("zonelet", "leader", 1)
		roleGauge.Set("zonelet", "standby", 0)
	} else {
		roleGauge.Set("zonelet", "leader", 0)
		roleGauge.Set("zonelet", "standby", 1)
	}

	if audit.Mgr != nil {
		written, dropped := audit.Mgr.Stats()
		auditRecords.Set("audit", "written", float64(written))
		auditRecords.Set("audit", "dropped", float64(dropped))
	}
}
