// Copyright 2015 Eryx <evoribu at gmail dot com>, All rights reserved.
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
	"sync"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// replicaMetricsMaxAge is how long a per-replica metrics entry remains valid
// without a refresh from its hostlet. The hostlet reports every few seconds,
// so an entry older than this is considered stale (host down, container gone,
// or replica rescheduled away) and is swept.
const replicaMetricsMaxAge int64 = 120

// gReplicaMetrics is the zone leader's in-memory cache of the most recent
// per-replica runtime metrics reported by hostlets via HostStatusUpdate. It is
// keyed by container name (i8k_<instance>_<repId>) and is deliberately never
// persisted: runtime metrics are ephemeral, so the AppInstance read path
// (AppInstanceList/AppInstanceInfo) merges this cache into a transient copy of
// each instance rather than flushing it to kvgo.
var gReplicaMetrics = &replicaMetricsCache{
	items: map[string]*inapi.NodeMetrics{},
}

type replicaMetricsCache struct {
	mu    sync.RWMutex
	items map[string]*inapi.NodeMetrics
}

// store records the latest metrics snapshot for a container, overwriting any
// prior entry.
func (c *replicaMetricsCache) store(name string, m *inapi.NodeMetrics) {
	if c == nil || name == "" || m == nil {
		return
	}
	c.mu.Lock()
	c.items[name] = m
	c.mu.Unlock()
}

// load returns the metrics snapshot for a container, or (nil, false).
func (c *replicaMetricsCache) load(name string) (*inapi.NodeMetrics, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.items[name]
	return m, ok
}

// sweep removes entries whose Updated timestamp is older than maxAge seconds.
// now is injected so callers (and tests) control the clock.
func (c *replicaMetricsCache) sweep(now, maxAge int64) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, m := range c.items {
		if m == nil || now-m.Updated > maxAge {
			delete(c.items, k)
		}
	}
}

// mergeInto populates inst.Status.Replicas with one entry per deployed
// replica, attaching the freshest cached metrics for each. It operates on a
// transient instance (already decoded from kvgo or cloned), so the persisted
// instance is never touched -- runtime metrics stay in-memory only. Instances
// with no placed replicas are left unchanged.
func (c *replicaMetricsCache) mergeInto(inst *inapi.AppInstance) {
	if c == nil || inst == nil || inst.Deploy == nil || len(inst.Deploy.Replicas) == 0 {
		return
	}
	name := inst.InstanceName()
	if name == "" {
		return
	}

	reps := make([]*inapi.AppReplicaStatus, 0, len(inst.Deploy.Replicas))
	for _, rep := range inst.Deploy.Replicas {
		if rep == nil {
			continue
		}
		st := &inapi.AppReplicaStatus{
			Id:     rep.Id,
			HostId: rep.HostId,
		}
		if m, ok := c.load(inapi.ContainerName(name, rep.Id)); ok {
			st.Metrics = m
		}
		reps = append(reps, st)
	}

	if inst.Status == nil {
		inst.Status = &inapi.AppStatus{}
	}
	inst.Status.Replicas = reps
}
