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

package zonelet

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"

	"github.com/sysinner/innerstack/v2/internal/config"
	zstatus "github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func renderToBody(t *testing.T, render func(io.Writer)) string {
	t.Helper()
	var buf bytes.Buffer
	render(&buf)
	return buf.String()
}

func TestRenderZoneletMetricsLeader(t *testing.T) {
	setupAlertsTest(t) // wires zone, leader state and cleanup

	now := time.Now().Unix()

	// One fresh host, one stale host.
	zstatus.Zonelet_HostStatusSet.Store("h-fresh", &inapi.HostStatus{
		HostId: "h-fresh", Updated: now - 5})
	zstatus.Zonelet_HostStatusSet.Store("h-stale", &inapi.HostStatus{
		HostId: "h-stale", Updated: now - 100})

	// One live instance with 2 placed replicas, one soft-deleted instance.
	gAppInstanceSet.Store(&inapi.AppInstance{
		Meta: &inapi.Metadata{Name: "myapp"},
		Spec: &inapi.AppSpec{Resources: &inapi.AppSpecResources{}},
		Deploy: &inapi.AppDeploy{Replicas: []*inapi.AppDeployReplica{
			{Id: 0, HostId: "h-fresh"},
			{Id: 1, HostId: "h-stale", State: "stop"},
		}},
	}, &kvapi.Meta{Version: 1})
	gAppInstanceSet.Store(&inapi.AppInstance{
		Meta: &inapi.Metadata{Name: "gone"},
		Spec: &inapi.AppSpec{Resources: &inapi.AppSpecResources{}},
		Deploy: &inapi.AppDeploy{
			Action:   inapi.OpActionDelete,
			Replicas: []*inapi.AppDeployReplica{{Id: 0, HostId: "h-fresh"}},
		},
	}, &kvapi.Meta{Version: 1})

	// One replica metrics entry.
	gReplicaMetrics.store("i8k_myapp_0", &inapi.NodeMetrics{
		CpuUser: 3000, CpuSys: 3000, // -> 100 millicores
		MemUsed:      1 << 20,
		NetRecvBytes: 600, // -> 10/s
	})

	body := renderToBody(t, renderZoneletMetrics)
	want := []string{
		`innerstack_zone_hosts{` + nameLabel("alertstest") + `,item="total"} 2`,
		`innerstack_zone_hosts{` + nameLabel("alertstest") + `,item="fresh"} 1`,
		`innerstack_zone_hosts{` + nameLabel("alertstest") + `,item="stale"} 1`,
		`innerstack_app_instances{` + nameLabel("alertstest") + `,item="total"} 1`,
		`innerstack_app_instances{` + nameLabel("alertstest") + `,item="deleted"} 1`,
		`innerstack_app_replicas{` + nameLabel("alertstest") + `,item="placed"} 1`,
		`innerstack_app_replicas{` + nameLabel("alertstest") + `,item="stop"} 1`,
	}
	for _, s := range want {
		if !strings.Contains(body, s) {
			t.Fatalf("body missing %q:\n%s", s, body)
		}
	}
	if !strings.Contains(body,
		`innerstack_host_report_age_seconds{`+nameLabel("h-stale")+`,item="age"}`) {
		t.Fatalf("report age missing for stale host:\n%s", body)
	}

	// Zone replica view.
	rbody := renderToBody(t, renderZoneReplicaMetrics)
	wantReplica := []string{
		`innerstack_replica_cpu_millicores{` + nameLabel("i8k_myapp_0") +
			`,item="used"} 100`,
		`innerstack_replica_memory_bytes{` + nameLabel("i8k_myapp_0") +
			`,item="used"} 1048576`,
		`innerstack_replica_io_bytes_per_sec{` + nameLabel("i8k_myapp_0") +
			`,item="net_recv"} 10`,
	}
	for _, s := range wantReplica {
		if !strings.Contains(rbody, s) {
			t.Fatalf("replica body missing %q:\n%s", s, rbody)
		}
	}
}

func TestRenderZoneletMetricsStandby(t *testing.T) {
	setupAlertsTest(t)

	zstatus.ZoneletLeader = "h-someone-else"
	zstatus.Zonelet_HostStatusSet.Store("h-fresh",
		&inapi.HostStatus{HostId: "h-fresh", Updated: time.Now().Unix()})

	if body := renderToBody(t, renderZoneletMetrics); body != "" {
		t.Fatalf("standby section should render empty, got:\n%s", body)
	}
	if body := renderToBody(t, renderZoneReplicaMetrics); body != "" {
		t.Fatalf("standby replicas section should render empty, got:\n%s", body)
	}
}

// nameLabel renders the name label; inmetrics sorts labels as (item, name)
// but metricspage emits (name, item), so tests target metricspage output.
func nameLabel(v string) string {
	return `name="` + v + `"`
}

// BenchmarkZoneletMetricsExport renders the leader aggregates at the
// documented ceiling (200 hosts, 10k instances) to guard the scrape-time
// render cost (design target < 5ms).
func BenchmarkZoneletMetricsExport(b *testing.B) {
	prevZone := config.Config.Zonelet.ZoneName
	prevHost := config.Config.Hostlet.HostId
	config.Config.Zonelet.ZoneName = "benchzone"
	config.Config.Hostlet.HostId = "h-bench"
	zstatus.ZoneletLeader = "h-bench"
	zstatus.ZoneletLeaderUpdated = time.Now().UnixMilli()
	b.Cleanup(func() {
		config.Config.Zonelet.ZoneName = prevZone
		config.Config.Hostlet.HostId = prevHost
		gAppInstanceSet.Clear()
	})

	now := time.Now().Unix()
	for i := 0; i < 200; i++ {
		host := fmt.Sprintf("h-bench-%03d", i)
		zstatus.Zonelet_HostStatusSet.Store(host, &inapi.HostStatus{
			HostId: host, Updated: now - int64(i%60)})
	}
	for i := 0; i < 10000; i++ {
		gAppInstanceSet.Store(&inapi.AppInstance{
			Meta: &inapi.Metadata{Name: fmt.Sprintf("app-%05d", i)},
			Spec: &inapi.AppSpec{Resources: &inapi.AppSpecResources{}},
			Deploy: &inapi.AppDeploy{Replicas: []*inapi.AppDeployReplica{
				{Id: 0, HostId: fmt.Sprintf("h-bench-%03d", i%200)},
			}},
		}, &kvapi.Meta{Version: 1})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderZoneletMetrics(io.Discard)
	}
}
