// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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

package audit

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// detailMaxLen bounds the detail JSON; over-limit drops trailing fields
// (truncated:true).
const detailMaxLen = 512

// Extractor derives target and whitelisted detail from a finished RPC call
// (resp and err available). Returning audit=false declines the record
// (PackagePush intermediate chunks). Only whitelisted fields may enter
// detail; the raw request proto is never serialized (secrets stay out).
type Extractor func(req, resp any, err error) (
	targetType, targetId string, detail map[string]any, audit bool)

// extractors is the audited-method set; adding a mutating RPC adds a row.
var extractors = map[string]Extractor{
	"/inapi.ZoneService/ZoneInit":          extractZoneInit,
	"/inapi.ZoneService/ZoneSet":           extractZoneSet,
	"/inapi.ZoneService/HostJoin":          extractHostJoin,
	"/inapi.ZoneService/AppInstanceDeploy": extractAppInstanceDeploy,
	"/inapi.ZoneService/AppInstanceDelete": extractAppInstanceDelete,
	"/inapi.ZoneService/GatewayIngressSet": extractGatewayIngressSet,
	"/inapi.ZoneService/PackagePush":       extractPackagePush,
	"/inapi.ZoneService/PackageDelete":     extractPackageDelete,
}

// TargetId format is "{type}/{key}" so prefix filters such as
// "app-instance/web" work directly.

func extractZoneInit(req, resp any, err error) (string, string, map[string]any, bool) {
	r, ok := req.(*inapi.ZoneInitRequest)
	if !ok {
		return "zone", "zone/", nil, true
	}
	return "zone", "zone/" + r.Name, map[string]any{"name": r.Name}, true
}

func extractZoneSet(req, resp any, err error) (string, string, map[string]any, bool) {
	r, ok := req.(*inapi.ZoneSetRequest)
	if !ok {
		return "zone", "zone/" + config.Config.Zonelet.ZoneName, nil, true
	}
	return "zone", "zone/" + config.Config.Zonelet.ZoneName, map[string]any{
		"vpc_bridge_cidr":    r.VpcBridgeCidr,
		"vpc_instance_cidr":  r.VpcInstanceCidr,
		"vpc_network_domain": r.VpcNetworkDomain,
	}, true
}

// extractHostJoin never records req.AccessKey (a secret); the assigned
// host_id is attached on success.
func extractHostJoin(req, resp any, err error) (string, string, map[string]any, bool) {
	r, ok := req.(*inapi.HostJoinRequest)
	if !ok {
		return "host", "host/", nil, true
	}
	detail := map[string]any{"addr": r.Addr}
	if rp, ok := resp.(*inapi.HostJoinResponse); ok && rp != nil && rp.Status != nil {
		detail["host_id"] = rp.Status.HostId
	}
	return "host", "host/" + r.Addr, detail, true
}

// extractAppInstanceDeploy records identity/shape fields only; spec config
// values may hold secrets and are never recorded.
func extractAppInstanceDeploy(req, resp any, err error) (string, string, map[string]any, bool) {
	r, ok := req.(*inapi.AppInstanceDeployRequest)
	if !ok {
		return "app-instance", "app-instance/", nil, true
	}

	detail := map[string]any{
		"name": r.Name,
	}
	if r.ReplicaCap > 0 {
		detail["replica_cap"] = r.ReplicaCap
	}
	if r.Deploy != nil && r.Deploy.Action != "" {
		detail["deploy_action"] = r.Deploy.Action
	}
	if r.Spec != nil {
		if r.Spec.Version != "" {
			detail["spec_version"] = r.Spec.Version
		}
		if pkgs := specPackageRefs(r.Spec); pkgs != "" {
			detail["packages"] = pkgs
		}
	}

	return "app-instance", "app-instance/" + r.Name, detail, true
}

