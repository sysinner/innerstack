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

package hostlet

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hostapi"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func TestRenderHostletMetrics(t *testing.T) {
	prevHost := config.Config.Hostlet.HostId
	config.Config.Hostlet.HostId = "h-exp-1"
	t.Cleanup(func() { config.Config.Hostlet.HostId = prevHost })

	// Host status: cpu 30000ms/1min -> 500 millicores, net 6000B -> 100/s.
	hostStatus.lock(func() {
		hostStatus.CpuCores = 4
		hostStatus.CpuSys = 12000
		hostStatus.CpuUser = 18000
		hostStatus.MemTotal = 8 << 30
		hostStatus.MemUsed = 2 << 30
		hostStatus.MemAvailable = 6 << 30
		hostStatus.NetRecvBytes = 6000
		hostStatus.NetSentBytes = 1200
		hostStatus.Uptime = 1000
	})

	// Container states: skip the driver-gated family (no driver in tests);
	// replica metrics from the stats cache.
	hoststatus.ContainerList.Store("i8k_myapp_0",
		&hostapi.ContainerInfo{Name: "i8k_myapp_0", State: inapi.OpStateRunning})
	t.Cleanup(func() { hoststatus.ContainerList.Delete("i8k_myapp_0") })

	entry := hoststatus.ReplicaStats("i8k_myapp_0")
	entry.SetMetrics(&inapi.NodeMetrics{
		CpuUser: 1500, CpuSys: 1500, // 3000ms/60 -> 50 millicores
		MemUsed:      1 << 20,
		NetRecvBytes: 300, // /60 -> 5
	})
	t.Cleanup(func() { hoststatus.ReplicaStatsSet.Delete("i8k_myapp_0") })

	var buf bytes.Buffer
	renderHostletMetrics(&buf)
	body := buf.String()

	want := []string{
		"innerstack_host_cpu_millicores{name=\"h-exp-1\",item=\"used\"} 500\n",
		"innerstack_host_cpu_millicores{name=\"h-exp-1\",item=\"total\"} 4000\n",
		"innerstack_host_memory_bytes{name=\"h-exp-1\",item=\"used\"} 2147483648\n",
		"innerstack_host_net_bytes_per_sec{name=\"h-exp-1\",item=\"recv\"} 100\n",
		"innerstack_host_net_bytes_per_sec{name=\"h-exp-1\",item=\"sent\"} 20\n",
		"innerstack_host_uptime_seconds{name=\"h-exp-1\",item=\"uptime\"} 1000\n",
		"innerstack_replica_cpu_millicores{name=\"i8k_myapp_0\",item=\"used\"} 50\n",
		"innerstack_replica_memory_bytes{name=\"i8k_myapp_0\",item=\"used\"} 1048576\n",
		"innerstack_replica_io_bytes_per_sec{name=\"i8k_myapp_0\",item=\"net_recv\"} 5\n",
	}
	for _, s := range want {
		if !strings.Contains(body, s) {
			t.Fatalf("body missing %q:\n%s", s, body)
		}
	}

	// Each family header appears exactly once despite interleaved emission.
	for _, m := range []string{
		"innerstack_host_cpu_millicores",
		"innerstack_replica_cpu_millicores",
	} {
		if n := strings.Count(body, "# HELP "+m+" "); n != 1 {
			t.Fatalf("%s header emitted %d times", m, n)
		}
	}
}
