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
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/pkgbuild"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
	"golang.org/x/mod/semver"
)

func (s *zoneServer) PackagePush(
	ctx context.Context, req *inapi.PackagePushRequest,
) (*inapi.PackagePushResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_Package_Write) {
		return nil, errors.New("auth fail: missing pkg:rw scope")
	}

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

	// chunk index == 0 必须首次 reqeust
	if req.Chunk.Index == 0 {
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

		// File.Size is the whole IPK file size (set by client)
		// Used for chunk count calculation and validation
		if req.Package.File == nil || req.Package.File.Size <= 0 {
			return nil, errors.New("package file size is required")
		}

		if req.ChunkSize != inapi.PackageFileChunkSizeDefault {
			return nil, fmt.Errorf("invalid chunk size %d ", req.ChunkSize)
		}
	}

	// Get or create per-package mutex
	muI, _ := uploadMutex.LoadOrStore(req.Id, &sync.Mutex{})
	mu := muI.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	var (
		infoKey = inapi.NsPackageInfo(req.Id)

		pkg inapi.Package
	)

	if rs := data.Package.NewReader(infoKey).Exec(); rs.OK() {
		if err := rs.Item().JsonDecode(&pkg); err != nil {
			return nil, fmt.Errorf("failed to decode upload info: %w", err)
		}
	} else if rs.NotFound() {

		if req.Chunk.Index != 0 {
			return nil, fmt.Errorf("package meta not found")
		}

		pkg = inapi.Package{
			Metadata: req.Package.Metadata,
			Release:  req.Package.Release,
			File: &inapi.PackageFile{
				State:          inapi.PackageFileStateUploading,
				Size:           req.Package.File.Size,
				ChunkSize:      req.ChunkSize,
				UploadedChunks: []int64{},
				Created:        time.Now().Unix(),
			},
		}

	} else {
		return nil, rs.Error()
	}

	// Check if already complete
	if pkg.File.State == inapi.PackageFileStateComplete {
		if !req.Overwrite {
			return &inapi.PackagePushResponse{
				Id:   req.Id,
				File: pkg.File,
			}, nil
		}

		if req.Chunk.Index != 0 {
			return nil, fmt.Errorf("package meta not found")
		}

		// Delete old chunks
		oldTotalChunks := calcTotalChunks(pkg.File.Size, pkg.File.ChunkSize)
		for i := int64(0); i < oldTotalChunks; i++ {
			chunkKey := inapi.NsPackageFileChunk(req.Id, i)
			data.Package.NewDeleter(chunkKey).Exec()
		}

		// Replace with new package info
		pkg = inapi.Package{
			Metadata: req.Package.Metadata,
			Release:  req.Package.Release,
			File: &inapi.PackageFile{
				State:          inapi.PackageFileStateUploading,
				Size:           req.Package.File.Size,
				ChunkSize:      req.ChunkSize,
				UploadedChunks: []int64{},
				Created:        time.Now().Unix(),
			},
		}
	}

	// Calculate total chunks dynamically using File.Size (entire IPK file size)
	totalChunks := calcTotalChunks(pkg.File.Size, pkg.File.ChunkSize)

	// Validate chunk index
	if req.Chunk.Index < 0 || req.Chunk.Index >= totalChunks {
		return nil, fmt.Errorf("invalid chunk index %d (total: %d)", req.Chunk.Index, totalChunks)
	}

	// Validate chunk size
	// - For non-last chunks: size must equal ChunkSize
	// - For last chunk: size can be <= ChunkSize (depends on File.Size % ChunkSize)
	if req.Chunk.Index != totalChunks-1 {
		if int64(len(req.Chunk.Data)) != pkg.File.ChunkSize {
			return nil, fmt.Errorf("invalid chunk size %d, expected %d", len(req.Chunk.Data), pkg.File.ChunkSize)
		}
	} else {
		expectedLastChunkSize := pkg.File.Size % pkg.File.ChunkSize
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

	// Check if upload complete
	complete := int64(len(pkg.File.UploadedChunks)) == totalChunks

	if complete {

		pkg.File.State = inapi.PackageFileStateComplete

		pkg.File.UploadedChunks = nil

		// Clean up upload mutex since upload is complete
		uploadMutex.Delete(req.Id)

		slog.Warn("zonelet package-push complete",
			"package_id", req.Id,
			"size", pkg.File.Size,
		)
	}

	pkg.File.Updated = time.Now().Unix()

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

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_Package_Read) {
		return nil, errors.New("auth fail: missing pkg:ro scope")
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	resp := &inapi.PackageListResponse{}

	offset := inapi.NsPackageInfo("")

	rs := data.Package.NewRanger(offset, append(offset, 0xff)).SetLimit(1000).Exec()
	for _, item := range rs.Items {
		var pkg inapi.Package
		if err := item.JsonDecode(&pkg); err != nil {
			continue
		}
		// Filter by upload status
		if !req.All && pkg.File.State != inapi.PackageFileStateComplete {
			continue
		}

		// Filter by name (exact match)
		if req.Name != "" && (pkg.Metadata == nil || pkg.Metadata.Name != req.Name) {
			continue
		}

		// Filter by version (fuzzy match)
		if req.Version != "" {
			if pkg.Release == nil || !versionMatch(req.Version, pkg.Release.Version) {
				continue
			}
		}

		// Filter by OS (exact match)
		if req.Os != "" && (pkg.Release == nil || pkg.Release.Os != req.Os) {
			continue
		}

		// Filter by arch (exact match)
		if req.Arch != "" && (pkg.Release == nil || pkg.Release.Arch != req.Arch) {
			continue
		}

		resp.Items = append(resp.Items, &pkg)
	}

	// If latest_only is true, keep only the latest version for each (name, os, arch) combination
	if req.LatestOnly && len(resp.Items) > 0 {
		resp.Items = filterLatestPackages(resp.Items)
	}

	return resp, nil
}

