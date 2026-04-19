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
	"slices"
	"sort"
	"time"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/status"
	"github.com/sysinner/incore/v2/internal/zonelet/network"
	"github.com/sysinner/incore/v2/internal/zonelet/scheduler"
)

var (
	lastRefreshed int64 = 0
	forceFreshTTL int64 = 1800
)

func schedulerRefresh(forceRefresh bool) error {

	if !status.IsZoneletLeader() {
		return nil
	}

	tn := time.Now().Unix()

	if !forceRefresh &&
		(lastRefreshed+forceFreshTTL < tn || gHostOperateSet.Len() == 0 ||
			status.Zonelet_ForceRefresh.Load()) {
		forceRefresh = true
		lastRefreshed = tn
		status.Zonelet_ForceRefresh.Store(false)
	}

	// Restore zone VPC networking on leader
	if forceRefresh {

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
		hostPortMap = map[string][]uint32{}

		instanceMap = []*appInstanceEntry{}

		activeInstance *appInstanceEntry
	)

	{
		var (
			offset = inapi.NsHostInfo(config.Config.Zonelet.ZoneName, "")
			rs     = data.Zonelet.NewRanger(offset, append(offset, 0xff)).
				SetLimit(inapi.Zonelet_MaxHosts).Exec()
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

			gHostSet.Store(host.Id, item.Meta, &host)

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
			} else if !chg {
				continue
			}

			slog.Warn("scheduler host VPC setup",
				"host_id", host.Id,
				"host_peer", host.PeerAddr,
				"bridge_ip", host.Deploy.VpcBridgeIp,
				"instance_cidr", host.Deploy.VpcInstanceCidr)

			if rs := data.Zonelet.NewWriter(
				inapi.NsHostInfo(config.Config.Zonelet.ZoneName, host.Id), &host).
				SetPrevVersion(item.Meta.Version).Exec(); !rs.OK() {
				return rs.Error()
			}
		}
	}

	if forceRefresh || !gAppInstanceSet.IsReady() {
		if err := gAppInstanceSet.Refresh(); err != nil {
			return err
		}
	}

	gAppInstanceSet.Iter(func(app *appInstanceEntry) bool {
		if activeInstance == nil {
			if len(app.Value.Deploy.Replicas) < int(app.Value.Deploy.ReplicaCap) {
				activeInstance = app
			} else {
				for _, rep := range app.Value.Deploy.Replicas {
					if rep.HostId == "" {
						activeInstance = app
						break
					}
				}
			}
		}

		for _, rep := range app.Value.Deploy.Replicas {
			if rep.HostId == "" {
				continue
			}
			if _, ok := hostPortMap[rep.HostId]; !ok {
				hostPortMap[rep.HostId] = []uint32{}
			}
			for _, sp := range rep.ServicePorts {
				if sp.HostPort > 0 &&
					!slices.Contains(hostPortMap[rep.HostId], sp.HostPort) {
					hostPortMap[rep.HostId] = append(hostPortMap[rep.HostId], sp.HostPort)
				}
			}
		}

		instanceMap = append(instanceMap, app)

		return true
	})

	// // Resolve app dependencies for all instances (inject dependent configs/services)
	// for _, instance := range instanceMap {
	// 	resolveAppDependencies(instance)
	// }

	{
		for hostId, ports := range hostPortMap {

			item := gHostSet.Load(hostId)
			if item == nil {
				continue
			}
			host := item.Value.(*inapi.Host)

			if host.Deploy == nil {
				host.Deploy = &inapi.HostDeploy{}
			}

			slices.Sort(host.Deploy.PortUsed)
			slices.Sort(ports)
			if slices.Compare(host.Deploy.PortUsed, ports) == 0 {
				continue
			}

			host.Deploy.PortUsed = ports
			hostKey := inapi.NsHostInfo(config.Config.Zonelet.ZoneName, host.Id)
			if rs := data.Zonelet.NewWriter(hostKey, host).SetPrevVersion(
				item.Meta.Version).Exec(); !rs.OK() {
				slog.Warn("scheduler update host port_used fail",
					"host_id", host.Id,
					"err", rs.ErrorMessage())
				return rs.Error()
			} else {
				slog.Warn("scheduler update host port_used",
					"host_id", host.Id,
					"ports", ports)
				item.Meta.Version = rs.Item().Meta.Version
			}
		}
	}

	for _, app := range instanceMap {

		ports := map[uint32]*inapi.AppSpecServicePort{}
		for _, sp := range app.Value.Spec.ServicePorts {
			if sp == nil || sp.Port < 1 || sp.Port >= 65536 {
				continue
			}
			ports[sp.Port] = sp
		}

		for _, rep := range app.Value.Deploy.Replicas {
			if rep.HostId == "" {
				continue
			}
			kvHost := gHostSet.Load(rep.HostId)
			if kvHost == nil {
				continue
			}
			host := kvHost.Value.(*inapi.Host)

			flush := false
			setPorts := make([]*inapi.AppDeployServicePort, 0, len(ports))
			for _, sp := range rep.ServicePorts {
				if _, ok := ports[sp.Port]; ok && sp.HostPort > 0 &&
					!slices.ContainsFunc(setPorts, func(p *inapi.AppDeployServicePort) bool {
						return p.Port == sp.Port
					}) {
					setPorts = append(setPorts, sp)
				}
			}

			if len(setPorts) == len(ports) {
				continue
			}

			for _, sp := range ports {

				if slices.ContainsFunc(setPorts, func(p *inapi.AppDeployServicePort) bool {
					return p.Port == sp.Port
				}) {
					continue
				}

				hp := network.HostPortAlloc(host.Deploy.PortUsed, 0)
				if hp == 0 {
					slog.Warn("scheduler port alloc failed",
						"host_id", rep.HostId,
						"box_port", sp.Port)
					continue
				}
				slog.Warn("scheduler port alloc",
					"host_id", rep.HostId,
					"box_port", sp.Port,
					"host_port", hp)

				host.Deploy.PortUsed = append(host.Deploy.PortUsed, hp)
				slices.Sort(host.Deploy.PortUsed)
				setPorts = append(setPorts, &inapi.AppDeployServicePort{
					Name:     sp.Name,
					Port:     sp.Port,
					HostPort: hp,
				})
				flush = true
			}

			if !flush {
				continue
			}

			rep.ServicePorts = setPorts

			if err := gAppInstanceSet.Flush(app); err != nil {
				slog.Warn("scheduler update instance fail",
					"instance_id", app.Value.Id,
					"err", err.Error())
				return err
			}

			slog.Warn("scheduler alloc instance vpc",
				"instance_id", app.Value.Id,
				"rep_id", rep.Id,
				"ports", setPorts)

			// Persist host with updated port_used
			hostKey := inapi.NsHostInfo(config.Config.Zonelet.ZoneName, host.Id)
			if rs := data.Zonelet.NewWriter(hostKey, host).SetPrevVersion(
				kvHost.Meta.Version).Exec(); !rs.OK() {
				slog.Warn("scheduler update host port_used fail",
					"host_id", host.Id,
					"err", rs.ErrorMessage())
				return rs.Error()
			} else {
				kvHost.Meta = rs.Item().Meta
			}
		}

		if !zoneNetMgr.IsReady() {
			continue
		}

		for _, rep := range app.Value.Deploy.Replicas {
			if rep.HostId == "" {
				continue
			}

			if rep.VpcIpv4 != "" {
				if s := zoneNetMgr.VpcInstance(rep.VpcIpv4); s == "" ||
					s != fmt.Sprintf("%s-%04x", app.Value.Id, rep.Id) {
					rep.VpcIpv4 = ""
				} else {
					continue
				}
			}

			if hostNet := zoneNetMgr.HostNetwork(rep.HostId); hostNet == nil {
				continue
			}
			rep.VpcIpv4 = zoneNetMgr.AllocHostSubNetwork(config.Config.Zonelet.ZoneName,
				rep.HostId, app.Value.Id, rep.Id)
			if rep.VpcIpv4 == "" {
				continue
			}

			if err := gAppInstanceSet.Flush(app); err != nil {
				slog.Warn("scheduler update instance fail",
					"instance_id", app.Value.Id,
					"err", err.Error())
				return err
			}

			slog.Warn("scheduler alloc instance vpc",
				"instance_id", app.Value.Id,
				"ip", rep.VpcIpv4)
		}
	}

	if activeInstance == nil && gHostOperateSet.Len() > 0 {
		return nil
	}

	var (
		schedResources = &scheduler.ScheduleHostList{}
		schedHosts     = map[string]*scheduler.ScheduleHostItem{}
	)

	gHostSet.Iter(func(kvHost *inapi.KvEntry) bool {

		host := kvHost.Value.(*inapi.Host)
		if hostStatus, ok := status.Zonelet_HostStatusSet.Load(host.Id); !ok {
			return true
		} else {
			host.Status = hostStatus.(*inapi.HostStatus)
		}

		if host.Status.CpuCores <= 0 ||
			host.Status.MemTotal <= 0 ||
			host.Status.DiskTotalBytes <= 0 {
			return true
		}

		host.Operate = &inapi.HostOperate{}

		schedHostItem := &scheduler.ScheduleHostItem{
			Id:       host.Id,
			OpAction: []string{inapi.HostSetupStart},

			CpuTotal: int64(host.Status.CpuCores) * 1000,
			CpuUsed:  host.Status.CpuSys + host.Status.CpuUser,

			MemTotal: host.Status.MemTotal,
			MemUsed:  host.Status.MemUsed,

			Volumes: []*scheduler.ScheduleHostVolume{
				{
					Name:  "default",
					Total: host.Status.DiskTotalBytes,
					Used:  host.Status.DiskTotalBytes - host.Status.DiskFreeBytes,
				},
			},
		}

		schedHosts[host.Id] = schedHostItem
		schedResources.Hosts = append(schedResources.Hosts, schedHostItem)

		return true
	})

	if len(schedHosts) == 0 {
		slog.Warn("scheduler no available hosts")
		return nil
	}

	// Calculate already allocated resources from existing replicas
	for _, app := range instanceMap {
		for _, rep := range app.Value.Deploy.Replicas {
			if rep.HostId == "" {
				continue
			}
			if host, ok := schedHosts[rep.HostId]; ok {
				host.CpuAlloc += app.Value.Deploy.CpuLimit
				host.MemAlloc += app.Value.Deploy.MemoryLimit
				host.Volumes[0].Alloc += app.Value.Deploy.VolumeLimit
			}
		}
	}

	for _, host := range schedResources.Hosts {
		hostOp := &inapi.HostOperate{
			CpuAlloc:     host.CpuAlloc,
			MemAlloc:     host.MemAlloc,
			StorageAlloc: host.Volumes[0].Alloc,
		}
		gHostOperateSet.Store(host.Id, nil, hostOp)
	}

	if activeInstance == nil {
		return nil
	}

	sched := scheduler.NewScheduler()

	// Schedule replicas for each instance
	for _, app := range []*appInstanceEntry{activeInstance} {
		// Determine replica capacity (default 1, max 128)
		rc := app.Value.Deploy.ReplicaCap
		app.Value.Deploy.ReplicaCap = max(1, min(128, rc))

		sort.Slice(app.Value.Deploy.Replicas, func(i, j int) bool {
			return app.Value.Deploy.Replicas[i].Id < app.Value.Deploy.Replicas[j].Id
		})

		repLen := uint32(len(app.Value.Deploy.Replicas))
		for repId := repLen; repId < app.Value.Deploy.ReplicaCap; repId++ {
			newReplica := &inapi.AppDeployReplica{
				Id: repId,
			}
			app.Value.Deploy.Replicas = append(app.Value.Deploy.Replicas, newReplica)
		}

		// Schedule replicas
		for _, rep := range app.Value.Deploy.Replicas {
			if rep.HostId != "" {
				continue
			}
			srep := &scheduler.SchedulePodReplica{
				RepId: uint64(rep.Id),
				Cpu:   app.Value.Deploy.CpuLimit,
				Mem:   app.Value.Deploy.MemoryLimit,
				Vol:   app.Value.Deploy.VolumeLimit,
			}

			hit, err := sched.ScheduleHost(srep, schedResources, nil)
			if err != nil {
				slog.Warn("scheduler failed",
					"instance_id", app.Value.Id,
					"instance_name", app.Value.Name,
					"sched-hosts", len(schedResources.Hosts),
					"err", err.Error())
				break
			}

			if hit == nil {
				slog.Warn("scheduler no host fit",
					"instance_id", app.Value.Id,
					"instance_name", app.Value.Name)
				break
			}

			kvHost := gHostSet.Load(hit.HostId)
			if kvHost == nil {
				break
			}
			host := kvHost.Value.(*inapi.Host)

			rep.HostId = hit.HostId

			// Allocate VPC IP for the instance
			if zoneNetMgr.IsReady() {
				if hostNet := zoneNetMgr.HostNetwork(hit.HostId); hostNet != nil {
					rep.VpcIpv4 = zoneNetMgr.AllocHostSubNetwork(config.Config.Zonelet.ZoneName,
						hit.HostId, app.Value.Id, rep.Id)
				}
			}

			rep.ServicePorts = []*inapi.AppDeployServicePort{}

			for _, sp := range app.Value.Spec.ServicePorts {
				if sp == nil || sp.Port < 1 || sp.Port >= 65536 {
					continue
				}

				hp := network.HostPortAlloc(host.Deploy.PortUsed, 0)
				if hp == 0 {
					slog.Warn("scheduler port alloc failed",
						"host_id", hit.HostId,
						"box_port", sp.Port)
					continue
				}
				host.Deploy.PortUsed = append(host.Deploy.PortUsed, hp)
				slices.Sort(host.Deploy.PortUsed)
				rep.ServicePorts = append(rep.ServicePorts, &inapi.AppDeployServicePort{
					Name:     sp.Name,
					Port:     sp.Port,
					HostPort: hp,
				})
			}

			if err := gAppInstanceSet.Flush(app); err != nil {
				slog.Warn("scheduler update instance fail",
					"instance_id", app.Value.Id,
					"err", err.Error())
				return err
			}

			slog.Info("scheduler assigned host",
				"instance_id", app.Value.Id,
				"instance_name", app.Value.Name,
				"replica_id", rep.Id,
				"host_id", hit.HostId)

			if host, ok := schedHosts[hit.HostId]; ok {
				host.CpuAlloc += app.Value.Deploy.CpuLimit
				host.MemAlloc += app.Value.Deploy.MemoryLimit
				host.Volumes[0].Alloc += app.Value.Deploy.VolumeLimit
			}

			if val := gHostOperateSet.Load(hit.HostId); val != nil {
				op := val.Value.(*inapi.HostOperate)
				op.CpuAlloc += app.Value.Deploy.CpuLimit
				op.MemAlloc += app.Value.Deploy.MemoryLimit
				op.StorageAlloc += app.Value.Deploy.VolumeLimit
			}

			// Persist host with updated port_used
			hostKey := inapi.NsHostInfo(config.Config.Zonelet.ZoneName, host.Id)
			if rs := data.Zonelet.NewWriter(hostKey, host).SetPrevVersion(
				kvHost.Meta.Version).Exec(); !rs.OK() {
				slog.Warn("scheduler update host port_used fail",
					"host_id", host.Id,
					"err", rs.ErrorMessage())
				return rs.Error()
			} else {
				kvHost.Meta = rs.Item().Meta
			}

			break
		}
	}

	return nil
}
