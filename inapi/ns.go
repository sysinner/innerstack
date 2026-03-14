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

package inapi

import (
	"fmt"

	hauth2 "github.com/hooto/hauth/go/v2"
)

const (
	HostSetupStart   = "start"
	HostSetupStop    = "stop"
	HostSetupDestroy = "destroy"

	// PackageChunkSizeDefault is the default chunk size (2MB)
	PackageChunkSizeDefault = int32(2 * 1024 * 1024)
	// PackageMaxSize is the maximum package size (200MB)
	PackageMaxSize = int64(200 * 1024 * 1024)
)

var (
	AuthPermSysAll = hauth2.NewScopeFilter("sys", "*")
)

func NsGlobalGatewayServiceDomain(name string) []byte {
	return []byte(fmt.Sprintf("v2/service/gateway/domain/%s", name))
}

func NsZoneletInfo(zone string) []byte {
	return []byte(fmt.Sprintf("v2/zone/%s/info", zone))
}

func NsZoneletLeader(zone string) []byte {
	return []byte(fmt.Sprintf("v2/zone/%s/leader", zone))
}

func NsZoneSysAccessKey(zone, kid string) []byte {
	return []byte(fmt.Sprintf("v2/zone/%s/sys-ak/%s", zone, kid))
}

func NsHostInfo(zone, host string) []byte {
	return []byte(fmt.Sprintf("v2/host/%s/info/%s", zone, host))
}

func NsHostStatus(zone, host string) []byte {
	return []byte(fmt.Sprintf("v2/host/%s/status/%s", zone, host))
}

func NsAppInstance(zone, id string) []byte {
	return []byte(fmt.Sprintf("v2/app/instance/%s/%s", zone, id))
}

// PackageId generates a unique package ID from package metadata.
// Format: {name}_{version}_{os}_{arch}
func PackageId(pkg *Package) string {
	if pkg == nil || pkg.Metadata == nil || pkg.Release == nil {
		return ""
	}
	return fmt.Sprintf("%s_%s_%s_%s",
		pkg.Metadata.Name,
		pkg.Release.Version,
		pkg.Release.Os,
		pkg.Release.Arch,
	)
}

// NsPackageUpload returns the KV key for package upload session.
// Key: v2/pkg/upload/{zone}/{pkg_id}
func NsPackageUpload(zone, pkgId string) []byte {
	return []byte(fmt.Sprintf("v2/pkg/upload/%s/%s", zone, pkgId))
}

// NsPackageChunk returns the KV key for a specific chunk.
// Key: v2/pkg/chunk/{zone}/{pkg_id}/{chunk_index}
func NsPackageChunk(zone, pkgId string, chunkIndex int32) []byte {
	return []byte(fmt.Sprintf("v2/pkg/chunk/%s/%s/%d", zone, pkgId, chunkIndex))
}

// NsPackageChunkPrefix returns the KV key prefix for all chunks of a package.
// Key: v2/pkg/chunk/{zone}/{pkg_id}/
func NsPackageChunkPrefix(zone, pkgId string) []byte {
	return []byte(fmt.Sprintf("v2/pkg/chunk/%s/%s/", zone, pkgId))
}

// NsPackageInfo returns the KV key for package metadata (persistent storage).
// Key: v2/pkg/info/{zone}/{pkg_id}
func NsPackageInfo(zone, pkgId string) []byte {
	return []byte(fmt.Sprintf("v2/pkg/info/%s/%s", zone, pkgId))
}
