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
	"io"
	"time"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/metricspage"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// RegisterMetricsExport contributes the leader's zone-wide sections: zone
// aggregates in GET /metrics and the opt-in zone replica view in
// GET /metrics/replicas. The leader renders only aggregates and
// cross-host derivations (report age): per-host detail belongs to each
// host's hostlet endpoint (one writer per metric family), and host history
// is served by the monstore. Non-leader terms render empty sections;
// innerstack_role (process section) tells dashboards which node to read.
func RegisterMetricsExport() {
	metricspage.Register(metricspage.Section{
		Name:   "zonelet",
		Render: renderZoneletMetrics,
	})
	metricspage.RegisterReplicas(metricspage.Section{
		Name:   "zonelet-replicas",
		Render: renderZoneReplicaMetrics,
	})
}

func renderZoneletMetrics(w io.Writer) {

	fw := &metricspage.FamilyWriter{}
	defer fw.Flush(w)

	if !status.IsZoneletLeader() {
		return
	}
	zone := config.Config.Zonelet.ZoneName
	if zone == "" {
		return
	}

	now := time.Now().Unix()
	freshLimit := config.Config.Alerts.HostDownSec()

	// Host freshness aggregate and per-host report age. The threshold is
	// the same constant the host_down alert rule uses.
	var total, fresh, stale int
	status.Zonelet_HostStatusSet.Range(func(key, value any) bool {
		hs, ok := value.(*inapi.HostStatus)
		if !ok || hs.HostId == "" {
			return true
		}
		total++
		age := now - hs.Updated
		if age > freshLimit {
			stale++
		} else {
			fresh++
		}
		fw.Gauge("innerstack_host_report_age_seconds",
			"Seconds since the host's last status report",
			hs.HostId, "age", float64(age))
		return true
	})
	fw.Gauge("innerstack_zone_hosts", "Zone hosts by report freshness",
		zone, "total", float64(total))
	fw.Gauge("innerstack_zone_hosts", "Zone hosts by report freshness",
		zone, "fresh", float64(fresh))
	fw.Gauge("innerstack_zone_hosts", "Zone hosts by report freshness",
		zone, "stale", float64(stale))

	// App instance and replica aggregates over the app cache. Soft-deleted
	// instances count only in the deleted aggregate; their replicas are
	// being torn down and do not join the state counts.
	var instances, deleted int
	replicaStates := map[string]int{}
	gAppInstanceSet.Iter(func(app *appInstanceEntry) bool {
		if app == nil || app.Value == nil || app.Value.Deploy == nil {
			return true
		}
		if app.Value.Deploy.Action == inapi.OpActionDelete {
			deleted++
			return true
		}
		instances++
		for _, rep := range app.Value.Deploy.Replicas {
			if rep == nil {
				continue
			}
			state := rep.State
			if state == "" {
				state = "placed"
			}
			replicaStates[state]++
		}
		return true
	})
	fw.Gauge("innerstack_app_instances", "App instances in the zone",
		zone, "total", float64(instances))
	fw.Gauge("innerstack_app_instances", "App instances in the zone",
		zone, "deleted", float64(deleted))
	for state, n := range replicaStates {
		fw.Gauge("innerstack_app_replicas",
			"App replicas in the zone by desired state", zone, state, float64(n))
	}
}

// renderZoneReplicaMetrics renders the zone-wide replica usage view from
// the report cache; entries sweep away with their containers. The cache is
// snapshotted under its read lock and rendered outside it so reports keep
// flowing during a scrape.
func renderZoneReplicaMetrics(w io.Writer) {

	if !status.IsZoneletLeader() {
		return
	}

	gReplicaMetrics.mu.RLock()
	items := make([]*inapi.NodeMetrics, 0, len(gReplicaMetrics.items))
	names := make([]string, 0, len(gReplicaMetrics.items))
	for name, m := range gReplicaMetrics.items {
		if m == nil || name == "" {
			continue
		}
		names = append(names, name)
		items = append(items, m)
	}
	gReplicaMetrics.mu.RUnlock()

	fw := &metricspage.FamilyWriter{}
	defer fw.Flush(w)

	for i, m := range items {
		metricspage.WriteReplicaGauges(fw, names[i], m)
	}
}
