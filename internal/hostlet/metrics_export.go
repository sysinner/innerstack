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
	"io"
	"maps"
	"slices"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hostapi"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/internal/metricspage"
)

// RegisterMetricsExport contributes the hostlet section to GET /metrics:
// host-level gauges (same derivations as the monstore fields), container
// driver/state counts and per-replica usage. Everything is
// computed from local in-memory caches at scrape time; nothing registers in
// the inmetrics global registry.
func RegisterMetricsExport() {
	metricspage.Register(metricspage.Section{
		Name:   "hostlet",
		Render: renderHostletMetrics,
	})
}

// hostGaugeSnapshot carries the scalar gauges read under one RLock, so the
// scrape path does not deep-clone the whole HostStatus proto.
type hostGaugeSnapshot struct {
	cpuCores, cpuSys, cpuUser       int64
	memTotal, memUsed, memAvailable int64
	diskTotal, diskFree             int64
	diskRead, diskWrite             int64
	netRecv, netSent                int64
	uptime                          int64
}

func (it *hostStatusMut) gaugeSnapshot() hostGaugeSnapshot {
	it.mu.RLock()
	defer it.mu.RUnlock()
	return hostGaugeSnapshot{
		cpuCores: int64(it.CpuCores), cpuSys: it.CpuSys, cpuUser: it.CpuUser,
		memTotal: it.MemTotal, memUsed: it.MemUsed, memAvailable: it.MemAvailable,
		diskTotal: it.DiskTotalBytes, diskFree: it.DiskFreeBytes,
		diskRead: it.DiskReadBytes, diskWrite: it.DiskWriteBytes,
		netRecv: it.NetRecvBytes, netSent: it.NetSentBytes,
		uptime: it.Uptime,
	}
}

func renderHostletMetrics(w io.Writer) {

	hostId := config.Config.Hostlet.HostId
	if hostId == "" {
		return
	}
	hs := hostStatus.gaugeSnapshot()

	fw := &metricspage.FamilyWriter{}
	defer fw.Flush(w)

	// Host gauges: windowed cpu-ms / 60 = millicores, 1-minute deltas / 60
	// = bytes per second (same formulas as the monstore sampling and the
	// zonelet scheduler).
	fw.Gauge("innerstack_host_cpu_millicores",
		"Host CPU usage in millicores", hostId, "used",
		float64((hs.cpuSys+hs.cpuUser)/inutil.Window1Min))
	fw.Gauge("innerstack_host_cpu_millicores",
		"Host CPU usage in millicores", hostId, "total",
		float64(hs.cpuCores)*1000)

	fw.Gauge("innerstack_host_memory_bytes",
		"Host memory in bytes", hostId, "total", float64(hs.memTotal))
	fw.Gauge("innerstack_host_memory_bytes",
		"Host memory in bytes", hostId, "used", float64(hs.memUsed))
	fw.Gauge("innerstack_host_memory_bytes",
		"Host memory in bytes", hostId, "available", float64(hs.memAvailable))

	fw.Gauge("innerstack_host_disk_bytes",
		"Host data disk in bytes", hostId, "total", float64(hs.diskTotal))
	fw.Gauge("innerstack_host_disk_bytes",
		"Host data disk in bytes", hostId, "free", float64(hs.diskFree))

	fw.Gauge("innerstack_host_disk_io_bytes_per_sec",
		"Host disk IO bytes per second (1min window)", hostId, "read",
		float64(hs.diskRead/inutil.Window1Min))
	fw.Gauge("innerstack_host_disk_io_bytes_per_sec",
		"Host disk IO bytes per second (1min window)", hostId, "write",
		float64(hs.diskWrite/inutil.Window1Min))

	fw.Gauge("innerstack_host_net_bytes_per_sec",
		"Host network bytes per second (1min window)", hostId, "recv",
		float64(hs.netRecv/inutil.Window1Min))
	fw.Gauge("innerstack_host_net_bytes_per_sec",
		"Host network bytes per second (1min window)", hostId, "sent",
		float64(hs.netSent/inutil.Window1Min))

	fw.Gauge("innerstack_host_uptime_seconds",
		"Host uptime in seconds", hostId, "uptime", float64(hs.uptime))

	// Container driver inventory.
	for driver, ctr := range hostStatus.ContainersSnapshot() {
		fw.Gauge("innerstack_host_containers",
			"Containers per driver", hostId, driver, float64(ctr))
	}

	// Container states by driver.
	if driver := containerDriverName(); driver != "" {
		states := map[string]int{}
		hoststatus.ContainerList.Range(func(key, value any) bool {
			if info, ok := value.(*hostapi.ContainerInfo); ok {
				states[info.State]++
			}
			return true
		})
		for _, state := range slices.Sorted(maps.Keys(states)) {
			fw.Gauge("innerstack_container_states",
				"Local container count by state", driver, state,
				float64(states[state]))
		}
	}

	// Per-replica usage (10s sampling, 1-minute sliding window).
	hoststatus.ReplicaStatsSet.Range(func(key, value any) bool {
		name, ok := key.(string)
		if !ok {
			return true
		}
		if entry, ok := value.(*hoststatus.ReplicaStatsEntry); ok {
			metricspage.WriteReplicaGauges(fw, name, entry.Metrics())
		}
		return true
	})
}

// containerDriverName returns the active container driver name for label
// purposes, or "" before the driver initialized.
func containerDriverName() string {
	if ctrDriver == nil {
		return ""
	}
	return ctrDriver.Name()
}
