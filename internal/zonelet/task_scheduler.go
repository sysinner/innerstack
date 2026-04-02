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
	"fmt"
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

	// Restore zone VPC networking on leader
	if forceRefresh || status.Zonelet_HostOperateSet.Len() == 0 {

		if err := zoneNetMgr.Restore(config.Config.Zonelet.ZoneName); err != nil {
			return err
		}

		var zone inapi.Zone
		if rs := data.Zonelet.NewReader(
			inapi.NsZoneletInfo(config.Config.Zonelet.ZoneName)).Exec(); !rs.OK() {
			return rs.Error()
		} else if err := rs.Item().JsonDecode(&zone); err != nil {
			return err
		} else if zone.VpcBridgeCidr != "" && zone.VpcInstanceCidr != "" {
			if err := zoneNetMgr.ZoneSetup(config.Config.Zonelet.ZoneName,
				zone.VpcBridgeCidr, zone.VpcInstanceCidr, zone.VpcNetworkDomain); err != nil {
				slog.Error("scheduler VPC zone setup failed", "err", err.Error())
			}
		}

		if !zoneNetMgr.IsReady() && config.Config.Zonelet.VpcBridgeCidr != "" {
			if err := zoneNetMgr.ZoneSetup(
				config.Config.Zonelet.ZoneName,
				config.Config.Zonelet.VpcBridgeCidr,
				config.Config.Zonelet.VpcInstanceCidr,
				config.Config.Zonelet.VpcNetworkDomain); err != nil {
				slog.Error("scheduler VPC zone setup failed", "err", err.Error())
			}
		}
	}

	var (
		hosts          = []*inapi.Host{}
		instances      = []*inapi.AppInstance{}
		activeInstance *inapi.AppInstance
	)

	{
		var (
			offset = inapi.NsHostInfo(config.Config.Zonelet.ZoneName, "")
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

			if host.Deploy == nil {
				host.Deploy = &inapi.HostDeploy{}
			}
			status.Zonelet_HostSet.Store(host.Id, &host)
			hosts = append(hosts, &host)

			if !zoneNetMgr.IsReady() {
				continue
			}

			chg, err := zoneNetMgr.RefreshHostNetwork(config.Config.Zonelet.ZoneName, &host)
			if err != nil {
				slog.Warn("scheduler host network refresh fail",
					"host_id", host.Id,
					"peer", host.PeerAddr,
					"bridge-ip", host.Deploy.VpcBridgeIp,
					"instance_cidr", host.Deploy.VpcInstanceCidr,
					"err", err.Error())
				continue
			}
			if chg {
				slog.Warn("scheduler host network refresh",
					"host_id", host.Id,
					"peer", host.PeerAddr,
					"bridge-ip", host.Deploy.VpcBridgeIp,
					"instance_cidr", host.Deploy.VpcInstanceCidr)

				slog.Warn("scheduler host VPC setup",
					"host_id", host.Id,
					"host_peer", host.PeerAddr,
					"bridge_ip", host.Deploy.VpcBridgeIp,
					"instance_cidr", host.Deploy.VpcInstanceCidr)

				if rs := data.Zonelet.NewWriter(
					inapi.NsHostInfo(config.Config.Zonelet.ZoneName, host.Id), host).
					SetPrevVersion(item.Meta.Version).Exec(); !rs.OK() {
					return rs.Error()
				}
			}
		}
	}

	{
		offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, "")
		rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
		if !rs.OK() && !rs.NotFound() {
			return rs.Error()
		}
		for _, item := range rs.Items {
			var instance inapi.AppInstance
			if err := item.JsonDecode(&instance); err != nil {
				continue
			}
			if instance.Deploy == nil || instance.Spec == nil ||
				instance.Spec.Resources == nil {
				slog.Warn("scheduler skip instance with invalid operate or spec",
					"instance_id", instance.Id)
				continue
			}
			if activeInstance == nil {
				if len(instance.Deploy.Replicas) < int(instance.Deploy.ReplicaCap) {
					activeInstance = &instance
				} else {
					for _, rep := range instance.Deploy.Replicas {
						if rep.HostId == "" {
							activeInstance = &instance
							break
						}
					}
				}
			}
			instances = append(instances, &instance)

			if zoneNetMgr.IsReady() &&
				(forceRefresh || status.Zonelet_HostOperateSet.Len() == 0) {

				for _, rep := range instance.Deploy.Replicas {
					if rep.HostId == "" {
						continue
					}

					if rep.VpcIpv4 != "" {
						if s := zoneNetMgr.VpcInstance(rep.VpcIpv4); s == "" ||
							s != fmt.Sprintf("%s-%04x", instance.Id, rep.Id) {
							rep.VpcIpv4 = ""
						} else {
							continue
						}
					}

					if hostNet := zoneNetMgr.HostNetwork(rep.HostId); hostNet == nil {
						continue
					}
					rep.VpcIpv4 = zoneNetMgr.AllocHostSubNetwork(config.Config.Zonelet.ZoneName,
						rep.HostId, instance.Id, rep.Id)
					if rep.VpcIpv4 == "" {
						continue
					}
					key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, instance.Id)
					if rs := data.Zonelet.NewWriter(key, instance).Exec(); !rs.OK() {
						slog.Warn("scheduler update instance fail",
							"instance_id", instance.Id,
							"err", rs.ErrorMessage())
					} else {
						slog.Warn("scheduler alloc instance vpc",
							"instance_id", instance.Id,
							"ip", rep.VpcIpv4)
					}
				}
			}
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
		for _, host := range hosts {

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
		for _, rep := range instance.Deploy.Replicas {
			if rep.HostId == "" {
				continue
			}
			if host, ok := schedHosts[rep.HostId]; ok {
				host.CpuAlloc += instance.Deploy.CpuLimit
				host.MemAlloc += instance.Deploy.MemoryLimit
				host.Volumes[0].Alloc += instance.Deploy.VolumeLimit
			}
		}
	}

	for _, host := range schedResources.Hosts {
		hostOp := &inapi.HostOperate{
			CpuAlloc:     host.CpuAlloc,
			MemAlloc:     host.MemAlloc,
			StorageAlloc: host.Volumes[0].Alloc,
		}
		status.Zonelet_HostOperateSet.Store(host.Id, nil, hostOp)
	}

	if activeInstance == nil {
		return nil
	}

	sched := scheduler.NewScheduler()

	// Schedule replicas for each instance
	for _, instance := range []*inapi.AppInstance{activeInstance} {
		// Determine replica capacity (default 1, max 128)
		rc := instance.Deploy.ReplicaCap
		instance.Deploy.ReplicaCap = max(1, min(128, rc))

		sort.Slice(instance.Deploy.Replicas, func(i, j int) bool {
			return instance.Deploy.Replicas[i].Id < instance.Deploy.Replicas[j].Id
		})

		repLen := uint32(len(instance.Deploy.Replicas))
		for repId := repLen; repId < instance.Deploy.ReplicaCap; repId++ {
			newReplica := &inapi.AppDeployReplica{
				Id: repId,
			}
			instance.Deploy.Replicas = append(instance.Deploy.Replicas, newReplica)
		}

		// Schedule replicas
		for _, rep := range instance.Deploy.Replicas {
			if rep.HostId != "" {
				continue
			}
			srep := &typeScheduler.SchedulePodReplica{
				RepId: uint64(rep.Id),
				Cpu:   instance.Deploy.CpuLimit,
				Mem:   instance.Deploy.MemoryLimit,
				Vol:   instance.Deploy.VolumeLimit,
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

			rep.HostId = hit.HostId

			// Allocate VPC IP for the instance
			if zoneNetMgr.IsReady() {
				if hostNet := zoneNetMgr.HostNetwork(hit.HostId); hostNet != nil {
					rep.VpcIpv4 = zoneNetMgr.AllocHostSubNetwork(config.Config.Zonelet.ZoneName,
						hit.HostId, instance.Id, rep.Id)
				}
			}

			key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, instance.Id)
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
					host.CpuAlloc += instance.Deploy.CpuLimit
					host.MemAlloc += instance.Deploy.MemoryLimit
					host.Volumes[0].Alloc += instance.Deploy.VolumeLimit
				}

				if val, ok := status.Zonelet_HostOperateSet.Load(hit.HostId); ok {
					op := val.Value.(*inapi.HostOperate)
					op.CpuAlloc += instance.Deploy.CpuLimit
					op.MemAlloc += instance.Deploy.MemoryLimit
					op.StorageAlloc += instance.Deploy.VolumeLimit
				}
			}

			break
		}
	}

	return nil
}
