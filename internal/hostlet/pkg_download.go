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

package hostlet

import (
	"context"
	"fmt"
	"hash/crc32"
	"log/slog"
	"os"
	"path"
	"runtime"
	"time"

	"github.com/sysinner/innerstack/v2/internal/client"
	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hostapi"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/innerstack/v2/internal/pkgbuild"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// PackagePaths provides path utilities for package storage.
type PackagePaths struct {
	prefix string
}

// NewPackagePaths creates a PackagePaths instance.
func NewPackagePaths(prefix string) *PackagePaths {
	return &PackagePaths{prefix: prefix}
}

// IpkDir returns the base directory for ipk files.
func (p *PackagePaths) IpkDir() string {
	return path.Join(p.prefix, "var", "ipk")
}

// IpkFile returns the path to the ipk archive file.
// Format: {prefix}/var/ipk/{name}/{name}_{version}_{os}_{arch}.ipk
func (p *PackagePaths) IpkFile(pkgName, pkgId string) string {
	return path.Join(p.IpkDir(), pkgName, pkgId+".ipk")
}

// IpkInstallDir returns the base directory for extracted packages.
func (p *PackagePaths) IpkInstallDir() string {
	return path.Join(p.prefix, "var", "ipk_install")
}

// IpkInstallPath returns the path to the extracted package directory.
// Format: {prefix}/var/ipk_install/{name}/{name}_{version}_{os}_{arch}/
func (p *PackagePaths) IpkInstallPath(pkgName, pkgId string) string {
	return path.Join(p.IpkInstallDir(), pkgName, pkgId)
}

// PackageDownload downloads a package from zonelet and extracts it.
// It returns the install path of the extracted package.
// The operation is idempotent - if the package is already downloaded and extracted,
// it returns the existing path.
func PackageDownload(pkgRef *inapi.AppSpecPackage) (string, error) {
	if pkgRef == nil || pkgRef.Name == "" {
		return "", fmt.Errorf("[PackageDownload] invalid package reference")
	}

	zoneLeaderAddr := ""
	if len(config.Config.Server.ZoneHosts) > 0 {
		zoneLeaderAddr = config.Config.Server.ZoneHosts[0]
	}
	if zoneLeaderAddr == "" {
		return "", fmt.Errorf("[PackageDownload] zone leader address not configured")
	}

	// Determine target OS and architecture
	targetOS := runtime.GOOS
	targetArch := runtime.GOOS

	// Get architecture from docker driver info if available
	if info, ok := hoststatus.StatusSet.Load("docker"); ok {
		if driverInfo, ok := info.(*hostapi.DriverInfo); ok {
			if driverInfo.OS != "" {
				targetOS = driverInfo.OS
			}
			if driverInfo.Arch != "" {
				targetArch = driverInfo.Arch
			}
		}
	}

	// Query zonelet for matching package
	conn, err := client.Connect(zoneLeaderAddr, config.Config.Hostlet.AuthKey(), false)
	if err != nil {
		return "", fmt.Errorf("[PackageDownload] failed to connect to zonelet: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	zc := inapi.NewZoneServiceClient(conn)
	zic := inapi.NewZoneInternalServiceClient(conn)

	listResp, err := zc.PackageList(ctx, &inapi.PackageListRequest{
		Name:       pkgRef.Name,
		Version:    pkgRef.Version,
		Os:         targetOS,
		Arch:       targetArch,
		LatestOnly: true,
	})
	if err != nil {
		return "", fmt.Errorf("[PackageDownload] failed to query package list: %w", err)
	}

	if len(listResp.Items) == 0 {
		return "", fmt.Errorf("[PackageDownload] package %s (version: %s, os: %s, arch: %s) not found",
			pkgRef.Name, pkgRef.Version, targetOS, targetArch)
	}

	pkg := listResp.Items[0]
	if pkg.File == nil || pkg.File.State != inapi.PackageFileStateComplete {
		return "", fmt.Errorf("[PackageDownload] package %s is not ready for download", pkgRef.Name)
	}

	// Generate package ID
	pkgId := inapi.PackageId(pkg)
	if pkgId == "" {
		return "", fmt.Errorf("[PackageDownload] failed to generate package ID")
	}

	pkgPaths := NewPackagePaths(config.Prefix)
	pkgName := pkg.Metadata.Name
	ipkFile := pkgPaths.IpkFile(pkgName, pkgId)
	installPath := pkgPaths.IpkInstallPath(pkgName, pkgId)

	// Check if already extracted (idempotent)
	if _, err := os.Stat(installPath); err == nil {
		slog.Info("package already downloaded and extracted",
			"package_id", pkgId,
			"install_path", installPath)
		return installPath, nil
	}

	// Create directories
	if err := os.MkdirAll(path.Dir(ipkFile), 0755); err != nil {
		return "", fmt.Errorf("[PackageDownload] failed to create ipk directory: %w", err)
	}
	if err := os.MkdirAll(path.Dir(installPath), 0755); err != nil {
		return "", fmt.Errorf("[PackageDownload] failed to create install directory: %w", err)
	}

	// Download all chunks and merge into ipk file
	totalChunks := calcTotalChunks(pkg.File.Size, pkg.File.ChunkSize)

	slog.Info("downloading package",
		"name", pkg.Metadata.Name,
		"version", pkg.Release.Version,
		"package_id", pkgId,
		"size", pkg.File.Size,
		"chunks", totalChunks)

	// Create temp file for download
	tmpFile := ipkFile + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("[PackageDownload] failed to create temp file: %w", err)
	}
	defer f.Close()
	defer os.Remove(tmpFile) // Clean up on failure

	for i := int64(0); i < totalChunks; i++ {
		ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)

		chunkResp, err := zic.PackageChunk(ctx2, &inapi.PackageChunkRequest{
			Id:    pkgId,
			Index: i,
		})
		cancel2()

		if err != nil {
			return "", fmt.Errorf("[PackageDownload] failed to download chunk %d: %w", i, err)
		}

		if chunkResp.Chunk == nil || chunkResp.Chunk.Data == nil {
			return "", fmt.Errorf("[PackageDownload] chunk %d has no data", i)
		}

		// Verify CRC32
		if crc32Val := crc32.ChecksumIEEE(chunkResp.Chunk.Data); crc32Val != chunkResp.Chunk.Crc32 {
			return "", fmt.Errorf("[PackageDownload] chunk %d CRC32 mismatch", i)
		}

		// Write chunk to file
		if _, err := f.Write(chunkResp.Chunk.Data); err != nil {
			return "", fmt.Errorf("[PackageDownload] failed to write chunk %d: %w", i, err)
		}
	}

	// Ensure all data is written to disk
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("[PackageDownload] failed to sync file: %w", err)
	}
	f.Close()

	// Rename temp file to final file
	if err := os.Rename(tmpFile, ipkFile); err != nil {
		return "", fmt.Errorf("[PackageDownload] failed to rename ipk file: %w", err)
	}

	slog.Info("package downloaded, extracting",
		"package_id", pkgId,
		"ipk_file", ipkFile)

	// Extract the package archive into the install directory.
	if err := pkgbuild.Extract(ipkFile, installPath); err != nil {
		return "", fmt.Errorf("[PackageDownload] failed to extract package: %w", err)
	}

	slog.Info("package ready",
		"package_id", pkgId,
		"install_path", installPath)

	return installPath, nil
}

