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

func TestRenderAppSpec(t *testing.T) {
	full := &inapi.AppSpec{
		Name: "gitea-x1", Version: "1.21.0",
		Image:       "gitea/gitea:1.21",
		Description: "Self-hosted Git service",
		Resources: &inapi.AppSpecResources{
			CpuLimit: "500m", MemoryLimit: "512Mi", VolumeLimit: "10Gi",
		},
		Depends: []*inapi.AppSpecDepend{
			{Name: "mysql-8.0", Version: "8.0.x"},
		},
		ServiceDepends: []*inapi.AppSpecDepend{
			{Name: "redis", Version: "7.x"},
		},
		ServicePorts: []*inapi.AppSpecServicePort{
			{Name: "web", Port: 3000},
		},
		Packages: []*inapi.AppSpecPackage{
			{Name: "gitea", Version: "1.21.0"},
		},
		Configs: []*inapi.AppSpecConfigItem{
			{Name: "server", Title: "Server", Type: "group", Items: []*inapi.AppSpecConfigItem{
				{Name: "domain", Title: "Domain", Type: "string", Default: "localhost"},
			}},
			{Name: "databases", Type: "array_group", KeyItem: "name", Items: []*inapi.AppSpecConfigItem{
				{Name: "name", Type: "string"},
				{Name: "host", Type: "string"},
			}},
		},
		Tasks: []*inapi.AppSpecTask{
			{Name: "init", OnStartup: true, Script: "gitea admin init"},
			{Name: "backup", IntervalSeconds: 3600, Script: "gitea dump"},
			{Name: "nightly", Cron: "0 3 * * *", Script: "echo hi"},
		},
	}

	cases := []struct {
		name   string
		spec   *inapi.AppSpec
		want   []string
		nowant []string
	}{
		{
			name: "full",
			spec: full,
			want: []string{
				"== Overview ==",
				"gitea-x1",
				"Self-hosted Git service",
				"== Resources ==",
				"== Depends ==",
				"mysql-8.0",
				"redis",
				"== Service Ports ==",
				"3000",
				"== Packages ==",
				"== Configs ==",
				"== Tasks ==",
				"on_startup",
				"interval=3600s",
				"cron=0 3",
				"key=name", // array_group key_item annotation
			},
		},
		{
			name: "minimal_skips_empty_sections",
			spec: &inapi.AppSpec{Name: "bare"},
			want: []string{
				"== Overview ==",
				"== Resources ==",
			},
			nowant: []string{
				"== Depends ==",
				"== Service Ports ==",
				"== Packages ==",
				"== Configs ==",
				"== Tasks ==",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			renderAppSpec(&buf, tc.spec)
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

			// nested config item keeps its indent; array_group child items
			// render after the group header (depth-first order).
			if tc.name == "full" {
				if !strings.Contains(out, "  domain") {
					t.Errorf("nested config item lost its indent in output:\n%s", out)
				}
				i := strings.Index(out, "databases")
				j := strings.Index(out, "name")
				if i < 0 || j < 0 || j < i {
					t.Errorf("expected array_group child after header (databases=%d name=%d)", i, j)
				}
				t.Logf("rendered output:\n%s", out)
			}
		})
	}
}

// renderAppSpec must be a no-op on a nil spec rather than panicking.
func TestRenderAppSpecNilSafe(t *testing.T) {
	var buf bytes.Buffer
	renderAppSpec(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil spec, got %q", buf.String())
	}
}

func TestSpecResourceHelpers(t *testing.T) {
	// cpu parses millicores; bytes parses binary quantities; empty -> "-";
	// unparsable values fall back to the raw text verbatim.
	if got := specCpu(""); got != "-" {
		t.Errorf("specCpu(\"\") = %q, want -", got)
	}
	if got := specCpu("500m"); got == "-" || got == "" {
		t.Errorf("specCpu(\"500m\") = %q, want non-empty", got)
	}
	if got := specCpu("abc"); got != "abc" {
		t.Errorf("specCpu(\"abc\") = %q, want raw fallback abc", got)
	}
	if got := specBytes(""); got != "-" {
		t.Errorf("specBytes(\"\") = %q, want -", got)
	}
	if got := specBytes("512Mi"); got == "-" || got == "" {
		t.Errorf("specBytes(\"512Mi\") = %q, want non-empty", got)
	}
}

func TestCliTaskTrigger(t *testing.T) {
	cases := []struct {
		task *inapi.AppSpecTask
		want string
	}{
		{nil, "-"},
		{&inapi.AppSpecTask{Name: "a", OnStartup: true}, "on_startup"},
		{&inapi.AppSpecTask{Name: "b", OnShutdown: true}, "on_shutdown"},
		{&inapi.AppSpecTask{Name: "c", IntervalSeconds: 120}, "interval=120s"},
		{&inapi.AppSpecTask{Name: "d", Cron: "0 3 * * *"}, "cron=0 3 * * *"},
		{&inapi.AppSpecTask{Name: "e"}, "-"},
	}
	for _, tc := range cases {
		if got := cliTaskTrigger(tc.task); got != tc.want {
			t.Errorf("cliTaskTrigger(%v) = %q, want %q", tc.task, got, tc.want)
		}
	}
}
