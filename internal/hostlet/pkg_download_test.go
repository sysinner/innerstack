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
	"errors"
	"reflect"
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// TestResolvePackage locks the specificity ordering of resolvePackage: a host
// always gets its native (os, arch) binary when one exists, degrading to "src"
// (any arch) and "all" (any os) only when necessary. The fake lister records
// the (os, arch) query sequence and returns canned responses keyed by "os/arch",
// so each case asserts both *which* package is selected and *which* candidates
// were consulted.
func TestResolvePackage(t *testing.T) {
	mkPkg := func(os, arch, state string) *inapi.Package {
		return &inapi.Package{
			Metadata: &inapi.PackageMetadata{Name: "odoo-ce"},
			Release: &inapi.PackageRelease{
				Version: "19.0.1",
				Os:      os,
				Arch:    arch,
			},
			File: &inapi.PackageFile{State: state},
		}
	}

	tests := []struct {
		name      string
		os        string                       // host os passed to resolvePackage
		arch      string                       // host arch passed to resolvePackage
		responses map[string][]*inapi.Package  // keyed by queried "os/arch"
		queryErr  map[string]error             // keyed by queried "os/arch"
		wantSel   string                       // selected "os/arch", "" when wantErr
		wantErr   bool
		wantTried []string // exact sequence of "os/arch" queries
	}{
		{
			// Native binary present: most specific candidate wins, nothing else
			// is queried.
			name:    "native_binary_preferred",
			os:      "linux",
			arch:    "amd64",
			responses: map[string][]*inapi.Package{
				"linux/amd64": {mkPkg("linux", "amd64", inapi.PackageFileStateComplete)},
				"linux/src":   {mkPkg("linux", "src", inapi.PackageFileStateComplete)},
			},
			wantSel:   "linux/amd64",
			wantTried: []string{"linux/amd64"},
		},
		{
			// arm64 host, no arm64 binary: fall back to a linux/src release.
			name: "native_arch_miss_falls_back_to_src",
			os:   "linux",
			arch: "arm64",
			responses: map[string][]*inapi.Package{
				"linux/src": {mkPkg("linux", "src", inapi.PackageFileStateComplete)},
			},
			wantSel:   "linux/src",
			wantTried: []string{"linux/arm64", "linux/src"},
		},
		{
			// src-only catalog on any host.
			name: "src_only_package",
			os:   "linux",
			arch: "amd64",
			responses: map[string][]*inapi.Package{
				"linux/src": {mkPkg("linux", "src", inapi.PackageFileStateComplete)},
			},
			wantSel:   "linux/src",
			wantTried: []string{"linux/amd64", "linux/src"},
		},
		{
			// Native release still uploading: error, do NOT silently fall back
			// (that would mask a transient upload state).
			name: "incomplete_native_does_not_fall_back",
			os:   "linux",
			arch: "amd64",
			responses: map[string][]*inapi.Package{
				"linux/amd64": {mkPkg("linux", "amd64", inapi.PackageFileStateUploading)},
				"linux/src":   {mkPkg("linux", "src", inapi.PackageFileStateComplete)},
			},
			wantErr:   true,
			wantTried: []string{"linux/amd64"},
		},
		{
			// RPC failure on the native query aborts immediately.
			name:      "native_query_error_aborts",
			os:        "linux",
			arch:      "amd64",
			queryErr:  map[string]error{"linux/amd64": errors.New("rpc unavailable")},
			wantErr:   true,
			wantTried: []string{"linux/amd64"},
		},
		{
			// Nothing for any (os, arch) candidate: all four are tried.
			name:      "all_candidates_miss_returns_error",
			os:        "linux",
			arch:      "arm64",
			responses: map[string][]*inapi.Package{},
			wantErr:   true,
			wantTried: []string{"linux/arm64", "linux/src", "all/arm64", "all/src"},
		},
		{
			// No driver info -> empty native arch: the native-arch slot is
			// skipped and only the src candidates are queried.
			name: "empty_arch_skips_native_slot",
			os:   "linux",
			arch: "",
			responses: map[string][]*inapi.Package{
				"linux/src": {mkPkg("linux", "src", inapi.PackageFileStateComplete)},
			},
			wantSel:   "linux/src",
			wantTried: []string{"linux/src"},
		},
		{
			// freebsd host, no freebsd release: fall back to an all/amd64
			// release (os wildcard beats the arch wildcard in specificity).
			name: "native_os_miss_falls_back_to_all",
			os:   "freebsd",
			arch: "amd64",
			responses: map[string][]*inapi.Package{
				"all/amd64": {mkPkg("all", "amd64", inapi.PackageFileStateComplete)},
			},
			wantSel:   "all/amd64",
			wantTried: []string{"freebsd/amd64", "freebsd/src", "all/amd64"},
		},
		{
			// Universal source package: every concrete candidate misses before
			// landing on all/src.
			name: "all_src_universal_fallback",
			os:   "darwin",
			arch: "arm64",
			responses: map[string][]*inapi.Package{
				"all/src": {mkPkg("all", "src", inapi.PackageFileStateComplete)},
			},
			wantSel:   "all/src",
			wantTried: []string{"darwin/arm64", "darwin/src", "all/arm64", "all/src"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tried []string
			lister := packageLister(func(ctx context.Context, req *inapi.PackageListRequest) (*inapi.PackageListResponse, error) {
				key := req.Os + "/" + req.Arch
				tried = append(tried, key)
				if tt.queryErr != nil {
					if e, ok := tt.queryErr[key]; ok {
						return nil, e
					}
				}
				return &inapi.PackageListResponse{Items: tt.responses[key]}, nil
			})

			pkg, err := resolvePackage(context.Background(), lister,
				"odoo-ce", "19.0", tt.os, tt.arch)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got package %s/%s", pkg.Release.Os, pkg.Release.Arch)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				gotSel := pkg.Release.Os + "/" + pkg.Release.Arch
				if gotSel != tt.wantSel {
					t.Errorf("selected = %q, want %q", gotSel, tt.wantSel)
				}
			}

			if !reflect.DeepEqual(tried, tt.wantTried) {
				t.Errorf("queried candidates = %v, want %v", tried, tt.wantTried)
			}
		})
	}
}
