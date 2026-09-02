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

// TestHostCpuUsedMc anchors the unit alignment between the hostlet's
// windowed cpu-ms counters and the scheduler's millicore totals: one full
// core burns 60000 cpu-ms per 60s window, i.e. 1000 millicores.
func TestHostCpuUsedMc(t *testing.T) {
	tests := []struct {
		name      string
		user, sys int64
		want      int64
	}{
		{"one full core", 60000, 0, 1000},
		{"mixed user and sys", 12000, 6000, 300},
	}
	for _, tt := range tests {
		st := &inapi.HostStatus{CpuUser: tt.user, CpuSys: tt.sys}
		if got := hostCpuUsedMc(st); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}
