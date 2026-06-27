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

package scheduler

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func TestPriorityList(t *testing.T) {

	ls := []hostPriority{
		{
			id:    "2",
			score: 2,
		},
		{
			id:    "1",
			score: 1,
		},
		{
			id:    "3",
			score: 3,
		},
	}

	sort.Slice(ls, func(i, j int) bool {
		return ls[i].score < ls[j].score
	})

	if ls[0].id != "1" || ls[1].id != "2" || ls[2].id != "3" {
		t.Fatal("Failed TestPriority")
	}
}

func TestPrioritizer(t *testing.T) {

	fitHosts := []*hostFit{
		{
			id:       "2",
			cpuAlloc: 80,
			cpuTotal: 160,
			memAlloc: 5,
			memTotal: 10,
		},
		{
			id:       "1",
			cpuAlloc: 10,
			cpuTotal: 160,
			memAlloc: 1,
			memTotal: 10,
		},
		{
			id:       "3",
			cpuAlloc: 160,
			cpuTotal: 160,
			memAlloc: 10,
			memTotal: 10,
		},
	}

	ls, err := prioritizer(fitHosts)
	if err != nil {
		t.Fatal("Failed TestPriority")
	}

	if ls[0].id != "1" || ls[1].id != "2" || ls[2].id != "3" {
		t.Fatal("Failed TestPriority")
	}
}

var (
	hosts ScheduleHostList
)

func benchInit() {

	if len(hosts.Hosts) > 0 {
		return
	}

	// 5000 hosts in one zone-master
	for i := 0; i < 5000; i++ {
		hosts.Hosts = append(hosts.Hosts, &ScheduleHostItem{
			Id:       fmt.Sprintf("%d", i),
			OpAction: []string{inapi.HostSetupStart},
			CpuTotal: 320,
			CpuUsed:  int64(rand.Int63n(160)),
			MemTotal: 64 * 1024,
			MemUsed:  int64(rand.Int63n(32)),
		})
	}
}

func Benchmark_Schedule(b *testing.B) {

	benchInit()

	schedulerBench := NewScheduler()

	for i := 0; i < b.N; i++ {

		rep := &ScheduleAppReplica{
			RepId: 0,
			Cpu:   int64(rand.Int63n(160)),
			Mem:   int64(rand.Int63n(32)),
		}

		if host, err := schedulerBench.ScheduleHost(rep, &hosts, nil); err != nil {
			b.Fatalf("Failed Benchmark_Prioritizer %s", err.Error())
		} else if host.HostId == "" {
			b.Fatal("Failed Benchmark_Prioritizer")
			// host.CpuUsed += rep.Cpu
			// host.MemUsed += rep.Mem
		}
	}
}
