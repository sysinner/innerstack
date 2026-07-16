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

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func TestRenderAppInstance(t *testing.T) {
	cases := []struct {
		name   string
		inst   *inapi.AppInstance
		want   []string // substrings that must appear
		nowant []string // substrings that must NOT appear (empty sections)
	}{
		{
			name: "full",
			inst: &inapi.AppInstance{
				Meta: &inapi.Metadata{
					Name: "sysinner-cn", User: "admin",
					Created: 1_750_000_000, Updated: 1_750_864_000,
				},
				Spec: &inapi.AppSpec{
					Name: "sysinner-cn", Version: "1.2.0",
					Image: "registry.example.com/sysinner-cn:1.2.0",
					Packages: []*inapi.AppSpecPackage{
						{Name: "sysinner-cn", Version: "1.2.0"},
					},
				},
				Deploy: &inapi.AppDeploy{
					Action: "start", CpuLimit: 2000, MemoryLimit: 4 * (1 << 30),
					ReplicaCap: 2,
					Replicas: []*inapi.AppDeployReplica{
						{Id: 1, State: "running", HostId: "host-a01",
							HostIpv4: "10.0.1.5", VpcIpv4: "172.16.0.5",
							ServicePorts: []*inapi.AppDeployServicePort{
								{Name: "web", Port: 8080, HostPort: 31080},
							}},
					},
					Depends: []*inapi.AppDeployDepend{
						{SpecName: "mysql-8.0", InstanceName: "sysinner-mysql"},
					},
					Stages: &inapi.AppDeployStage{
						Name: "setup", Owner: "scheduler", State: "success",
						Created: 1_750_000_000_000, Finished: 1_750_000_001_200,
						Stages: []*inapi.AppDeployStage{
							{Name: "zonelet", Owner: "zonelet", State: "success",
								Created: 1_750_000_000_500, Finished: 1_750_000_001_300},
						},
					},
				},
				RefByInstances: []string{"web-front"},
			},
			want: []string{
				"== Overview ==",
				"sysinner-cn / 1.2.0",
				"1 / 2", // replicas / cap
				"web:8080->31080",
				"== Replicas ==",
				"== Dependencies ==",
				"mysql-8.0",
				"Referenced by: web-front",
				"== Packages ==",
				"== Deploy Stages ==",
				"1.2s",   // setup stage duration
				"800ms",  // nested zonelet stage duration
				"zonelet",
			},
		},
		{
			name: "minimal_skips_empty_sections",
			inst: &inapi.AppInstance{
				Meta: &inapi.Metadata{Name: "bare"},
				Spec: &inapi.AppSpec{Name: "bare"},
			},
			want: []string{
				"== Overview ==",
				"== Resources ==",
			},
			nowant: []string{
				"== Replicas ==",
				"== Dependencies ==",
				"== Packages ==",
				"== Deploy Stages ==",
			},
		},
		{
			name: "unset_limits_render_dash",
			inst: &inapi.AppInstance{
				Meta:   &inapi.Metadata{Name: "x"},
				Deploy: &inapi.AppDeploy{ReplicaCap: 1},
			},
			want: []string{
				"CPU Limit", "Memory Limit", "Volume Limit",
			},
			nowant: []string{"0m", "0 B"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderAppInstance(&buf, tc.inst)
			out := buf.String()

			for _, w := range tc.want {
				if !containsFold(out, w) {
					t.Errorf("missing substring %q in output:\n%s", w, out)
				}
			}
			for _, nw := range tc.nowant {
				if strings.Contains(out, nw) {
					t.Errorf("unexpected substring %q in output:\n%s", nw, out)
				}
			}

			// nested stage should render after its parent (depth-first order),
			// and its leading-space indent must survive table rendering.
			if tc.name == "full" {
				i, j := strings.Index(out, "setup"), strings.Index(out, "zonelet")
				if i < 0 || j < 0 || j < i {
					t.Errorf("expected zonelet after setup (setup=%d zonelet=%d)", i, j)
				}
				if !strings.Contains(out, "  zonelet") {
					t.Errorf("nested stage lost its indent in output:\n%s", out)
				}
				t.Logf("rendered output:\n%s", out)
			}
		})
	}
}

// TestRenderAppInstanceMetrics verifies the Replicas table surfaces the
// per-replica runtime metrics attached to Status.Replicas: a replica with
// reported metrics renders CPU/Memory/Uptime cells, while one without shows "-".
func TestRenderAppInstanceMetrics(t *testing.T) {
	inst := &inapi.AppInstance{
		Meta: &inapi.Metadata{Name: "app1"},
		Deploy: &inapi.AppDeploy{
			Replicas: []*inapi.AppDeployReplica{
				{Id: 1, State: "running", HostId: "h1"},
				{Id: 2, State: "running", HostId: "h2"},
			},
		},
		Status: &inapi.AppStatus{Replicas: []*inapi.AppReplicaStatus{
			{Id: 1, Metrics: &inapi.NodeMetrics{
				// 30000ms over the window => 0.5 core => "500m".
				CpuUser: 30000, CpuSys: 0,
				MemUsed: 1 << 20, // => "1 MiB"
				Uptime:  3661,    // => "1h 1m"
				// 60s-window deltas => /60 gives bytes/sec.
				NetRecvBytes: 60 * (1 << 20), // => "1 MiB/s"
				NetSentBytes: 60 * (2 << 20), // => "2 MiB/s"
			}},
		}},
	}

	var buf bytes.Buffer
	renderAppInstance(&buf, inst)
	out := buf.String()

	for _, w := range []string{"500m", "1 MiB", "1h 1m", "1 MiB/s", "2 MiB/s"} {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in output:\n%s", w, out)
		}
	}
}

// renderAppInstance must be a no-op on a nil instance rather than panicking.
func TestRenderAppInstanceNilSafe(t *testing.T) {
	var buf bytes.Buffer
	renderAppInstance(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil instance, got %q", buf.String())
	}
}

// containsFold is a case-insensitive Contains, used so header assertions are
// not coupled to tablewriter's auto-uppercase of header text.
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
