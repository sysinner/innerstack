// Copyright 2015 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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
	"log/slog"
	"sort"

	"github.com/sysinner/incore/v2/inapi"
	typeScheduler "github.com/sysinner/incore/v2/inapi/scheduler"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/status"
	"github.com/sysinner/incore/v2/internal/zonelet/scheduler"
)

func schedulerRefresh(forceRefresh bool) error {

	if !status.IsZoneletLeader() {
		return nil
	}

	var (
		instances      = []*inapi.AppInstance{}
		activeInstance *inapi.AppInstance
	)

	{
		offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneId, "")
		rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
		if !rs.OK() && !rs.NotFound() {
			return rs.Error()
		}
		for _, item := range rs.Items {
			var instance inapi.AppInstance
			if err := item.JsonDecode(&instance); err != nil {
				continue
			}
			if instance.Operate == nil || instance.Spec == nil ||
				instance.Spec.Resources == nil {
				slog.Warn("scheduler skip instance with invalid operate or spec",
					"instance_id", instance.Id)
				continue
			}
			if activeInstance == nil {
				if len(instance.Operate.Replicas) < int(instance.Operate.ReplicaCap) {
					activeInstance = &instance
				} else {
					for _, rep := range instance.Operate.Replicas {
						if rep.HostId == "" {
							activeInstance = &instance
							break
						}
					}
				}
			}
			instances = append(instances, &instance)
		}
	}

	if activeInstance == nil && status.Zonelet_HostOperateSet.Len() > 0 {
		return nil
	}

	var (
		schedResources = &typeScheduler.ScheduleHostList{}
		schedHosts     = map[string]*typeScheduler.ScheduleHostItem{}
	)

	{
		var (
			offset = inapi.NsHostInfo(config.Config.Zonelet.ZoneId, "")
			rs     = data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
		)
		if !rs.OK() && !rs.NotFound() {
			return rs.Error()
		}
		for _, item := range rs.Items {
			var host inapi.Host
			if err := item.JsonDecode(&host); err != nil {
				continue
			}

			if hostStatus, ok := status.Zonelet_HostStatusSet.Load(host.Id); !ok {
				continue
			} else {
				host.Status = hostStatus.(*inapi.HostStatus)
			}

			if host.Status.CpuCores <= 0 ||
				host.Status.MemTotal <= 0 ||
				host.Status.DiskTotalBytes <= 0 {
				continue
			}

			host.Operate = &inapi.HostOperate{}

			schedHostItem := &typeScheduler.ScheduleHostItem{
				Id:       host.Id,
				OpAction: []string{inapi.HostSetupStart},

				CpuTotal: int64(host.Status.CpuCores) * 1000,
				CpuUsed:  host.Status.CpuSys + host.Status.CpuUser,

				MemTotal: host.Status.MemTotal,
				MemUsed:  host.Status.MemUsed,

				Volumes: []*typeScheduler.ScheduleHostVolume{
					{
						Name:  "default",
						Total: host.Status.DiskTotalBytes,
						Used:  host.Status.DiskTotalBytes - host.Status.DiskFreeBytes,
					},
				},
			}

			schedHosts[host.Id] = schedHostItem
			schedResources.Hosts = append(schedResources.Hosts, schedHostItem)
		}
	}

	if len(schedHosts) == 0 {
		slog.Warn("scheduler no available hosts")
		return nil
	}

	// Calculate already allocated resources from existing replicas
	for _, instance := range instances {
		for _, rep := range instance.Operate.Replicas {
			if rep.HostId == "" {
				continue
			}
			if host, ok := schedHosts[rep.HostId]; ok {
				host.CpuAlloc += instance.Operate.CpuLimit
				host.MemAlloc += instance.Operate.MemoryLimit
				host.Volumes[0].Alloc += instance.Operate.VolumeLimit
			}
		}
	}

	for _, host := range schedResources.Hosts {
		status.Zonelet_HostOperateSet.Store(host.Id, nil, &inapi.HostOperate{
			CpuAlloc:     host.CpuAlloc,
			MemAlloc:     host.MemAlloc,
			StorageAlloc: host.Volumes[0].Alloc,
		})
	}

	if activeInstance == nil {
		return nil
	}

	sched := scheduler.NewScheduler()

	// Schedule replicas for each instance
	for _, instance := range []*inapi.AppInstance{activeInstance} {
		// Determine replica capacity (default 1, max 128)
		rc := instance.Operate.ReplicaCap
		instance.Operate.ReplicaCap = max(1, min(128, rc))

		sort.Slice(instance.Operate.Replicas, func(i, j int) bool {
			return instance.Operate.Replicas[i].Id < instance.Operate.Replicas[j].Id
		})

		repLen := uint32(len(instance.Operate.Replicas))
		for repId := repLen; repId < instance.Operate.ReplicaCap; repId++ {
			newReplica := &inapi.AppOperateReplica{
				Id: repId,
			}
			instance.Operate.Replicas = append(instance.Operate.Replicas, newReplica)
		}

		// Schedule replicas
		for _, rep := range instance.Operate.Replicas {
			if rep.HostId != "" {
				continue
			}
			srep := &typeScheduler.SchedulePodReplica{
				RepId: uint64(rep.Id),
				Cpu:   instance.Operate.CpuLimit,
				Mem:   instance.Operate.MemoryLimit,
				Vol:   instance.Operate.VolumeLimit,
			}

			hit, err := sched.ScheduleHost(srep, schedResources, nil)
			if err != nil {
				slog.Warn("scheduler failed",
					"instance_id", instance.Id,
					"instance_name", instance.Name,
					"err", err.Error())
				break
			}

			if hit == nil {
				slog.Warn("scheduler no host fit",
					"instance_id", instance.Id,
					"instance_name", instance.Name)
				break
			}

			key := inapi.NsAppInstance(config.Config.Zonelet.ZoneId, instance.Id)
			if rs := data.Zonelet.NewWriter(key, instance).Exec(); !rs.OK() {
				slog.Warn("scheduler update instance fail",
					"instance_id", instance.Id,
					"err", rs.ErrorMessage())
			} else {
				slog.Info("scheduler assigned host",
					"instance_id", instance.Id,
					"instance_name", instance.Name,
					"replica_id", rep.Id,
					"host_id", hit.HostId)

				if host, ok := schedHosts[hit.HostId]; ok {
					host.CpuAlloc += instance.Operate.CpuLimit
					host.MemAlloc += instance.Operate.MemoryLimit
					host.Volumes[0].Alloc += instance.Operate.VolumeLimit
				}

				if val, ok := status.Zonelet_HostOperateSet.Load(hit.HostId); ok {
					op := val.Value.(*inapi.HostOperate)
					op.CpuAlloc += instance.Operate.CpuLimit
					op.MemAlloc += instance.Operate.MemoryLimit
					op.StorageAlloc += instance.Operate.VolumeLimit
				}
			}

			break
		}
	}

	return nil
}
