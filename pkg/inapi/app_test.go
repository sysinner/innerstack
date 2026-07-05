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

package inapi

import "testing"

func TestNormalizeLegacy(t *testing.T) {
	// Each case covers all three resources (cpu, memory, volume) uniformly;
	// the same mapping rules apply to each. Fields not mentioned start empty.
	cases := []struct {
		name string
		in   *AppSpecResources
		want *AppSpecResources
	}{
		{
			name: "legacy_only_maps_to_fixed_point",
			in:   &AppSpecResources{CpuLimit: "500m", MemoryLimit: "512Mi", VolumeLimit: "10Gi"},
			want: &AppSpecResources{CpuMin: "500m", CpuMax: "500m",
				MemoryMin: "512Mi", MemoryMax: "512Mi",
				VolumeMin: "10Gi", VolumeMax: "10Gi"},
		},
		{
			name: "range_only_unchanged_and_legacy_cleared",
			in: &AppSpecResources{
				CpuMin: "500m", CpuMax: "2000m",
				MemoryMin: "512Mi", MemoryMax: "2Gi",
				VolumeMin: "5Gi", VolumeMax: "20Gi",
			},
			want: &AppSpecResources{
				CpuMin: "500m", CpuMax: "2000m",
				MemoryMin: "512Mi", MemoryMax: "2Gi",
				VolumeMin: "5Gi", VolumeMax: "20Gi",
			},
		},
		{
			name: "range_wins_over_legacy",
			in: &AppSpecResources{
				CpuLimit: "999m", CpuMin: "100m", CpuMax: "300m",
				MemoryLimit: "999Mi", MemoryMin: "128Mi", MemoryMax: "256Mi",
				VolumeLimit: "999Gi", VolumeMin: "1Gi", VolumeMax: "5Gi",
			},
			want: &AppSpecResources{
				CpuMin: "100m", CpuMax: "300m",
				MemoryMin: "128Mi", MemoryMax: "256Mi",
				VolumeMin: "1Gi", VolumeMax: "5Gi",
			},
		},
		{
			name: "min_only_fills_max",
			in:   &AppSpecResources{CpuMin: "500m", MemoryMin: "512Mi", VolumeMin: "10Gi"},
			want: &AppSpecResources{CpuMin: "500m", CpuMax: "500m",
				MemoryMin: "512Mi", MemoryMax: "512Mi",
				VolumeMin: "10Gi", VolumeMax: "10Gi"},
		},
		{
			name: "max_only_fills_min",
			in:   &AppSpecResources{CpuMax: "2000m", MemoryMax: "2Gi", VolumeMax: "20Gi"},
			want: &AppSpecResources{CpuMin: "2000m", CpuMax: "2000m",
				MemoryMin: "2Gi", MemoryMax: "2Gi",
				VolumeMin: "20Gi", VolumeMax: "20Gi"},
		},
		{
			name: "all_empty_stays_empty",
			in:   &AppSpecResources{},
			want: &AppSpecResources{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.NormalizeLegacy()
			if !resourcesEqual(tc.in, tc.want) {
				t.Errorf("got %+v, want %+v", tc.in, tc.want)
			}
		})
	}

	// Idempotent: running twice must not change the result. Compare via the
	// pointer-based helper (the protobuf message embeds a mutex, so it must
	// not be copied by value).
	t.Run("idempotent", func(t *testing.T) {
		r := &AppSpecResources{CpuLimit: "500m", MemoryLimit: "512Mi", VolumeLimit: "10Gi"}
		r.NormalizeLegacy()
		first := snapshotResources(r)
		r.NormalizeLegacy()
		if !snapshotEqual(snapshotResources(r), first) {
			t.Errorf("not idempotent:\n first = %+v\n second = %+v", first, snapshotResources(r))
		}
	})
}

func TestNormalizeLegacyNilSafe(t *testing.T) {
	var r *AppSpecResources
	r.NormalizeLegacy() // must not panic
}

// resourcesEqual compares only the user-visible resource fields of two
// AppSpecResources values for equality (legacy fields are always "" after
// normalize, so they are compared too).
func resourcesEqual(a, b *AppSpecResources) bool {
	if a == nil || b == nil {
		return a == b
	}
	return snapshotEqual(snapshotResources(a), snapshotResources(b))
}

// resourceSnapshot is a plain copy of the user-visible AppSpecResources
// string fields, used to compare values without copying the embedded
// protobuf mutex.
type resourceSnapshot struct {
	cpuLimit, memoryLimit, volumeLimit           string
	cpuMin, cpuMax                               string
	memoryMin, memoryMax                         string
	volumeMin, volumeMax                         string
}

func snapshotResources(r *AppSpecResources) resourceSnapshot {
	return resourceSnapshot{
		cpuLimit: r.CpuLimit, memoryLimit: r.MemoryLimit, volumeLimit: r.VolumeLimit,
		cpuMin: r.CpuMin, cpuMax: r.CpuMax,
		memoryMin: r.MemoryMin, memoryMax: r.MemoryMax,
		volumeMin: r.VolumeMin, volumeMax: r.VolumeMax,
	}
}

func snapshotEqual(a, b resourceSnapshot) bool {
	return a == b
}
