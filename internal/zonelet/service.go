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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/internal/status"
)

type zoneServer struct {
	inapi.UnimplementedZoneletServer
}

func NewServer() inapi.ZoneletServer {
	return &zoneServer{}
}

func (s *zoneServer) ZoneInit(
	ctx context.Context, req *inapi.ZoneInitRequest,
) (*inapi.ZoneInitResponse, error) {

	req.Name = strings.ToLower(req.Name)

	if err := inapi.NameValid(req.Name); err != nil {
		return nil, err
	}

	if config.Config.Zonelet.ZoneId != "" ||
		len(config.Config.Server.ZoneHosts) > 0 {
		return nil, errors.New("System already initialized")
	}

	config.Config.Zonelet.ZoneId = req.Name
	config.Config.Server.ZoneHosts = []string{
		fmt.Sprintf("%s:%d", config.Config.Hostlet.LanAddr, config.Config.Server.PeerPort),
	}

	zone := &inapi.Zone{
		Id:    req.Name,
		Hosts: config.Config.Server.ZoneHosts,
	}

	if rs := data.Zonelet.NewWriter(
		inapi.NsZoneletInfo(zone.Id), zone).
		SetCreateOnly(true).Exec(); !rs.OK() {
		return nil, rs.Error()
	}

	slog.Warn("zonelet init-zone",
		"zone_id", zone.Id,
		"host_id", config.Config.Hostlet.HostId,
	)

	if err := config.Flush(); err != nil {
		return nil, err
	}

	{
		host := &inapi.Host{
			Id:        config.Config.Hostlet.HostId,
			PeerAddr:  fmt.Sprintf("%s:%d", config.Config.Hostlet.LanAddr, config.Config.Server.PeerPort),
			SecretKey: config.Config.Hostlet.SecretKey,
		}

		if rs := data.Zonelet.NewWriter(
			inapi.NsHostInfo(config.Config.Zonelet.ZoneId, host.Id), host).
			SetCreateOnly(true).Exec(); !rs.OK() {
			return nil, rs.Error()
		}

		slog.Warn("zonelet init-host",
			"host_id", config.Config.Hostlet.HostId,
		)
	}

	return &inapi.ZoneInitResponse{}, nil
}

func (s *zoneServer) ZoneInfo(
	ctx context.Context, req *inapi.ZoneInfoRequest,
) (*inapi.ZoneInfoResponse, error) {

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	var zone inapi.Zone

	if rs := data.Zonelet.NewReader(
		inapi.NsZoneletInfo(config.Config.Zonelet.ZoneId)).
		Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("System uninitialized")
		}
		return nil, rs.Error()
	} else if err := rs.Item().JsonDecode(&zone); err != nil {
		return nil, err
	}

	return &inapi.ZoneInfoResponse{
		Zone: &zone,
	}, nil
}

func (s *zoneServer) HostJoin(
	ctx context.Context, req *inapi.HostJoinRequest,
) (*inapi.HostJoinResponse, error) {

	if err := inapi.Ip4AddrValid(req.Addr); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	conn, err := client.Connect(req.Addr, nil, false)
	if err != nil {
		return nil, err
	}

	hc := inapi.NewHostletClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req2 := &inapi.HostInitRequest{
		ZoneId:    config.Config.Zonelet.ZoneId,
		ZoneHosts: config.Config.Server.ZoneHosts,
		Token:     req.Token,
	}

	resp, err := hc.HostInit(ctx, req2)
	if err != nil {
		return nil, fmt.Errorf("failed to join host: %s", err.Error())
	}

	host := &inapi.Host{
		Id:        resp.HostId,
		PeerAddr:  req.Addr,
		SecretKey: req.Token,
	}

	if rs := data.Zonelet.NewWriter(
		inapi.NsHostInfo(config.Config.Zonelet.ZoneId, resp.HostId), host).
		SetCreateOnly(true).Exec(); !rs.OK() {
		return nil, rs.Error()
	}

	if !inapi.ObjectIdValid.MatchString(resp.HostId) {
		return nil, errors.New("invalid host_id")
	}

	slog.Warn("zonelet init-host",
		"host_id", resp.HostId,
	)

	return &inapi.HostJoinResponse{
		Status: resp.Status,
	}, nil
}

