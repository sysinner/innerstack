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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/internal/pkgbuild"
	"github.com/sysinner/incore/v2/internal/status"
)

// uploadMutex provides per-package mutex for concurrent upload protection
var uploadMutex sync.Map // map[string]*sync.Mutex

// calcTotalChunks calculates total chunks from file size and chunk size
func calcTotalChunks(totalSize, chunkSize int64) int64 {
	return (totalSize + chunkSize - 1) / chunkSize
}

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
		} else {
			slog.Warn("app decode err "+err.Error(), "value", string(item.Value))
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

	// For new instances, spec is required
	if req.Id == "" && req.Spec == nil {
		return nil, errors.New("spec is required for new instance")
	}

	var cpuLimit, memoryLimit, volumeLimit int64

	if req.Spec != nil {
		if req.Spec.Name == "" {
			return nil, errors.New("spec.name is required")
		}

		if req.Spec.Resources == nil {
			return nil, errors.New("spec.resources is required")
		}

		if req.Spec.Resources.CpuLimit != "" {
			if v, err := inutil.ParseCPUs(req.Spec.Resources.CpuLimit); err != nil {
				return nil, fmt.Errorf("invalid cpu_limit: %w", err)
			} else {
				cpuLimit = v
			}
		}

		if req.Spec.Resources.MemoryLimit != "" {
			if v, err := inutil.ParseBytes(req.Spec.Resources.MemoryLimit); err != nil {
				return nil, fmt.Errorf("invalid memory_limit: %w", err)
			} else {
				memoryLimit = v
			}
		}

		if req.Spec.Resources.VolumeLimit != "" {
			if v, err := inutil.ParseBytes(req.Spec.Resources.VolumeLimit); err != nil {
				return nil, fmt.Errorf("invalid volume_limit: %w", err)
			} else {
				volumeLimit = v
			}
		}

		if cpuLimit < inapi.CPUMin || cpuLimit > inapi.CPUMax {
			return nil, fmt.Errorf("spec.cpu_limit must be between %d and %d",
				inapi.CPUMin, inapi.CPUMax)
		}

		if memoryLimit < inapi.MemoryMin || memoryLimit > inapi.MemoryMax {
			return nil, fmt.Errorf("spec.memory_limit must be between %d and %d",
				inapi.MemoryMin, inapi.MemoryMax)
		}

		if volumeLimit < inapi.VolumeMin || volumeLimit > inapi.VolumeMax {
			return nil, fmt.Errorf("spec.volume_limit must be between %d and %d",
				inapi.VolumeMin, inapi.VolumeMax)
		}
	}

	var instance *inapi.AppInstance

	if req.Id != "" {
		// Update existing instance
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

		instance = &existingInstance

		// Update spec only if provided
		if req.Spec != nil {
			instance.Spec = req.Spec
		}

		if instance.Operate == nil {
			instance.Operate = &inapi.AppOperate{}
		}

		// Update resource limits only if spec is provided
		if req.Spec != nil {
			instance.Operate.CpuLimit = cpuLimit
			instance.Operate.MemoryLimit = memoryLimit
			instance.Operate.VolumeLimit = volumeLimit
		}

		if req.ReplicaCap > 0 {
			instance.Operate.ReplicaCap = min(128, req.ReplicaCap)
		}

		// Update operate options if provided
		if req.Operate != nil && len(req.Operate.Options) > 0 {
			instance.Operate.Options = req.Operate.Options
		}

		// Update operate action if provided
		if req.Operate != nil && req.Operate.Action != "" {
			instance.Operate.Action = req.Operate.Action
		}

		if rs := data.Zonelet.NewWriter(key, instance).Exec(); !rs.OK() {
			return nil, rs.Error()
		}

		slog.Warn("zonelet app-instance-update",
			"instance_id", req.Id,
			"instance_name", instance.Name,
			"replica_cap", instance.Operate.ReplicaCap,
			"action", instance.Operate.Action,
		)
	} else {
		// 创建新实例

		operate := &inapi.AppOperate{
			Action:      inapi.OpActionStart,
			CpuLimit:    cpuLimit,
			MemoryLimit: memoryLimit,
			VolumeLimit: volumeLimit,
			ReplicaCap:  max(1, min(128, req.ReplicaCap)),
		}

		// Set operate options if provided
		if req.Operate != nil && len(req.Operate.Options) > 0 {
			operate.Options = req.Operate.Options
		}

		instance = &inapi.AppInstance{
			Id:      inutil.SeqRandHexString(4, 8),
			Name:    req.Spec.Name,
			Operate: operate,
			Spec:    req.Spec,
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

func (s *zoneServer) PackagePush(
	ctx context.Context, req *inapi.PackagePushRequest,
) (*inapi.PackagePushResponse, error) {

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	// Validate request
	if req.Id == "" {
		return nil, errors.New("id is required")
	}
	if req.Chunk == nil {
		return nil, errors.New("chunk is required")
	}
	if req.Chunk.Data == nil {
		return nil, errors.New("chunk data is required")
	}

	// Validate chunk size
	chunkSize := int64(len(req.Chunk.Data))
	if chunkSize > inapi.PackageFileChunkSizeDefault {
		return nil, fmt.Errorf("chunk size %d exceeds maximum %d", chunkSize, inapi.PackageFileChunkSizeDefault)
	}

	// Get or create per-package mutex
	muI, _ := uploadMutex.LoadOrStore(req.Id, &sync.Mutex{})
	mu := muI.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	var (
		infoKey = inapi.NsPackageInfo(req.Id)

		// Load or create upload session
		pkg inapi.Package
	)

	if rs := data.Package.NewReader(infoKey).Exec(); rs.OK() {
		if err := rs.Item().JsonDecode(&pkg); err != nil {
			return nil, fmt.Errorf("failed to decode upload info: %w", err)
		}
	} else if rs.NotFound() {
		// Create new session
		if req.Package == nil {
			return nil, errors.New("package is required for first chunk")
		}
		if req.TotalSize <= 0 {
			return nil, errors.New("total_size is required for first chunk")
		}
		if req.TotalSize > inapi.PackageMaxSize {
			return nil, fmt.Errorf("package size %d exceeds maximum %d", req.TotalSize, inapi.PackageMaxSize)
		}

		// Validate package metadata
		if req.Package.Metadata == nil || req.Package.Metadata.Name == "" {
			return nil, errors.New("package metadata is required")
		}

		if req.Package.Release == nil {
			return nil, errors.New("package release info is required")
		}

		// Validate package metadata using pkgbuild
		if err := pkgbuild.MetadataValidate(req.Package.Metadata); err != nil {
			return nil, err
		}

		// Validate package release info using pkgbuild
		if err := pkgbuild.ReleaseValidate(req.Package.Release); err != nil {
			return nil, err
		}

		// Store total size in Release.Size
		req.Package.Release.Size = req.TotalSize

		pkg = inapi.Package{
			Metadata: req.Package.Metadata,
			Release:  req.Package.Release,
			File: &inapi.PackageFile{
				State:          inapi.PackageFileStateUploading,
				ChunkSize:      inapi.PackageFileChunkSizeDefault,
				UploadedChunks: []int64{},
				Created:        time.Now().Unix(),
				Updated:        time.Now().Unix(),
			},
		}

	} else {
		return nil, rs.Error()
	}

	// Calculate total chunks dynamically
	totalChunks := calcTotalChunks(pkg.Release.Size, pkg.File.ChunkSize)

	// Check if already complete
	if pkg.File.State == inapi.PackageFileStateComplete {
		if !req.Overwrite {
			return &inapi.PackagePushResponse{
				Id:   req.Id,
				File: pkg.File,
			}, nil
		}
		// Overwrite: reset session
		pkg.File.State = inapi.PackageFileStateUploading
		pkg.File.UploadedChunks = []int64{}
		pkg.Release.Checksum = ""
		pkg.File.Updated = time.Now().Unix()
	}

	// Validate chunk index
	if req.Chunk.Index < 0 || req.Chunk.Index >= totalChunks {
		return nil, fmt.Errorf("invalid chunk index %d (total: %d)", req.Chunk.Index, totalChunks)
	}

	// Validate chunk size
	// - For non-last chunks: size must equal ChunkSize
	// - For last chunk: size can be <= ChunkSize (depends on TotalSize % ChunkSize)
	if req.Chunk.Index != totalChunks-1 {
		if int64(len(req.Chunk.Data)) != pkg.File.ChunkSize {
			return nil, fmt.Errorf("invalid chunk size %d, expected %d", len(req.Chunk.Data), pkg.File.ChunkSize)
		}
	} else {
		expectedLastChunkSize := pkg.Release.Size % pkg.File.ChunkSize
		if expectedLastChunkSize == 0 {
			expectedLastChunkSize = pkg.File.ChunkSize
		}
		if int64(len(req.Chunk.Data)) != expectedLastChunkSize {
			return nil, fmt.Errorf("invalid last chunk size %d, expected %d",
				len(req.Chunk.Data), expectedLastChunkSize)
		}
	}

	if crc32Val := crc32.ChecksumIEEE(req.Chunk.Data); crc32Val != req.Chunk.Crc32 {
		return nil, fmt.Errorf("invalid chunk data checksum crc32")
	}

	// Check if chunk already uploaded (idempotent)
	if slices.Contains(pkg.File.UploadedChunks, req.Chunk.Index) {
		return &inapi.PackagePushResponse{
			Id:   req.Id,
			File: pkg.File,
		}, nil
	}

	// Store chunk (Offset and Size are calculated from Index and len(Data))
	chunkKey := inapi.NsPackageFileChunk(req.Id, req.Chunk.Index)
	chunk := &inapi.PackageFileChunk{
		Index:    req.Chunk.Index,
		Crc32:    req.Chunk.Crc32,
		Data:     req.Chunk.Data,
		Uploaded: time.Now().Unix(),
	}

	if rs := data.Package.NewWriter(chunkKey, chunk).Exec(); !rs.OK() {
		return nil, fmt.Errorf("failed to store chunk: %w", rs.Error())
	}

	// Update upload progress
	pkg.File.UploadedChunks = append(pkg.File.UploadedChunks, req.Chunk.Index)
	sort.Slice(pkg.File.UploadedChunks, func(i, j int) bool {
		return pkg.File.UploadedChunks[i] < pkg.File.UploadedChunks[j]
	})
	pkg.File.Updated = time.Now().Unix()

	// Check if upload complete
	complete := int64(len(pkg.File.UploadedChunks)) == totalChunks

	if complete {
		// Finalize package
		checksum, err := s.finalizePackage(req.Id, totalChunks)
		if err != nil {
			pkg.File.State = inapi.PackageFileStateFailed
			data.Package.NewWriter(infoKey, &pkg).Exec()
			return nil, fmt.Errorf("failed to finalize package: %w", err)
		}

		pkg.File.State = inapi.PackageFileStateComplete
		pkg.Release.Checksum = checksum
		// pkg.Release.Size is already set during first chunk upload

		pkg.File.UploadedChunks = nil

		slog.Warn("zonelet package-push complete",
			"package_id", req.Id,
			"size", pkg.Release.Size,
			"checksum", checksum,
		)
	}

	if rs := data.Package.NewWriter(infoKey, &pkg).Exec(); !rs.OK() {
		return nil, fmt.Errorf("failed to store package info: %w", rs.Error())
	}

	return &inapi.PackagePushResponse{
		Id:   req.Id,
		File: pkg.File,
	}, nil
}

func (s *zoneServer) PackageList(
	ctx context.Context, req *inapi.PackageListRequest,
) (*inapi.PackageListResponse, error) {

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	resp := &inapi.PackageListResponse{}

	offset := inapi.NsPackageInfo("")

	rs := data.Package.NewRanger(offset, append(offset, 0xff)).Exec()
	for _, item := range rs.Items {
		var pkg inapi.Package
		if err := item.JsonDecode(&pkg); err == nil {
			if req.All || pkg.File.State == inapi.PackageFileStateComplete {
				resp.Packages = append(resp.Packages, &pkg)
			}
		}
	}

	return resp, nil
}

// finalizePackage assembles all chunks and calculates SHA-256 checksum
func (s *zoneServer) finalizePackage(pkgId string, totalChunks int64) (string, error) {
	hash := sha256.New()

	// Read chunks in order and compute hash
	for i := int64(0); i < totalChunks; i++ {
		chunkKey := inapi.NsPackageFileChunk(pkgId, i)

		var chunk inapi.PackageFileChunk
		if rs := data.Package.NewReader(chunkKey).Exec(); !rs.OK() {
			if rs.NotFound() {
				return "", fmt.Errorf("chunk %d not found", i)
			}
			return "", fmt.Errorf("failed to read chunk %d: %w", i, rs.Error())
		} else if err := rs.Item().JsonDecode(&chunk); err != nil {
			return "", fmt.Errorf("failed to decode chunk %d: %w", i, err)
		}

		if _, err := hash.Write(chunk.Data); err != nil {
			return "", fmt.Errorf("failed to hash chunk %d: %w", i, err)
		}
	}

	checksum := hex.EncodeToString(hash.Sum(nil))
	return checksum, nil
}