// specPackageRefs renders the spec package references as "name:version"
// (version omitted when unset), comma separated.
func specPackageRefs(spec *inapi.AppSpec) string {
	var parts []string
	for _, pkg := range spec.Packages {
		if pkg == nil {
			continue
		}
		if pkg.Version == "" {
			parts = append(parts, pkg.Name)
		} else {
			parts = append(parts, pkg.Name+":"+pkg.Version)
		}
	}
	return strings.Join(parts, ",")
}

func extractAppInstanceDelete(req, resp any, err error) (string, string, map[string]any, bool) {
	r, ok := req.(*inapi.AppInstanceDeleteRequest)
	if !ok {
		return "app-instance", "app-instance/", nil, true
	}
	name := r.Name
	if name == "" {
		name = r.Id // legacy field
	}
	return "app-instance", "app-instance/" + name, map[string]any{"name": name}, true
}

func extractGatewayIngressSet(req, resp any, err error) (string, string, map[string]any, bool) {
	r, ok := req.(*inapi.GatewayIngressSetRequest)
	if !ok || r.Item == nil {
		return "ingress", "ingress/", nil, true
	}
	detail := map[string]any{
		"domain": r.Item.Domain,
		"routes": strconv.Itoa(len(r.Item.Routes)),
	}
	if r.Item.Action != "" {
		detail["action"] = r.Item.Action
	}
	if r.Item.Options != nil && r.Item.Options.LetsencryptEnable {
		detail["letsencrypt_enable"] = true
	}
	return "ingress", "ingress/" + r.Item.Domain, detail, true
}

// extractPackagePush records only the completed upload or a failed call;
// intermediate chunks return audit=false.
func extractPackagePush(req, resp any, err error) (string, string, map[string]any, bool) {
	r, ok := req.(*inapi.PackagePushRequest)
	if !ok {
		return "pkg", "pkg/", nil, true
	}

	rp, _ := resp.(*inapi.PackagePushResponse)
	complete := rp != nil && rp.File != nil &&
		rp.File.State == inapi.PackageFileStateComplete

	if err == nil && !complete {
		return "pkg", "pkg/" + r.Id, nil, false
	}

	detail := map[string]any{
		"id":        r.Id,
		"overwrite": r.Overwrite,
	}
	if rp != nil && rp.File != nil {
		detail["total_size"] = strconv.FormatInt(rp.File.Size, 10)
	} else if r.TotalSize > 0 {
		detail["total_size"] = strconv.FormatInt(r.TotalSize, 10)
	}

	return "pkg", "pkg/" + r.Id, detail, true
}

func extractPackageDelete(req, resp any, err error) (string, string, map[string]any, bool) {
	r, ok := req.(*inapi.PackageDeleteRequest)
	if !ok {
		return "pkg", "pkg/", nil, true
	}
	detail := map[string]any{"id": r.Id}
	if rp, ok := resp.(*inapi.PackageDeleteResponse); ok && rp != nil {
		detail["chunks_deleted"] = strconv.Itoa(int(rp.ChunksDeleted))
	}
	return "pkg", "pkg/" + r.Id, detail, true
}

// encodeDetail renders detail as canonical JSON, dropping trailing fields
// over detailMaxLen (truncated:true).
func encodeDetail(detail map[string]any) string {
	if len(detail) == 0 {
		return ""
	}

	if s, ok := marshalDetail(detail); ok && len(s) <= detailMaxLen {
		return s
	}

	// Over the limit: drop fields in key order until it fits, then mark
	// truncated:true.
	keys := make([]string, 0, len(detail))
	for k := range detail {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for end := len(keys) - 1; end > 0; end-- {
		trimmed := make(map[string]any, end+1)
		for _, k := range keys[:end] {
			trimmed[k] = detail[k]
		}
		if s, ok := marshalDetail(trimmed); ok && len(s)+len(`,"truncated":true}`) <= detailMaxLen {
			return strings.Replace(s, "}", `,"truncated":true}`, 1)
		}
	}

	return `{"truncated":true}`
}

func marshalDetail(detail map[string]any) (string, bool) {
	if len(detail) == 0 {
		return "", false
	}
	b, err := json.Marshal(detail)
	if err != nil {
		return "", false
	}
	return string(b), true
}