func (s *zoneServer) HostList(
	ctx context.Context, req *inapi.HostListRequest,
) (*inapi.HostListResponse, error) {

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	resp := &inapi.HostListResponse{}

	offset := inapi.NsHostInfo(config.Config.Zonelet.ZoneId, "")

	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
	for _, item := range rs.Items {
		var host inapi.Host
		if err := item.JsonDecode(&host); err == nil {
			host.SecretKey = ""
			if val, ok := status.Zonelet_HostStatusSet.Load(host.Id); ok {
				host.Status = val.(*inapi.HostStatus)
			}
			if val, ok := status.Zonelet_HostOperateSet.Load(host.Id); ok {
				host.Operate = val.Value.(*inapi.HostOperate)
			}
			resp.Hosts = append(resp.Hosts, &host)
		}
	}

	return resp, nil
}

func (s *zoneServer) HostStatusUpdate(
	ctx context.Context, req *inapi.HostStatusUpdateRequest,
) (*inapi.HostStatusUpdateResponse, error) {
	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Host == nil ||
		req.Status == nil {
		return nil, errors.New("bad request")
	}

	if !inapi.ObjectIdValid.MatchString(req.Host.Id) {
		return nil, errors.New("invalid host_id")
	}

	resp := &inapi.HostStatusUpdateResponse{}

	key := inapi.NsHostStatus(config.Config.Zonelet.ZoneId,
		req.Host.Id)

	if rs := data.Zonelet.NewWriter(key, req.Status).Exec(); !rs.OK() {
		return nil, rs.Error()
	}

	status.Zonelet_HostStatusSet.Store(req.Host.Id, req.Status)

	slog.Debug("zonelist update host status", "host_id", req.Host.Id, "status", req.Status)

	// Query app instances associated with this host_id
	offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneId, "")
	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
	for _, item := range rs.Items {
		var instance inapi.AppInstance
		if err := item.JsonDecode(&instance); err == nil {
			if instance.Operate != nil {
				for _, rep := range instance.Operate.Replicas {
					if rep.HostId == req.Host.Id {
						resp.AppInstances = append(resp.AppInstances, &instance)
						break
					}
				}
			}
		}
	}

	return resp, nil
}

