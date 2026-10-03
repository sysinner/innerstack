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
	"strings"
	"testing"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// TestExtractors covers every registered extractor: target derivation,
// whitelisted detail fields and the audit flag (PackagePush chunks).
func TestExtractors(t *testing.T) {

	config.Config.Zonelet.ZoneName = "testzone"

	fixtures := []struct {
		name       string
		method     string
		req, resp  any
		err        error
		audit      bool
		targetType string
		targetId   string
		detailWant map[string]any
	}{
		{
			name:       "zone_init",
			method:     "/inapi.ZoneService/ZoneInit",
			req:        &inapi.ZoneInitRequest{Name: "zone1"},
			audit:      true,
			targetType: "zone",
			targetId:   "zone/zone1",
			detailWant: map[string]any{"name": "zone1"},
		},
		{
			name:   "zone_set",
			method: "/inapi.ZoneService/ZoneSet",
			req: &inapi.ZoneSetRequest{
				VpcBridgeCidr:    "192.168.10.0/24",
				VpcInstanceCidr:  "10.10.0.0/16",
				VpcNetworkDomain: "local",
			},
			audit:      true,
			targetType: "zone",
			targetId:   "zone/testzone",
			detailWant: map[string]any{
				"vpc_bridge_cidr":    "192.168.10.0/24",
				"vpc_instance_cidr":  "10.10.0.0/16",
				"vpc_network_domain": "local",
			},
		},
		{
			name:   "host_join_ok",
			method: "/inapi.ZoneService/HostJoin",
			req: &inapi.HostJoinRequest{
				Addr:      "192.168.1.10",
				AccessKey: "ak_000000000000_topsecretvalue",
			},
			resp: &inapi.HostJoinResponse{
				Status: &inapi.HostStatus{HostId: "host01"},
			},
			audit:      true,
			targetType: "host",
			targetId:   "host/192.168.1.10",
			detailWant: map[string]any{"addr": "192.168.1.10", "host_id": "host01"},
		},
		{
			name:   "app_instance_deploy",
			method: "/inapi.ZoneService/AppInstanceDeploy",
			req: &inapi.AppInstanceDeployRequest{
				Name:       "web",
				ReplicaCap: 3,
				Deploy:     &inapi.AppDeploy{Action: inapi.OpActionStart},
				Spec: &inapi.AppSpec{
					Name:    "web",
					Version: "1.2.3",
					Packages: []*inapi.AppSpecPackage{
						{Name: "nginx", Version: "1.0"},
						{Name: "webapp"},
					},
				},
			},
			audit:      true,
			targetType: "app-instance",
			targetId:   "app-instance/web",
			detailWant: map[string]any{
				"name":          "web",
				"replica_cap":   uint32(3),
				"deploy_action": "start",
				"spec_version":  "1.2.3",
				"packages":      "nginx:1.0,webapp",
			},
		},
		{
			name:       "app_instance_delete_name",
			method:     "/inapi.ZoneService/AppInstanceDelete",
			req:        &inapi.AppInstanceDeleteRequest{Name: "web"},
			audit:      true,
			targetType: "app-instance",
			targetId:   "app-instance/web",
			detailWant: map[string]any{"name": "web"},
		},
		{
			name:       "app_instance_delete_legacy_id",
			method:     "/inapi.ZoneService/AppInstanceDelete",
			req:        &inapi.AppInstanceDeleteRequest{Id: "inst-123"},
			audit:      true,
			targetType: "app-instance",
			targetId:   "app-instance/inst-123",
			detailWant: map[string]any{"name": "inst-123"},
		},
		{
			name:   "gateway_ingress_set",
			method: "/inapi.ZoneService/GatewayIngressSet",
			req: &inapi.GatewayIngressSetRequest{
				Item: &inapi.GatewayIngress{
					Domain:  "example.com",
					Action:  inapi.GatewayIngressActionEnable,
					Options: &inapi.GatewayIngress_Options{LetsencryptEnable: true},
					Routes: []*inapi.GatewayIngress_HttpRoute{
						{Path: "/"}, {Path: "/api"},
					},
				},
			},
			audit:      true,
			targetType: "ingress",
			targetId:   "ingress/example.com",
			detailWant: map[string]any{
				"domain":             "example.com",
				"routes":             "2",
				"action":             "enable",
				"letsencrypt_enable": true,
			},
		},
		{
			name:   "package_push_intermediate_chunk_skipped",
			method: "/inapi.ZoneService/PackagePush",
			req:    &inapi.PackagePushRequest{Id: "nginx_1.0.0_linux_amd64"},
			resp: &inapi.PackagePushResponse{
				Id:   "nginx_1.0.0_linux_amd64",
				File: &inapi.PackageFile{State: inapi.PackageFileStateUploading},
			},
			audit: false, // design D4: intermediate chunks are not recorded
		},
		{
			name:   "package_push_complete",
			method: "/inapi.ZoneService/PackagePush",
			req: &inapi.PackagePushRequest{
				Id:        "nginx_1.0.0_linux_amd64",
				Overwrite: true,
			},
			resp: &inapi.PackagePushResponse{
				Id: "nginx_1.0.0_linux_amd64",
				File: &inapi.PackageFile{
					State: inapi.PackageFileStateComplete,
					Size:  2048,
				},
			},
			audit:      true,
			targetType: "pkg",
			targetId:   "pkg/nginx_1.0.0_linux_amd64",
			detailWant: map[string]any{
				"id":         "nginx_1.0.0_linux_amd64",
				"overwrite":  true,
				"total_size": "2048",
			},
		},
		{
			name:   "package_push_error",
			method: "/inapi.ZoneService/PackagePush",
			req:    &inapi.PackagePushRequest{Id: "nginx_1.0.0_linux_amd64", TotalSize: 99},
			err:    errTest("invalid chunk"),
			audit:  true,
			detailWant: map[string]any{
				"id":         "nginx_1.0.0_linux_amd64",
				"overwrite":  false,
				"total_size": "99",
			},
			targetType: "pkg",
			targetId:   "pkg/nginx_1.0.0_linux_amd64",
		},
		{
			name:   "package_delete",
			method: "/inapi.ZoneService/PackageDelete",
			req:    &inapi.PackageDeleteRequest{Id: "nginx_1.0.0_linux_amd64"},
			resp:   &inapi.PackageDeleteResponse{Id: "nginx_1.0.0_linux_amd64", ChunksDeleted: 3},
			audit:  true,
			detailWant: map[string]any{
				"id":             "nginx_1.0.0_linux_amd64",
				"chunks_deleted": "3",
			},
			targetType: "pkg",
			targetId:   "pkg/nginx_1.0.0_linux_amd64",
		},
		{
			name:   "user_set",
			method: "/inapi.ZoneService/UserSet",
			req: &inapi.UserSetRequest{
				Name:        "tim",
				State:       "disabled",
				Description: "on leave",
			},
			audit:      true,
			targetType: "user",
			targetId:   "user/tim",
			detailWant: map[string]any{
				"name":        "tim",
				"state":       "disabled",
				"description": "on leave",
			},
		},
		{
			name:       "user_delete",
			method:     "/inapi.ZoneService/UserDelete",
			req:        &inapi.UserDeleteRequest{Name: "tim"},
			audit:      true,
			targetType: "user",
			targetId:   "user/tim",
			detailWant: map[string]any{"name": "tim"},
		},
		{
			name:   "access_key_set",
			method: "/inapi.ZoneService/AccessKeySet",
			req: &inapi.AccessKeySetRequest{
				User:   "tim",
				Scopes: []string{"app:rw", "pkg:ro"},
			},
			resp: &inapi.AccessKeySetResponse{
				KeyId:     "69cb4cfe4b64",
				AccessKey: "ak_69cb4cfe4b64_topsecretvalue",
			},
			audit:      true,
			targetType: "access-key",
			targetId:   "access-key/69cb4cfe4b64",
			detailWant: map[string]any{
				"user":   "tim",
				"scopes": "app:rw,pkg:ro",
				"key_id": "69cb4cfe4b64",
			},
		},
		{
			name:       "access_key_delete",
			method:     "/inapi.ZoneService/AccessKeyDelete",
			req:        &inapi.AccessKeyDeleteRequest{KeyId: "69cb4cfe4b64"},
			audit:      true,
			targetType: "access-key",
			targetId:   "access-key/69cb4cfe4b64",
			detailWant: map[string]any{"key_id": "69cb4cfe4b64"},
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {

			ext, ok := extractors[f.method]
			if !ok {
				t.Fatalf("method %s not registered", f.method)
			}

			targetType, targetId, detail, audit := ext(f.req, f.resp, f.err)

			if audit != f.audit {
				t.Fatalf("audit = %v, want %v", audit, f.audit)
			}
			if !audit {
				return
			}
			if targetType != f.targetType {
				t.Fatalf("targetType = %q, want %q", targetType, f.targetType)
			}
			if targetId != f.targetId {
				t.Fatalf("targetId = %q, want %q", targetId, f.targetId)
			}

			var got map[string]any
			if err := json.Unmarshal([]byte(encodeDetail(detail)), &got); err != nil {
				t.Fatalf("detail not valid json: %v", err)
			}
			for k, want := range f.detailWant {
				v, ok := got[k]
				if !ok {
					t.Fatalf("detail missing key %q in %v", k, got)
				}
				wantJSON, _ := json.Marshal(want)
				gotJSON, _ := json.Marshal(v)
				if string(wantJSON) != string(gotJSON) {
					t.Fatalf("detail[%q] = %s, want %s", k, gotJSON, wantJSON)
				}
			}
		})
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// TestExtractorSanitization asserts that secrets never reach the record:
// HostJoin access_key and AppSpec config values must not appear anywhere in
// the serialized record (design 8.2 blacklist).
func TestExtractorSanitization(t *testing.T) {

	const (
		hostJoinSecret = "ak_000000000000_hjoinTopSecret123"
		specSecret     = "s3cr3tPa55word"
		keySecret      = "oneTimeKeySecret99"
	)

	fixtures := []struct {
		name   string
		method string
		req    any
		resp   any
	}{
		{
			name:   "host_join_access_key",
			method: "/inapi.ZoneService/HostJoin",
			req:    &inapi.HostJoinRequest{Addr: "10.0.0.9", AccessKey: hostJoinSecret},
			resp:   &inapi.HostJoinResponse{},
		},
		{
			name:   "access_key_set_credential",
			method: "/inapi.ZoneService/AccessKeySet",
			req:    &inapi.AccessKeySetRequest{User: "tim", Scopes: []string{"app:rw"}},
			resp: &inapi.AccessKeySetResponse{
				KeyId:     "69cb4cfe4b64",
				AccessKey: "ak_69cb4cfe4b64_" + keySecret,
			},
		},
		{
			name:   "app_deploy_config_values",
			method: "/inapi.ZoneService/AppInstanceDeploy",
			req: &inapi.AppInstanceDeployRequest{
				Name: "db",
				Spec: &inapi.AppSpec{
					Name:    "db",
					Version: "1.0.0",
					Configs: []*inapi.AppSpecConfigItem{
						{Name: "admin_password", Type: inapi.SpecFieldTypeAuthCert,
							Default: specSecret},
					},
				},
			},
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {

			ext := extractors[f.method]
			_, _, detail, audit := ext(f.req, f.resp, nil)
			if !audit {
				t.Fatal("expected audited")
			}

			bs, err := json.Marshal(map[string]any{
				"detail": detail,
				"record": buildRecordForTest(f.method, detail),
			})
			if err != nil {
				t.Fatal(err)
			}

			for _, secret := range []string{"hjoinTopSecret123", "s3cr3tPa55word", keySecret} {
				if strings.Contains(string(bs), secret) {
					t.Fatalf("secret %q leaked into audit detail: %s", secret, bs)
				}
			}
		})
	}
}

func buildRecordForTest(method string, detail map[string]any) *inapi.AuditRecord {
	return &inapi.AuditRecord{
		Action:     method,
		TargetType: "test",
		TargetId:   "test/x",
		Detail:     encodeDetail(detail),
	}
}

// TestEncodeDetailTruncation verifies the 512-byte bound and the
// truncated:true marker when fields must be dropped.
func TestEncodeDetailTruncation(t *testing.T) {

	pad := strings.Repeat("x", 200)

	// Under the bound: all fields survive, no marker.
	small := encodeDetail(map[string]any{"a": pad})
	if strings.Contains(small, "truncated") {
		t.Fatalf("small detail unexpectedly marked truncated: %s", small)
	}

	// Over the bound: bounded output with the marker.
	big := encodeDetail(map[string]any{
		"a": pad,
		"b": pad,
		"c": pad,
	})
	if len(big) > detailMaxLen {
		t.Fatalf("detail len %d exceeds bound %d", len(big), detailMaxLen)
	}
	if !strings.Contains(big, `"truncated":true`) {
		t.Fatalf("truncated detail missing marker: %s", big)
	}

	if encodeDetail(nil) != "" {
		t.Fatal("empty detail should encode to empty string")
	}
}

// TestErrorTruncation verifies the 256-byte error bound.
func TestErrorTruncation(t *testing.T) {
	long := strings.Repeat("e", 1000)
	got := truncateString(long, errorMaxLen)
	if len(got) != errorMaxLen {
		t.Fatalf("truncated error len = %d, want %d", len(got), errorMaxLen)
	}
}
