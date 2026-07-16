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
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func newReplicaMetricsCache() *replicaMetricsCache {
	return &replicaMetricsCache{items: map[string]*inapi.NodeMetrics{}}
}

func TestReplicaMetricsCacheStoreLoad(t *testing.T) {
	c := newReplicaMetricsCache()

	if _, ok := c.load("missing"); ok {
		t.Fatal("load should miss on an empty cache")
	}

	c.store("i8k_app1_1", &inapi.NodeMetrics{MemUsed: 123, Updated: 100})

	m, ok := c.load("i8k_app1_1")
	if !ok {
		t.Fatal("load should hit after store")
	}
	if m.MemUsed != 123 {
		t.Fatalf("MemUsed = %d, want 123", m.MemUsed)
	}

	// store overwrites the prior value.
	c.store("i8k_app1_1", &inapi.NodeMetrics{MemUsed: 999, Updated: 100})
	m, _ = c.load("i8k_app1_1")
	if m.MemUsed != 999 {
		t.Fatalf("MemUsed = %d, want 999 after overwrite", m.MemUsed)
	}

	// store rejects nil/empty-key no-ops without panicking.
	c.store("", &inapi.NodeMetrics{})
	c.store("i8k_app1_2", nil)
	if _, ok := c.load(""); ok {
		t.Error("empty-key store should be ignored")
	}
	if _, ok := c.load("i8k_app1_2"); ok {
		t.Error("nil-value store should be ignored")
	}
}

func TestReplicaMetricsCacheSweep(t *testing.T) {
	c := newReplicaMetricsCache()
	c.store("fresh", &inapi.NodeMetrics{Updated: 1000})
	c.store("stale", &inapi.NodeMetrics{Updated: 100})

	const maxAge int64 = 120
	c.sweep(1000+maxAge, maxAge) // fresh within window, stale outside

	if _, ok := c.load("fresh"); !ok {
		t.Error("fresh entry should survive sweep")
	}
	if _, ok := c.load("stale"); ok {
		t.Error("stale entry should be swept")
	}
}

func TestReplicaMetricsCacheMergeInto(t *testing.T) {
	c := newReplicaMetricsCache()
	c.store(inapi.ContainerName("app1", 1), &inapi.NodeMetrics{
		CpuUser: 6000, CpuSys: 0, MemUsed: 1 << 20, Uptime: 3600, Updated: 1,
	})
	// replica 2 has no reported metrics.

	inst := &inapi.AppInstance{
		Meta: &inapi.Metadata{Name: "app1"},
		Deploy: &inapi.AppDeploy{
			Replicas: []*inapi.AppDeployReplica{
				{Id: 1, HostId: "h1"},
				{Id: 2, HostId: "h2"},
			},
		},
	}

	c.mergeInto(inst)

	if inst.Status == nil {
		t.Fatal("Status should be populated")
	}
	if len(inst.Status.Replicas) != 2 {
		t.Fatalf("expected 2 replica statuses, got %d", len(inst.Status.Replicas))
	}

	byID := map[uint32]*inapi.AppReplicaStatus{}
	for _, r := range inst.Status.Replicas {
		byID[r.Id] = r
	}
	if byID[1].Metrics == nil {
		t.Errorf("replica 1 should carry metrics, got nil")
	}
	if byID[2].Metrics != nil {
		t.Errorf("replica 2 should have no metrics, got %+v", byID[2].Metrics)
	}
	if byID[1].HostId != "h1" {
		t.Errorf("replica 1 HostId = %q, want h1", byID[1].HostId)
	}
}

func TestReplicaMetricsCacheMergeIntoNils(t *testing.T) {
	c := newReplicaMetricsCache()

	// nil instance / no replicas: no panic, no change.
	c.mergeInto(nil)

	inst := &inapi.AppInstance{Meta: &inapi.Metadata{Name: "x"}}
	c.mergeInto(inst)
	if inst.Status != nil {
		t.Errorf("instance without replicas should not get a Status, got %+v", inst.Status)
	}
}
