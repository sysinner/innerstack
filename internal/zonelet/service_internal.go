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

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/status"
	"github.com/sysinner/incore/v2/pkg/inauth"
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

	if !inauth.AppContext(ctx).Allow(fmt.Sprintf("%s:%s", inapi.AuthScope_Host_Write, req.Host.Id)) {
		return nil, errors.New("auth fail")
	}

	resp := &inapi.HostStatusUpdateResponse{}

	key := inapi.NsHostStatus(config.Config.Zonelet.ZoneName,
		req.Host.Id)

	if rs := data.Zonelet.NewWriter(key, req.Status).Exec(); !rs.OK() {
		return nil, rs.Error()
	}

	status.Zonelet_HostStatusSet.Store(req.Host.Id, req.Status)

	slog.Debug("zonelist update host status", "host_id", req.Host.Id, "status", req.Status)

	// Query app instances associated with this host_id
	offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, "")
	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
	for _, item := range rs.Items {
		var instance inapi.AppInstance
		if err := item.JsonDecode(&instance); err == nil {
			if instance.Deploy != nil {
				for _, rep := range instance.Deploy.Replicas {
					if rep.HostId == req.Host.Id {
						resp.AppInstances = append(resp.AppInstances, &instance)
						break
					}
				}
			}
		} else {
			slog.Warn(fmt.Sprintf("app decode err %s, value %s", err.Error(), string(item.Value)))
		}
	}

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
