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

package cli

import (
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func TestAppListStatus(t *testing.T) {
	cases := []struct {
		name   string
		deploy *inapi.AppDeploy
		want   string
	}{
		{"nil deploy", nil, "-"},
		{"empty deploy", &inapi.AppDeploy{}, "-"},
		{"action only", &inapi.AppDeploy{Action: "start"}, "start"},
		{"stage wins over action", &inapi.AppDeploy{
			Action: "start",
			Stages: &inapi.AppDeployStage{State: inapi.AppStageStateRunning},
		}, inapi.AppStageStateRunning},
		{"failed stage", &inapi.AppDeploy{
			Stages: &inapi.AppDeployStage{State: inapi.AppStageStateFailed},
		}, inapi.AppStageStateFailed},
		{"empty stage state falls back to action", &inapi.AppDeploy{
			Action:  "stop",
			Stages:  &inapi.AppDeployStage{},
		}, "stop"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := appListStatus(c.deploy); got != c.want {
				t.Fatalf("appListStatus() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAppListReplicas(t *testing.T) {
	// A replica counts as ready when Status.Replicas carries live metrics for
	// it (the hostlet reports metrics only for running containers).
	mk := func(cap uint32, reps ...*inapi.AppReplicaStatus) *inapi.AppInstance {
		inst := &inapi.AppInstance{Deploy: &inapi.AppDeploy{ReplicaCap: cap}}
		if len(reps) > 0 {
			inst.Status = &inapi.AppStatus{Replicas: reps}
		}
		return inst
	}
	metric := func(id uint32) *inapi.AppReplicaStatus {
		return &inapi.AppReplicaStatus{Id: id, Metrics: &inapi.NodeMetrics{}}
	}
	plain := func(id uint32) *inapi.AppReplicaStatus {
		return &inapi.AppReplicaStatus{Id: id}
	}

	cases := []struct {
		name string
		inst *inapi.AppInstance
		want string
	}{
		{"nil instance", nil, "-"},
		{"no deploy", &inapi.AppInstance{}, "-"},
		{"empty deploy, no status", mk(0), "0/0"},
		{"cap drives desired, no status", mk(3), "0/3"},
		{"replica without metrics is not ready", mk(1, plain(1)), "0/1"},
		{"single running replica", mk(1, metric(1)), "1/1"},
		{"one of two running", mk(2, metric(1), plain(2)), "1/2"},
		{"both running", mk(2, metric(1), metric(2)), "2/2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := appListReplicas(c.inst); got != c.want {
				t.Fatalf("appListReplicas() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAppListUptime(t *testing.T) {
	cases := []struct {
		name string
		inst *inapi.AppInstance
		want string
	}{
		{"nil", nil, "-"},
		{"no status", &inapi.AppInstance{}, "-"},
		{"replica without metrics", &inapi.AppInstance{
			Status: &inapi.AppStatus{Replicas: []*inapi.AppReplicaStatus{{Id: 1}}},
		}, "-"},
		{"single replica", &inapi.AppInstance{
			Status: &inapi.AppStatus{Replicas: []*inapi.AppReplicaStatus{
				{Id: 1, Metrics: &inapi.NodeMetrics{Uptime: 90000}}, // 1d 1h
			}},
		}, "1d 1h"},
		{"reports youngest of multiple replicas", &inapi.AppInstance{
			Status: &inapi.AppStatus{Replicas: []*inapi.AppReplicaStatus{
				{Id: 1, Metrics: &inapi.NodeMetrics{Uptime: 90000}}, // 1d 1h
				{Id: 2, Metrics: &inapi.NodeMetrics{Uptime: 3661}},  // 1h 1m (youngest)
			}},
		}, "1h 1m"},
		{"ignores zero uptime", &inapi.AppInstance{
			Status: &inapi.AppStatus{Replicas: []*inapi.AppReplicaStatus{
				{Id: 1, Metrics: &inapi.NodeMetrics{Uptime: 0}},
				{Id: 2, Metrics: &inapi.NodeMetrics{Uptime: 3661}},
			}},
		}, "1h 1m"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := appListUptime(c.inst); got != c.want {
				t.Fatalf("appListUptime() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAppAggregateUsage(t *testing.T) {
	// 12000ms + 6000ms of CPU over the window => 18000/60 = 300 millicores.
	cases := []struct {
		name      string
		inst      *inapi.AppInstance
		wantCpuMc int64
		wantMem   int64
		wantHas   bool
	}{
		{"nil", nil, 0, 0, false},
		{"no status", &inapi.AppInstance{}, 0, 0, false},
		{"replica without metrics", &inapi.AppInstance{
			Status: &inapi.AppStatus{Replicas: []*inapi.AppReplicaStatus{{Id: 1}}},
		}, 0, 0, false},
		{"sums across replicas", &inapi.AppInstance{
			Status: &inapi.AppStatus{Replicas: []*inapi.AppReplicaStatus{
				{Id: 1, Metrics: &inapi.NodeMetrics{
					CpuUser: 6000, CpuSys: 6000, MemUsed: 1 << 20}},
				{Id: 2, Metrics: &inapi.NodeMetrics{
					CpuUser: 3000, CpuSys: 3000, MemUsed: 2 << 20}},
			}},
		}, 300, 3 << 20, true},
		{"reported metrics but idle (zero usage)", &inapi.AppInstance{
			Status: &inapi.AppStatus{Replicas: []*inapi.AppReplicaStatus{
				{Id: 1, Metrics: &inapi.NodeMetrics{}}, // present but all zero
			}},
		}, 0, 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cpuMc, mem, has := appAggregateUsage(c.inst)
			if cpuMc != c.wantCpuMc {
				t.Errorf("cpuMc = %d, want %d", cpuMc, c.wantCpuMc)
			}
			if mem != c.wantMem {
				t.Errorf("mem = %d, want %d", mem, c.wantMem)
			}
			if has != c.wantHas {
				t.Errorf("has = %v, want %v", has, c.wantHas)
			}
		})
	}
}

func TestCpuUsageLimit(t *testing.T) {
	cases := []struct {
		used, limit int64
		ok          bool
		want        string
	}{
		{0, 0, false, "-"},      // no realtime data
		{0, 1000, false, "-"},   // no data, even with a limit
		{0, 1000, true, "0m/1"}, // measured idle, marks the 0
		{0, 0, true, "0m"},      // measured idle, no limit
		{500, 0, true, "500m"},  // usage only
		{500, 1000, true, "500m/1"},
	}
	for _, c := range cases {
		if got := cpuUsageLimit(c.used, c.limit, c.ok); got != c.want {
			t.Errorf("cpuUsageLimit(%d,%d,%v) = %q, want %q", c.used, c.limit, c.ok, got, c.want)
		}
	}
}

func TestBytesUsageLimit(t *testing.T) {
	cases := []struct {
		used, limit int64
		want        string
	}{
		{0, 0, "-"},
		{0, 1 << 20, "1 MiB"},
		{1 << 20, 0, "1 MiB"},
		{1 << 20, 2 << 20, "1 MiB/2 MiB"},
	}
	for _, c := range cases {
		if got := bytesUsageLimit(c.used, c.limit); got != c.want {
			t.Errorf("bytesUsageLimit(%d,%d) = %q, want %q", c.used, c.limit, got, c.want)
		}
	}
}

func TestAppAggregateNet(t *testing.T) {
	cases := []struct {
		name    string
		inst    *inapi.AppInstance
		wantRx  int64
		wantTx  int64
	}{
		{"nil", nil, 0, 0},
		{"no status", &inapi.AppInstance{}, 0, 0},
		{"sums across replicas", &inapi.AppInstance{
			Status: &inapi.AppStatus{Replicas: []*inapi.AppReplicaStatus{
				{Id: 1, Metrics: &inapi.NodeMetrics{NetRecvBytes: 1000, NetSentBytes: 2000}},
				{Id: 2, Metrics: &inapi.NodeMetrics{NetRecvBytes: 3000, NetSentBytes: 4000}},
			}},
		}, 4000, 6000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rx, tx := appAggregateNet(c.inst)
			if rx != c.wantRx {
				t.Errorf("rx = %d, want %d", rx, c.wantRx)
			}
			if tx != c.wantTx {
				t.Errorf("tx = %d, want %d", tx, c.wantTx)
			}
		})
	}
}

func TestNetRate(t *testing.T) {
	cases := []struct {
		rx, tx int64
		want   string
	}{
		{0, 0, "-"},
		{1 << 20, 0, "1 MiB/s, 0 B/s"},
		{1 << 20, 2 << 20, "1 MiB/s, 2 MiB/s"},
	}
	for _, c := range cases {
		if got := netRate(c.rx, c.tx); got != c.want {
			t.Errorf("netRate(%d,%d) = %q, want %q", c.rx, c.tx, got, c.want)
		}
	}
}

func TestSpecNameVersion(t *testing.T) {
	cases := []struct {
		name string
		spec *inapi.AppSpec
		want string
	}{
		{"nil", nil, "-"},
		{"empty", &inapi.AppSpec{}, "-"},
		{"name only", &inapi.AppSpec{Name: "mysql"}, "mysql"},
		{"version only", &inapi.AppSpec{Version: "8.0"}, "8.0"},
		{"name and version", &inapi.AppSpec{Name: "mysql", Version: "8.0"}, "mysql:8.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := specNameVersion(c.spec); got != c.want {
				t.Fatalf("specNameVersion() = %q, want %q", got, c.want)
			}
		})
	}
}