func (s *zoneServer) PackageDelete(
	ctx context.Context, req *inapi.PackageDeleteRequest,
) (*inapi.PackageDeleteResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_Package_Write) {
		return nil, errors.New("auth fail: missing pkg:rw scope")
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Id == "" {
		return nil, errors.New("id is required")
	}

	// Check if there's an ongoing upload for this package
	if _, ok := uploadMutex.Load(req.Id); ok {
		return nil, errors.New("package is being uploaded, please retry later")
	}

	infoKey := inapi.NsPackageInfo(req.Id)

	// Check if package exists
	var pkg inapi.Package
	if rs := data.Package.NewReader(infoKey).Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("package not found")
		}
		return nil, rs.Error()
	} else if err := rs.Item().JsonDecode(&pkg); err != nil {
		return nil, fmt.Errorf("failed to decode package info: %w", err)
	}

	// Calculate total chunks
	var totalChunks int64
	if pkg.File != nil && pkg.File.ChunkSize > 0 && pkg.File.Size > 0 {
		totalChunks = calcTotalChunks(pkg.File.Size, pkg.File.ChunkSize)
	}

	// Delete all chunks
	chunksDeleted := int32(0)
	for i := int64(0); i < totalChunks; i++ {
		chunkKey := inapi.NsPackageFileChunk(req.Id, i)
		if rs := data.Package.NewDeleter(chunkKey).Exec(); rs.OK() {
			chunksDeleted++
		}
	}

	// Delete package info
	if rs := data.Package.NewDeleter(infoKey).Exec(); !rs.OK() {
		if !rs.NotFound() {
			return nil, fmt.Errorf("failed to delete package info: %w", rs.Error())
		}
	}

	slog.Warn("zonelet package-delete",
		"package_id", req.Id,
		"chunks_deleted", chunksDeleted,
	)

	return &inapi.PackageDeleteResponse{
		Id:            req.Id,
		ChunksDeleted: chunksDeleted,
	}, nil
}

// validatePackageDependencies checks that all packages referenced in the app
// spec exist in the package store and have been fully uploaded (state=complete).
// Version matching uses the same fuzzy prefix logic as versionMatch.
func (s *zoneServer) validatePackageDependencies(packages []*inapi.AppSpecPackage) error {
	if len(packages) == 0 {
		return nil
	}

	// Load all completed packages from the store, indexed by name for O(1) lookup.
	// Multiple versions of the same package name are collected in the slice.
	offset := inapi.NsPackageInfo("")
	rs := data.Package.NewRanger(offset, append(offset, 0xff)).Exec()

	available := make(map[string][]string, len(rs.Items))

	for _, item := range rs.Items {
		var pkg inapi.Package
		if err := item.JsonDecode(&pkg); err != nil {
			continue
		}
		if pkg.File == nil || pkg.File.State != inapi.PackageFileStateComplete {
			continue
		}
		if pkg.Metadata == nil || pkg.Metadata.Name == "" || pkg.Release == nil {
			continue
		}
		available[pkg.Metadata.Name] = append(available[pkg.Metadata.Name], pkg.Release.Version)
	}

	for _, dep := range packages {
		if dep == nil {
			continue
		}
		if dep.Name == "" {
			return errors.New("package name is required")
		}

		versions, exists := available[dep.Name]
		if !exists {
			if dep.Version != "" {
				return fmt.Errorf("package %q version %q not found or not fully uploaded",
					dep.Name, dep.Version)
			}
			return fmt.Errorf("package %q not found or not fully uploaded", dep.Name)
		}

		if dep.Version != "" {
			found := false
			for _, v := range versions {
				if versionMatch(dep.Version, v) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("package %q version %q not found or not fully uploaded",
					dep.Name, dep.Version)
			}
		}
	}

	return nil
}

