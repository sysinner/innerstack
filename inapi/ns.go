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
	"strings"

	hauth2 "github.com/hooto/hauth/go/v2"
)

const (
	HostSetupStart   = "start"
	HostSetupStop    = "stop"
	HostSetupDestroy = "destroy"

	// PackageFileChunkSizeDefault is the default chunk size (1MB)
	PackageFileChunkSizeDefault = int64(1 << 20)
	// PackageMaxSize is the maximum package size (200MB)
	PackageMaxSize = int64(200 << 20)
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

func NsZoneletAccessKey(zone, kid string) []byte {
	return []byte(fmt.Sprintf("v2/zone/%s/ak/%s", zone, kid))
}

// NsZoneletNetworkIPAM returns the KV key for persisting IPAM state.
func NsZoneletNetworkIPAM(zone string) []byte {
	return []byte(fmt.Sprintf("v2/zone/%s/network/ipam", zone))
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
	return strings.ToLower(fmt.Sprintf("%s_%s_%s_%s",
		pkg.Metadata.Name,
		pkg.Release.Version,
		pkg.Release.Os,
		pkg.Release.Arch,
	))
}

// NsPackageInfo returns the KV key for package metadata.
// Key: v2/pkg/info/{pkg_id}
func NsPackageInfo(pkgId string) []byte {
	return []byte(fmt.Sprintf("v2/pkg/info/%s", pkgId))
}

// NsPackageFileChunk returns the KV key for a specific chunk.
// Key: v2/pkg/chunk/{pkg_id}/{chunk_index:08d}
func NsPackageFileChunk(pkgId string, chunkIndex int64) []byte {
	return []byte(fmt.Sprintf("v2/pkg/chunk/%s/%08d", pkgId, chunkIndex))
}

// NsPackageFileChunkPrefix returns the KV key prefix for all chunks of a package.
// Key: v2/pkg/chunk/{pkg_id}/
func NsPackageFileChunkPrefix(pkgId string) []byte {
	return []byte(fmt.Sprintf("v2/pkg/chunk/%s/", pkgId))
}
