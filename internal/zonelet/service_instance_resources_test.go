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

// TestParseSpecResources covers the validation of a normalized
// AppSpecResources into int64 ranges: legacy fields are not mapped here (the
// caller normalizes first), so each fixture feeds the range fields directly.
func TestParseSpecResources(t *testing.T) {
	cases := []struct {
		name    string
		res     *inapi.AppSpecResources
		wantErr bool
	}{
		{
			name: "valid_range",
			res: &inapi.AppSpecResources{
				CpuMin: "500m", CpuMax: "2000m",
				MemoryMin: "64Mi", MemoryMax: "512Mi",
				VolumeMin: "1Gi", VolumeMax: "10Gi",
			},
		},
		{
			name: "fixed_point_min_eq_max",
			res: &inapi.AppSpecResources{
				CpuMin: "500m", CpuMax: "500m",
				MemoryMin: "128Mi", MemoryMax: "128Mi",
				VolumeMin: "5Gi", VolumeMax: "5Gi",
			},
		},
		{
			name: "cpu_min_gt_max",
			res: &inapi.AppSpecResources{
				CpuMin: "2000m", CpuMax: "500m",
				MemoryMin: "64Mi", MemoryMax: "512Mi",
				VolumeMin: "1Gi", VolumeMax: "10Gi",
			},
			wantErr: true,
		},
		{
			name: "memory_out_of_system_bounds",
			res: &inapi.AppSpecResources{
				CpuMin: "500m", CpuMax: "1000m",
				MemoryMin: "1Ki", MemoryMax: "512Mi", // below MemoryMin (64MiB)
				VolumeMin: "1Gi", VolumeMax: "10Gi",
			},
			wantErr: true,
		},
		{
			name:    "nil_resources",
			res:     nil,
			wantErr: true,
		},
		{
			name: "invalid_cpu_min_unit",
			res: &inapi.AppSpecResources{
				CpuMin: "abc",
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := parseSpecResources(tc.res)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil + %+v", p)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.cpuMin <= 0 || p.memMin <= 0 || p.volMin <= 0 {
				t.Errorf("parsed min must be positive: %+v", p)
			}
			if p.cpuMin > p.cpuMax || p.memMin > p.memMax || p.volMin > p.volMax {
				t.Errorf("min must be <= max: %+v", p)
			}
		})
	}
}

// TestParseSpecResourcesLegacyPipeline verifies the full normalize-then-parse
// pipeline for a legacy single-value spec: cpu_limit="500m" must map to a
// fixed point [500, 500] millicores.
func TestParseSpecResourcesLegacyPipeline(t *testing.T) {
	res := &inapi.AppSpecResources{
		CpuLimit: "500m", MemoryLimit: "128Mi", VolumeLimit: "5Gi",
	}
	res.NormalizeLegacy()
	p, err := parseSpecResources(res)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.cpuMin != 500 || p.cpuMax != 500 {
		t.Errorf("cpu legacy mapping = [%d, %d], want [500, 500]", p.cpuMin, p.cpuMax)
	}
	if res.CpuLimit != "" {
		t.Errorf("legacy cpu_limit not cleared: %q", res.CpuLimit)
	}
}

func TestResolveInRange(t *testing.T) {
	cases := []struct {
		v, lo, hi, want int64
	}{
		{0, 100, 500, 100},   // unset -> default to min
		{-5, 100, 500, 100},  // non-positive -> min
		{50, 100, 500, 100},  // below min -> clamp up
		{600, 100, 500, 500}, // above max -> clamp down
		{250, 100, 500, 250}, // in range -> unchanged
		{100, 100, 500, 100}, // boundary min
		{500, 100, 500, 500}, // boundary max
	}
	for _, tc := range cases {
		got := resolveInRange(tc.v, tc.lo, tc.hi)
		if got != tc.want {
			t.Errorf("resolveInRange(%d, %d, %d) = %d, want %d",
				tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}

func TestRequireInRange(t *testing.T) {
	// zero v -> the min default, no error
	if v, err := requireInRange("x", 0, 100, 500); err != nil || v != 100 {
		t.Errorf("requireInRange(0,...) = %d, %v; want 100, nil", v, err)
	}
	// in range -> unchanged, no error
	if v, err := requireInRange("x", 250, 100, 500); err != nil || v != 250 {
		t.Errorf("requireInRange(250,...) = %d, %v; want 250, nil", v, err)
	}
	// below range -> error
	if _, err := requireInRange("x", 50, 100, 500); err == nil {
		t.Errorf("requireInRange(50,...) expected error, got nil")
	}
	// above range -> error
	if _, err := requireInRange("x", 600, 100, 500); err == nil {
		t.Errorf("requireInRange(600,...) expected error, got nil")
	}
}