// calcTotalChunks calculates total chunks from file size and chunk size
func calcTotalChunks(totalSize, chunkSize int64) int64 {
	return (totalSize + chunkSize - 1) / chunkSize
}

// EnsurePackages downloads and prepares all packages for an app.
// It returns a map of package name -> install path.
// Duplicate packages (same name and version) are skipped.
func EnsurePackages(app *inapi.AppInstance) (map[string]string, error) {
	if app == nil || app.Spec == nil {
		return nil, nil
	}

	packages := app.Spec.Packages
	if len(packages) == 0 {
		return nil, nil
	}

	result := make(map[string]string)
	seen := make(map[string]bool) // key: "name:version" for deduplication

	for _, pkgRef := range packages {
		if pkgRef == nil || pkgRef.Name == "" {
			continue
		}

		// Deduplicate by name:version
		key := pkgRef.Name + ":" + pkgRef.Version
		if seen[key] {
			continue
		}
		seen[key] = true

		installPath, err := PackageDownload(pkgRef)
		if err != nil {
			slog.Warn("failed to download package",
				"package", pkgRef.Name,
				"version", pkgRef.Version,
				"error", err)
			return nil, fmt.Errorf("[EnsurePackages] failed to prepare package %s: %w", pkgRef.Name, err)
		}

		result[pkgRef.Name] = installPath
		slog.Info("package ready for container",
			"package", pkgRef.Name,
			"install_path", installPath)
	}

	return result, nil
}
