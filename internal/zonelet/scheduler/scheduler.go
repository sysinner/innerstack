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
	"errors"
	"slices"
	"sort"

	"github.com/sysinner/incore/v2/pkg/inapi"
)

var (
	errBadArgument = errors.New("BadArgument")
	cpuOverAlloc   = float64(3.0)
	memOverAlloc   = float64(1.1)
)

type genericScheduler struct {
}

func NewScheduler() Scheduler {
	return &genericScheduler{}
}

func (*genericScheduler) ScheduleHost(
	rep *SchedulePodReplica,
	hostls *ScheduleHostList,
	opts *ScheduleOptions,
) (
	hit *ScheduleHitItem,
	err error,
) {

	//
	if rep == nil || len(hostls.Hosts) < 1 {
		return nil, errBadArgument
	}

	fitHostList, err := findHostListThatFit(rep, hostls, opts)
	if err != nil {
		return nil, err
	}

	priorityList, err := prioritizer(fitHostList)
	if err != nil {
		return nil, err
	}

	if len(priorityList) == 0 {
		return nil, errors.New("No Host Scheduled")
	}

	for _, v := range hostls.Hosts {

		if v.Id != priorityList[0].id {
			continue
		}

		return &ScheduleHitItem{
			HostId: v.Id,
			Host:   v,
			Volumes: []*ScheduleHitVolume{
				{
					Name: priorityList[0].volume,
					Size: rep.Vol,
				},
			},
		}, nil
	}

	return nil, errors.New("No Host Scheduled")
}

func findHostListThatFit(
	rep *SchedulePodReplica,
	hostls *ScheduleHostList,
	opts *ScheduleOptions,
) ([]*hostFit, error) {

	var (
		hostFits     = []*hostFit{}
		hostExcludes = []string{}
		specCpu      = rep.Cpu
		specMem      = rep.Mem
	)

	if opts != nil {
		if len(opts.HostExcludes) > 0 {
			hostExcludes = opts.HostExcludes
		}
	}

	for _, v := range hostls.Hosts {
		if !slices.Contains(v.OpAction, inapi.HostSetupStart) ||
			len(v.Volumes) < 1 {
			continue
		}

		if len(hostExcludes) > 0 {
			found := false
			for _, hostId := range hostExcludes {
				if hostId == v.Id {
					found = true
					break
				}
			}
			if found {
				continue
			}
		}

		cpuCap := int64(float64(v.CpuTotal) * cpuOverAlloc)
		memCap := int64(float64(v.MemTotal) * memOverAlloc)

		if (specCpu+max(v.CpuUsed, v.CpuAlloc)) > cpuCap ||
			(specMem+max(v.MemUsed, v.MemAlloc)) > memCap {
			continue // TODO
		}

		sort.Slice(v.Volumes, func(i, j int) bool {
			return (v.Volumes[i].Total - max(v.Volumes[i].Used, v.Volumes[i].Alloc)) >
				(v.Volumes[j].Total - max(v.Volumes[j].Used, v.Volumes[j].Alloc))
		})

		volFit := ""
		for _, vp := range v.Volumes {

			if max(vp.Used, vp.Alloc)+rep.Vol <= vp.Total {
				volFit = vp.Name
				break
			}
		}

		if volFit == "" {
			continue
		}

		hostFits = append(hostFits, &hostFit{
			id:       v.Id,
			cpuAlloc: v.CpuAlloc,
			cpuTotal: cpuCap,
			cpuUsed:  v.CpuUsed,
			memAlloc: v.MemAlloc,
			memTotal: memCap,
			memUsed:  v.MemUsed,
			volume:   volFit,
		})
	}

	return hostFits, nil
}

func (*genericScheduler) ScheduleHostValid(
	host *ScheduleHostItem,
	entry *SchedulePodReplica,
) error {

	cpuCap := int64(float64(host.CpuTotal) * cpuOverAlloc)
	memCap := int64(float64(host.MemTotal) * memOverAlloc)

	if (entry.Cpu+max(host.CpuUsed, host.CpuAlloc)) > cpuCap ||
		(entry.Mem+max(host.MemUsed, host.MemAlloc)) > memCap {
		return errors.New("No Resource Fit")
	}

	return nil
}
