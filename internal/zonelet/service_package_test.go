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
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func TestSemverCompare(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		v2   string
		want int
	}{
		// release-revision (-rN) semantics: higher N is newer,
		// and a bare version equals -r0.
		{"release_iter_bare_vs_r2", "1.26.4", "1.26.4-r2", -1},
		{"release_iter_r2_vs_bare", "1.26.4-r2", "1.26.4", 1},
		{"release_iter_r2_vs_r1", "1.26.4-r2", "1.26.4-r1", 1},
		{"release_iter_r1_vs_r2", "1.26.4-r1", "1.26.4-r2", -1},
		{"release_iter_equal", "1.26.4-r2", "1.26.4-r2", 0},
		{"release_iter_bare_vs_bare", "1.26.4", "1.26.4", 0},

		// v-prefix variants are handled identically
		{"release_iter_vprefix", "v1.26.4-r2", "v1.26.4", 1},

		// core version differences take precedence over -rN
		{"core_higher_over_iter", "1.27.0", "1.26.4-r9", 1},
		{"core_lower_over_iter", "1.26.3-r9", "1.26.4", -1},

		// standard semver comparisons still work
		{"basic_newer", "2.0.0", "1.0.0", 1},
		{"basic_older", "1.0.0", "2.0.0", -1},
		{"basic_equal", "1.0.0", "1.0.0", 0},
		{"patch_diff", "1.0.1", "1.0.0", 1},
		{"minor_diff", "1.1.0", "1.0.9", 1},

		// Release candidates (-rcN) are SemVer pre-releases: older than the
		// bare release. They must NOT be treated as a release revision.
		{"rc_lower_than_bare", "1.26.4-rc1", "1.26.4", -1},
		{"bare_higher_than_rc", "1.26.4", "1.26.4-rc1", 1},
		{"rc2_higher_than_rc1", "1.26.4-rc2", "1.26.4-rc1", 1},
		{"rc_lower_than_release_revision", "1.26.4-rc1", "1.26.4-r1", -1},
		{"release_revision_higher_than_rc", "1.26.4-r1", "1.26.4-rc1", 1},

		// true SemVer pre-release (non "-r<digits>") must NOT be
		// misread as a release revision.
		{"prerelease_not_iter", "1.0.0-beta", "1.0.0", -1},
		{"prerelease_vs_release_iter", "1.0.0-r1", "1.0.0-beta", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := semverCompare(tt.v1, tt.v2)
			if got != tt.want {
				t.Errorf("semverCompare(%q, %q) = %d, want %d",
					tt.v1, tt.v2, got, tt.want)
			}
		})
	}
}

func TestFilterLatestPackages(t *testing.T) {
	mkPkg := func(name, version, os, arch string) *inapi.Package {
		return &inapi.Package{
			Metadata: &inapi.PackageMetadata{Name: name},
			Release: &inapi.PackageRelease{
				Version: version,
				Os:      os,
				Arch:    arch,
			},
		}
	}

	tests := []struct {
		name string
		in   []*inapi.Package
		want []string // expected release versions in (name,os,arch) order
	}{
		{
			name: "release_iteration_picks_r2_over_bare",
			in: []*inapi.Package{
				mkPkg("kube", "1.26.4", "linux", "amd64"),
				mkPkg("kube", "1.26.4-r2", "linux", "amd64"),
			},
			want: []string{"1.26.4-r2"},
		},
		{
			name: "release_iteration_picks_highest_r",
			in: []*inapi.Package{
				mkPkg("kube", "1.26.4-r1", "linux", "amd64"),
				mkPkg("kube", "1.26.4-r3", "linux", "amd64"),
				mkPkg("kube", "1.26.4-r2", "linux", "amd64"),
			},
			want: []string{"1.26.4-r3"},
		},
		{
			name: "core_version_beats_lower_core_with_higher_r",
			in: []*inapi.Package{
				mkPkg("kube", "1.27.0", "linux", "amd64"),
				mkPkg("kube", "1.26.4-r9", "linux", "amd64"),
			},
			want: []string{"1.27.0"},
		},
		{
			name: "groups_by_os_arch",
			in: []*inapi.Package{
				mkPkg("kube", "1.26.4", "linux", "amd64"),
				mkPkg("kube", "1.26.4-r2", "linux", "amd64"),
				mkPkg("kube", "1.26.4-r2", "linux", "arm64"),
			},
			want: []string{"1.26.4-r2", "1.26.4-r2"},
		},
		{
			name: "different_names_kept_separately",
			in: []*inapi.Package{
				mkPkg("app-a", "1.0.0", "linux", "amd64"),
				mkPkg("app-b", "2.0.0-r1", "linux", "amd64"),
				mkPkg("app-b", "2.0.0", "linux", "amd64"),
			},
			want: []string{"1.0.0", "2.0.0-r1"},
		},
		{
			// Mixed rc/bare/r ordering for the same base version.
			// Expected: 1.26.4-rc1 < 1.26.4 < 1.26.4-r1 < 1.26.4-r2
			name: "mixed_rc_bare_and_release_revisions",
			in: []*inapi.Package{
				mkPkg("kube", "1.26.4-rc1", "linux", "amd64"),
				mkPkg("kube", "1.26.4", "linux", "amd64"),
				mkPkg("kube", "1.26.4-r1", "linux", "amd64"),
				mkPkg("kube", "1.26.4-r2", "linux", "amd64"),
			},
			want: []string{"1.26.4-r2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterLatestPackages(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("filterLatestPackages returned %d items, want %d",
					len(got), len(tt.want))
			}
			for i, wv := range tt.want {
				if got[i].Release.Version != wv {
					t.Errorf("item[%d].version = %q, want %q",
						i, got[i].Release.Version, wv)
				}
			}
		})
	}
}
