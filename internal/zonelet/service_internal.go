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

package zonelet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/protobuf/proto"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
	"github.com/sysinner/innerstack/v2/pkg/inetutil"
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
		return nil, errors.New("zonelet leader not ready")
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
				depApp := gAppInstanceSet.Load(dep.InstanceName)
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

					rep2 := proto.Clone(rep).(*inapi.AppDeployReplica)

					// Try VPC peer IP first, fallback to host LAN address
					if peerIp, ok := zoneNetMgr.HostPeerIp(rep.HostId); ok {
						rep2.HostIpv4 = peerIp
					} else if kvDepHost := gHostSet.Load(rep.HostId); kvDepHost != nil {
						if depHost, ok := kvDepHost.Value.(*inapi.Host); ok && depHost.PeerAddr != "" {
							if ip, err := inetutil.ParsePrivateAddress(depHost.PeerAddr); err == nil {
								rep2.HostIpv4 = inetutil.IP4ToString(ip)
							}
						}
					}

					dep.Replicas = append(dep.Replicas, rep2)
				}
			}

			resp.AppInstances = append(resp.AppInstances, appInstance)
			break
		}

		return true
	})

	// Merge hostlet-reported per-replica stage progress into AppDeploy.Stages.
	if len(req.ReplicaStages) > 0 {
		mergeHostReplicaStages(req.ReplicaStages)
	}

	return resp, nil
}

// mergeHostReplicaStages merges hostlet-reported host-side stage nodes into
// the matching per-replica stage subtree of each AppInstance. Zone-side
// children (schedule, ipam_alloc, ...) are preserved; host-side children are
// replaced by the report. The instance is flushed only when the subtree
// actually changed, to avoid a kvgo write on every 3-second status poll.
func mergeHostReplicaStages(reports []*inapi.HostReplicaStageReport) {
	for _, rpt := range reports {
		if rpt == nil || rpt.InstanceName == "" {
			continue
		}
		app := gAppInstanceSet.Load(rpt.InstanceName)
		if app == nil || app.Value == nil || app.Value.Deploy == nil {
			continue
		}

		// Skip soft-deleted instances: a Flush here writes without SetTTL and
		// would clear the soft-delete TTL. The hostlet does not report stages
		// for deleted instances either, so this is defensive.
		if app.Value.Deploy.Action == inapi.OpActionDelete {
			continue
		}

		repNode := app.Value.Deploy.StagesRoot().ReplicaStage(rpt.ReplicaId)

		before, errBefore := proto.Marshal(repNode)

		// Drop existing relayed (host-side + inagent) children; keep zone-side.
		repNode.PruneChildren(func(name string) bool {
			_, isRelayed := inapi.AppDeployStageRelayedNames[name]
			return !isRelayed
		})
		// Apply reported relayed (host-side/inagent) stages. Discard stages
		// based on a stale deploy revision (the meta they were recorded
		// against has since changed).
		curRev := app.Value.Deploy.Revision
		for _, s := range rpt.Stages {
			if s == nil || s.Name == "" {
				continue
			}
			if s.Revision > 0 && curRev > 0 && s.Revision < curRev {
				continue
			}
			dst := repNode.Child(s.Name, s.Owner)
			dst.State = s.State
			dst.Attempt = s.Attempt
			dst.Created = s.Created
			dst.Finished = s.Finished
			dst.Message = s.Message
			dst.Revision = s.Revision
		}

		after, errAfter := proto.Marshal(repNode)
		if errBefore != nil || errAfter != nil || bytes.Equal(before, after) {
			continue
		}

		if err := gAppInstanceSet.Flush(app); err != nil {
			slog.Warn("merge host replica stages flush failed",
				"instance_name", rpt.InstanceName,
				"replica_id", rpt.ReplicaId,
				"err", err.Error())
		}
	}
}

func (s *zoneInternalServer) PackageChunk(
	ctx context.Context, req *inapi.PackageChunkRequest,
) (*inapi.PackageChunkResponse, error) {

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if ak := inauth.AppContext(ctx).AccessKey(); !ak.Allow(
		fmt.Sprintf("%s:%s", inapi.AuthScope_Host_Write, ak.Id)) {
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