func (s *zoneServer) AppInstanceDeploy(
	ctx context.Context, req *inapi.AppInstanceDeployRequest,
) (*inapi.AppInstanceDeployResponse, error) {
	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Spec == nil {
		return nil, errors.New("spec is required")
	}

	if req.Spec.Name == "" {
		return nil, errors.New("spec.name is required")
	}

	cpuLimit := int64(0)
	if req.Spec.CpuLimit != "" {
		if v, err := inutil.ParseCPUs(req.Spec.CpuLimit); err != nil {
			return nil, fmt.Errorf("invalid cpu_limit: %w", err)
		} else {
			cpuLimit = v
		}
	}

	memoryLimit := int64(0)
	if req.Spec.MemoryLimit != "" {
		if v, err := inutil.ParseBytes(req.Spec.MemoryLimit); err != nil {
			return nil, fmt.Errorf("invalid memory_limit: %w", err)
		} else {
			memoryLimit = v
		}
	}

	volumeLimit := int64(0)
	if req.Spec.VolumeLimit != "" {
		if v, err := inutil.ParseBytes(req.Spec.VolumeLimit); err != nil {
			return nil, fmt.Errorf("invalid volume_limit: %w", err)
		} else {
			volumeLimit = v
		}
	}

	if cpuLimit < inapi.CPUMin || cpuLimit > inapi.CPUMax {
		return nil, fmt.Errorf("spec.cpu_limit must be between %d and %d", inapi.CPUMin, inapi.CPUMax)
	}

	if memoryLimit < inapi.MemoryMin || memoryLimit > inapi.MemoryMax {
		return nil, fmt.Errorf("spec.memory_limit must be between %d and %d", inapi.MemoryMin, inapi.MemoryMax)
	}

	if volumeLimit < inapi.VolumeMin || volumeLimit > inapi.VolumeMax {
		return nil, fmt.Errorf("spec.volume_limit must be between %d and %d", inapi.VolumeMin, inapi.VolumeMax)
	}

	var instance *inapi.AppInstance

	if req.Id != "" {
		// 更新现有实例
		key := inapi.NsAppInstance(config.Config.Zonelet.ZoneId, req.Id)

		var existingInstance inapi.AppInstance
		if rs := data.Zonelet.NewReader(key).Exec(); !rs.OK() {
			if rs.NotFound() {
				return nil, errors.New("instance not found")
			}
			return nil, rs.Error()
		} else if err := rs.Item().JsonDecode(&existingInstance); err != nil {
			return nil, err
		}

		// 更新实例的 spec 和 operate
		instance = &existingInstance
		instance.Spec = req.Spec
		if instance.Operate == nil {
			instance.Operate = &inapi.AppOperate{}
		}

		instance.Operate.CpuLimit = cpuLimit
		instance.Operate.MemoryLimit = memoryLimit
		instance.Operate.VolumeLimit = volumeLimit

		if req.ReplicaCap > 0 {
			instance.Operate.ReplicaCap = min(128, req.ReplicaCap)
		}

		if rs := data.Zonelet.NewWriter(key, instance).Exec(); !rs.OK() {
			return nil, rs.Error()
		}

		slog.Warn("zonelet app-instance-update",
			"instance_id", req.Id,
			"instance_name", instance.Name,
			"replica_cap", instance.Operate.ReplicaCap,
		)
	} else {
		// 创建新实例

		instance = &inapi.AppInstance{
			Id:   inutil.SeqRandHexString(4, 8),
			Name: req.Spec.Name,
			Operate: &inapi.AppOperate{
				CpuLimit:    cpuLimit,
				MemoryLimit: memoryLimit,
				VolumeLimit: volumeLimit,
				ReplicaCap:  max(1, min(128, req.ReplicaCap)),
			},
			Spec: req.Spec,
		}

		key := inapi.NsAppInstance(config.Config.Zonelet.ZoneId, instance.Id)

		if rs := data.Zonelet.NewWriter(key, instance).
			SetCreateOnly(true).Exec(); !rs.OK() {
			return nil, rs.Error()
		}

		slog.Warn("zonelet app-instance-deploy",
			"instance_id", instance.Id,
			"instance_name", instance.Name,
			"replica_cap", instance.Operate.ReplicaCap,
		)
	}

	return &inapi.AppInstanceDeployResponse{
		Id: instance.Id,
	}, nil
}

func (s *zoneServer) AppInstanceInfo(
	ctx context.Context, req *inapi.AppInstanceInfoRequest,
) (*inapi.AppInstanceInfoResponse, error) {
	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Id == "" {
		return nil, errors.New("id is required")
	}

	var instance inapi.AppInstance

	if rs := data.Zonelet.NewReader(
		inapi.NsAppInstance(config.Config.Zonelet.ZoneId, req.Id)).Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("instance not found")
		}
		return nil, rs.Error()
	} else if err := rs.Item().JsonDecode(&instance); err != nil {
		return nil, err
	}

	return &inapi.AppInstanceInfoResponse{
		Instance: &instance,
	}, nil
}

func (s *zoneServer) AppInstanceList(
	ctx context.Context, req *inapi.AppInstanceListRequest,
) (*inapi.AppInstanceListResponse, error) {
	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	resp := &inapi.AppInstanceListResponse{}

	offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneId, "")

	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
	for _, item := range rs.Items {
		var instance inapi.AppInstance
		if err := item.JsonDecode(&instance); err == nil {
			resp.Items = append(resp.Items, &instance)
		}
	}

	return resp, nil
}

func (s *zoneServer) AppInstanceDelete(
	ctx context.Context, req *inapi.AppInstanceDeleteRequest,
) (*inapi.AppInstanceDeleteResponse, error) {
	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Id == "" {
		return nil, errors.New("id is required")
	}

	key := inapi.NsAppInstance(config.Config.Zonelet.ZoneId, req.Id)

	if rs := data.Zonelet.NewDeleter(key).Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("instance not found")
		}
		return nil, rs.Error()
	}

	slog.Warn("zonelet app-instance-delete",
		"instance_id", req.Id,
	)

	return &inapi.AppInstanceDeleteResponse{}, nil
}