// versionMatch checks if a version matches the filter with fuzzy matching support.
// If filter has 2 parts (e.g., "2.0"), it matches any 2.0.x version.
// If filter has 3 parts (e.g., "2.0.0"), it matches exactly.
func versionMatch(filter, version string) bool {
	if filter == "" {
		return true
	}
	if version == "" {
		return false
	}

	filterParts := strings.Split(filter, ".")
	versionParts := strings.Split(version, ".")

	// Exact match if filter has 3 or more parts
	if len(filterParts) >= 3 {
		return filter == version
	}

	// Fuzzy match for 1 or 2 part filters (e.g., "2" or "2.0")
	// Match the prefix parts exactly
	for i := 0; i < len(filterParts); i++ {
		if i >= len(versionParts) {
			return false
		}
		if filterParts[i] != versionParts[i] {
			return false
		}
	}

	return true
}

// filterLatestPackages returns only the latest version for each (name, os, arch) combination.
// It uses golang.org/x/mod/semver for semantic version comparison.
func filterLatestPackages(packages []*inapi.Package) []*inapi.Package {
	// Group packages by (name, os, arch)
	type groupKey struct {
		name string
		os   string
		arch string
	}

	slog.Info("filterLatestPackages", "num", len(packages))

	latestMap := make(map[groupKey]*inapi.Package)

	for _, pkg := range packages {
		if pkg.Metadata == nil || pkg.Release == nil {
			continue
		}

		key := groupKey{
			name: pkg.Metadata.Name,
			os:   pkg.Release.Os,
			arch: pkg.Release.Arch,
		}

		existing, exists := latestMap[key]
		if !exists {
			latestMap[key] = pkg
			continue
		}

		// Compare versions using semver, keep the newer one
		if semverCompare(pkg.Release.Version, existing.Release.Version) > 0 {
			latestMap[key] = pkg
		}
	}

	// Convert map to slice
	result := make([]*inapi.Package, 0, len(latestMap))
	for _, pkg := range latestMap {
		result = append(result, pkg)
	}

	// Sort by name for consistent output
	sort.Slice(result, func(i, j int) bool {
		if result[i].Metadata.Name != result[j].Metadata.Name {
			return result[i].Metadata.Name < result[j].Metadata.Name
		}
		if result[i].Release.Os != result[j].Release.Os {
			return result[i].Release.Os < result[j].Release.Os
		}
		return result[i].Release.Arch < result[j].Release.Arch
	})

	return result
}

// releaseIterRegexp matches a trailing package release-revision suffix
// "-r<digits>" (e.g. "-r1", "-r2"). The "r" must be immediately followed by
// digits, so release-candidate suffixes such as "-rc1" are NOT matched and
// fall through to the standard SemVer pre-release ordering instead.
var releaseIterRegexp = regexp.MustCompile(`(-r\d+)$`)

// semverCompare compares two version strings.
//
// The base version (X.Y.Z) is compared with golang.org/x/mod/semver, which
// follows the strict SemVer spec.
//
// Two kinds of suffix are distinguished:
//
//   - "-rN": a formal release revision. It is newer than the bare version and
//     a higher N is newer still; a bare version is equivalent to "-r0".
//     Example ordering: 1.26.4 < 1.26.4-r1 < 1.26.4-r2.
//
//   - "-rcN" (and any other non "-r<digits>" suffix): treated as a standard
//     SemVer pre-release, which is older than the bare release.
//     Example ordering: 1.26.4-rc1 < 1.26.4.
//
// Return values:
//
//	-1 if v1 < v2
//	 0 if v1 == v2
//	 1 if v1 > v2
func semverCompare(v1, v2 string) int {

	// splitVersion separates a version string into its base SemVer part and
	// a release-revision number derived from a trailing "-rN" suffix.
	//
	// "-rcN" and other non "-r<digits>" suffixes are left untouched in the
	// base so they are handled by the standard SemVer pre-release ordering.
	splitVersion := func(v string) (base string, iter int) {
		if v == "" {
			return "v0.0.0", 0
		}
		if !strings.HasPrefix(v, "v") {
			v = "v" + v
		}
		m := releaseIterRegexp.FindStringSubmatchIndex(v)
		if m == nil {
			return v, 0
		}
		// m[0]:m[1] bounds the full match "-rN" at the tail of v.
		n := 0
		for _, c := range v[m[0]+2 : m[1]] {
			n = n*10 + int(c-'0')
		}
		return v[:m[0]], n
	}

	base1, iter1 := splitVersion(v1)
	base2, iter2 := splitVersion(v2)

	if c := semver.Compare(base1, base2); c != 0 {
		return c
	}
	switch {
	case iter1 < iter2:
		return -1
	case iter1 > iter2:
		return 1
	default:
		return 0
	}
}
