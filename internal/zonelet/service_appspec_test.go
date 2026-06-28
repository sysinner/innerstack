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

import "testing"

func TestAppSpecResolveVersion(t *testing.T) {
	tests := []struct {
		name        string
		reqVersion  string
		prevVersion string
		prevExists  bool
		want        string
		wantErr     bool
	}{
		// No previous spec: default and pass-through.
		{"no_prev_empty_defaults", "", "", false, "0.0.1", false},
		{"no_prev_keep_version", "1.2.3", "", false, "1.2.3", false},
		{"no_prev_keep_release_suffix", "0.0.1-1", "", false, "0.0.1-1", false},
		{"no_prev_invalid", "abc", "", false, "", true},
		{"no_prev_invalid_short", "1.0", "", false, "", true},

		// Previous exists, request empty -> default 0.0.1 then resolve.
		{"prev_empty_bare_bumps", "", "0.0.1", true, "0.0.1-1", false},
		{"prev_empty_with_release_bumps", "", "0.0.1-1", true, "0.0.1-2", false},

		// Equal main version: bump release from previous (request release ignored).
		{"equal_main_bare_to_r1", "0.0.1", "0.0.1", true, "0.0.1-1", false},
		{"equal_main_r1_to_r2", "0.0.1", "0.0.1-1", true, "0.0.1-2", false},
		{"equal_main_r2_to_r3", "0.0.1", "0.0.1-2", true, "0.0.1-3", false},
		{"equal_main_ignores_req_release", "0.0.1-5", "0.0.1-1", true, "0.0.1-2", false},

		// Request main greater: use request version as-is.
		{"greater_main_minor", "0.2.0", "0.0.1", true, "0.2.0", false},
		{"greater_main_major", "1.0.0", "0.9.0", true, "1.0.0", false},
		{"greater_main_keep_release_suffix", "1.0.0-3", "0.9.0", true, "1.0.0-3", false},
		{"greater_main_over_prev_release", "0.1.0", "0.0.1-9", true, "0.1.0", false},

		// Request main lower: error.
		{"lower_main_error", "0.9.0", "1.0.0", true, "", true},
		{"lower_main_vs_release_error", "0.0.1", "0.0.2", true, "", true},

		// Invalid request version with previous existing.
		{"prev_invalid_req", "not-a-version", "0.0.1", true, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := appSpecResolveVersion(tt.reqVersion, tt.prevVersion, tt.prevExists)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("appSpecResolveVersion(%q, %q, %v) want error, got %q",
						tt.reqVersion, tt.prevVersion, tt.prevExists, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("appSpecResolveVersion(%q, %q, %v) unexpected error: %v",
					tt.reqVersion, tt.prevVersion, tt.prevExists, err)
			}
			if got != tt.want {
				t.Errorf("appSpecResolveVersion(%q, %q, %v) = %q, want %q",
					tt.reqVersion, tt.prevVersion, tt.prevExists, got, tt.want)
			}
		})
	}
}

func TestAppSpecSplitVersion(t *testing.T) {
	tests := []struct {
		in       string
		wantMain string
		wantRel  int
	}{
		{"0.0.1", "0.0.1", 0},
		{"0.0.1-1", "0.0.1", 1},
		{"0.0.1-12", "0.0.1", 12},
		{"1.2.3-0", "1.2.3", 0},
		{"1.0.0-alpha", "1.0.0-alpha", 0},
		{"", "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			main, rel := appSpecSplitVersion(tt.in)
			if main != tt.wantMain || rel != tt.wantRel {
				t.Errorf("appSpecSplitVersion(%q) = (%q, %d), want (%q, %d)",
					tt.in, main, rel, tt.wantMain, tt.wantRel)
			}
		})
	}
}
