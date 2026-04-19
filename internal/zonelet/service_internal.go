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

	"google.golang.org/protobuf/proto"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/status"
	"github.com/sysinner/incore/v2/pkg/inauth"
	"github.com/sysinner/incore/v2/pkg/inetutil"
)

type zoneInternalServer struct {
	inapi.UnimplementedZoneInternalServiceServer
}

func NewInternalServer() inapi.ZoneInternalServiceServer {
	return &zoneInternalServer{}
}

func (s *zoneInternalServer) HostStatusUpdate(
	ctx context.Context, req *inapi.HostStatusUpdateRequest,
) (*inapi.HostStatusUpdateResponse, error) {

	if !status.IsZoneletLeader() || !gAppInstanceSet.IsReady() {
		return nil, errors.New("zonelet leader")
	}

	if req.Host == nil ||
		req.Status == nil {
		return nil, errors.New("bad request")
	}

	if !inapi.ObjectIdValid.MatchString(req.Host.Id) {
		return nil, errors.New("invalid host_id")
	}

	if !inauth.AppContext(ctx).Allow(
		fmt.Sprintf("%s:%s", inapi.AuthScope_Host_Write, req.Host.Id)) {
		return nil, errors.New("auth fail")
	}

	resp := &inapi.HostStatusUpdateResponse{}

	key := inapi.NsHostStatus(config.Config.Zonelet.ZoneName, req.Host.Id)

	if rs := data.Zonelet.NewWriter(key, req.Status).Exec(); !rs.OK() {
		return nil, rs.Error()
	}

	status.Zonelet_HostStatusSet.Store(req.Host.Id, req.Status)

	// Populate VPC config in response from host operate data
	if val := gHostSet.Load(req.Host.Id); val != nil {
		host := val.Value.(*inapi.Host)
		if host.Deploy != nil {
			resp.VpcBridgeIp = host.Deploy.VpcBridgeIp
			resp.VpcInstanceCidr = host.Deploy.VpcInstanceCidr
		}
		if req.Host.PeerAddr != "" && req.Host.PeerAddr != host.PeerAddr {
			if _, err := inetutil.ParsePrivateAddress(req.Host.PeerAddr); err != nil {
				slog.Warn("update host peer-address fail", "host_id", req.Host.Id,
					"err", err.Error(),
				)
			} else {
				host.PeerAddr = req.Host.PeerAddr
				if rs := data.Zonelet.NewWriter(
					inapi.NsHostInfo(config.Config.Zonelet.ZoneName, host.Id), host).Exec(); !rs.OK() {
					return nil, rs.Error()
				}
			}
		}
	}

	// Attach zone network map if VPC is active
	if zoneNetMgr.IsReady() {
		resp.ZoneNetworkMap = zoneNetMgr.Map
	}

	slog.Debug("zonelist update host status",
		"host_id", req.Host.Id, "status", req.Status)

	gAppInstanceSet.Iter(func(app *appInstanceEntry) bool {

		for _, rep := range app.Value.Deploy.Replicas {
			if rep.HostId != req.Host.Id {
				continue
			}

			appInstance := proto.Clone(app.Value).(*inapi.AppInstance)

			for _, dep := range appInstance.Deploy.Depends {
				depApp := gAppInstanceSet.Load(dep.InstanceId)
				if depApp == nil || depApp.Value.Deploy == nil ||
					len(depApp.Value.Deploy.Configs) == 0 {
					continue
				}
				dep.Configs = nil
				for _, opt := range depApp.Value.Deploy.Configs {
					dep.Configs = append(dep.Configs,
						proto.Clone(opt).(*inapi.AppDeployConfigItem))
				}
				dep.Replicas = nil
				for _, rep := range depApp.Value.Deploy.Replicas {
					if rep.HostId == "" {
						continue
					}
					peerIp, ok := zoneNetMgr.HostPeerIp(rep.HostId)
					if !ok {
						continue
					}

					rep2 := proto.Clone(rep).(*inapi.AppDeployReplica)
					rep2.HostIpv4 = peerIp

					dep.Replicas = append(dep.Replicas, rep2)
				}
			}

			resp.AppInstances = append(resp.AppInstances, appInstance)
			break
		}

		return true
	})

	return resp, nil
}

func (s *zoneInternalServer) PackageChunk(
	ctx context.Context, req *inapi.PackageChunkRequest,
) (*inapi.PackageChunkResponse, error) {

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if ak := inauth.AppContext(ctx).AccessKey(); !ak.Allow(fmt.Sprintf("%s:%s", inapi.AuthScope_Host_Write, ak.Id)) {
		return nil, errors.New("auth fail")
	}

	if req.Id == "" {
		return nil, errors.New("id is required")
	}

	if req.Index < 0 {
		return nil, errors.New("index must be non-negative")
	}

	// Read package metadata
	infoKey := inapi.NsPackageInfo(req.Id)
	var pkg inapi.Package
	if rs := data.Package.NewReader(infoKey).Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("package not found")
		}
		return nil, rs.Error()
	} else if err := rs.Item().JsonDecode(&pkg); err != nil {
		return nil, fmt.Errorf("failed to decode package info: %w", err)
	}

	// Validate package state is complete
	if pkg.File == nil || pkg.File.State != inapi.PackageFileStateComplete {
		return nil, errors.New("package is not ready for download")
	}

	// Validate chunk index
	totalChunks := calcTotalChunks(pkg.File.Size, pkg.File.ChunkSize)
	if req.Index >= totalChunks {
		return nil, fmt.Errorf("chunk index %d out of range (total: %d)", req.Index, totalChunks)
	}

	// Read chunk data
	chunkKey := inapi.NsPackageFileChunk(req.Id, req.Index)
	var chunk inapi.PackageFileChunk
	if rs := data.Package.NewReader(chunkKey).Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, fmt.Errorf("chunk %d not found", req.Index)
		}
		return nil, rs.Error()
	} else if err := rs.Item().JsonDecode(&chunk); err != nil {
		return nil, fmt.Errorf("failed to decode chunk data: %w", err)
	}

	return &inapi.PackageChunkResponse{
		Chunk: &chunk,
		File:  pkg.File,
	}, nil
}
