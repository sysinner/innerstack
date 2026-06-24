// Copyright 2015 Eryx <evorly at gmail dot com>, All rights reserved.
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
	"encoding/json"
	"os"
	"path"
	"testing"
	"time"

	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
	"github.com/sysinner/incore/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/pkg/inapi"
)

// newRevisionTestRep builds an AppReplicaInstance whose only meaningful
// field for containerSpecReset is Deploy.Revision; all other spec fields
// (image, cpu, memory, ports, vpc, lxcfs) are set to match a running
// container so that the revision check is the sole reset trigger.
func newRevisionTestRep(name string, replicaId uint32, revision uint64) *inapi.AppReplicaInstance {
	const image = "test/image:latest"
	return &inapi.AppReplicaInstance{
		App: &inapi.AppInstance{
			Meta: &inapi.Metadata{Name: name},
			Spec: &inapi.AppSpec{
				Image:     image,
				Resources: &inapi.AppSpecResources{},
			},
			Deploy: &inapi.AppDeploy{
				Revision:    revision,
				CpuLimit:    1000,
				MemoryLimit: 1 << 30,
			},
		},
		Replica: &inapi.AppDeployReplica{
			Id:      replicaId,
			VpcIpv4: "",
		},
	}
}

// seedContainer registers a running container in the host cache with
// resource fields matching the replica built by newRevisionTestRep, so
// only the revision check can drive a reset decision.
func seedContainer(t *testing.T, rep *inapi.AppReplicaInstance) {
	t.Helper()
	hoststatus.ContainerList.Store(rep.ContainerName(), &hostapi.ContainerInfo{
		Name:            rep.ContainerName(),
		Image:           rep.App.Spec.Image,
		State:           inapi.OpStateRunning,
		CpuLimit:        rep.App.Deploy.CpuLimit,
		MemoryLimit:     rep.App.Deploy.MemoryLimit,
		Ports:           map[int32]hostapi.PortBinding{},
		LastInspectTime: time.Now().Unix(),
	})
}

// writeAppReplicaToDisk writes app_replica.json for rep (with rep's current
// revision) under the configured AppPath, simulating the file a real create
// leaves behind. Used to exercise the on-disk ground-truth path.
func writeAppReplicaToDisk(t *testing.T, rep *inapi.AppReplicaInstance) {
	t.Helper()
	appPaths := hostapi.NewContainerPath(config.Config.Hostlet.AppPath, rep.ContainerName())
	if err := os.MkdirAll(appPaths.InnerStackDir(), 0755); err != nil {
		t.Fatalf("mkdir innerstack: %v", err)
	}
	if err := inutil.JsonEncodeToFileIndent(appPaths.AppReplicaFile(), rep, 0644); err != nil {
		t.Fatalf("write app_replica.json: %v", err)
	}
}

func TestContainerSpecResetRevision(t *testing.T) {
	// Point AppPath at a temp dir so the on-disk ground-truth path is live.
	prevAppPath := config.Config.Hostlet.AppPath
	config.Config.Hostlet.AppPath = t.TempDir()
	t.Cleanup(func() { config.Config.Hostlet.AppPath = prevAppPath })

	tests := []struct {
		name        string
		appliedRev  uint64 // in-memory cache revision (0 = no entry)
		diskRev     uint64 // on-disk app_replica.json revision (0 = no file)
		incomingRev uint64
		wantReset   bool
	}{
		{"revision_increment_resets", 2, 2, 3, true},
		{"same_revision_no_reset", 3, 3, 3, false},
		{"older_incoming_no_reset", 3, 3, 2, false},
		// Bootstrap / hostlet restart with no in-memory cache: the on-disk
		// file is the ground truth. A bump (disk 2 < incoming 3) must still
		// trigger a recreate; no bump (disk 3 == incoming 3) must not.
		{"missing_cache_disk_current_no_reset", 0, 3, 3, false},
		{"missing_cache_disk_behind_resets", 0, 2, 3, true},
		// Stale-low cache (persistence fell behind disk): disk says the
		// container is already current, so no spurious recreate.
		{"stale_cache_disk_current_no_reset", 2, 3, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep := newRevisionTestRep(tt.name, 1, tt.incomingRev)
			seedContainer(t, rep)

			if tt.appliedRev > 0 {
				hoststatus.Active.SetAppliedRevision(rep.ContainerName(), tt.appliedRev)
			}
			if tt.diskRev > 0 {
				diskRep := newRevisionTestRep(tt.name, 1, tt.diskRev)
				writeAppReplicaToDisk(t, diskRep)
			}

			got := containerSpecReset(rep)
			if got != tt.wantReset {
				t.Fatalf("containerSpecReset = %v, want %v", got, tt.wantReset)
			}
		})
	}
}

// TestHostActiveConfigAppliedRevisionsRoundTrip verifies that the
// last-applied revision map survives JSON marshal/unmarshal, i.e. a
// Deploy.Revision increment issued while the hostlet was down remains
// detectable after restart (TryRun loads the file into hoststatus.Active,
// which containerSpecReset consults).
func TestHostActiveConfigAppliedRevisionsRoundTrip(t *testing.T) {
	src := hoststatus.HostActiveConfig{}
	src.SetAppliedRevision("i8k_myapp_1", 7)
	src.SetAppliedRevision("i8k_other_2", 1)

	data, err := json.Marshal(&src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var dst hoststatus.HostActiveConfig
	if err := json.Unmarshal(data, &dst); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for name, want := range map[string]uint64{"i8k_myapp_1": 7, "i8k_other_2": 1} {
		got, ok := dst.AppliedRevision(name)
		if !ok {
			t.Errorf("entry %q missing after round-trip", name)
		} else if got != want {
			t.Errorf("entry %q = %d, want %d", name, got, want)
		}
	}
}

// TestProvisionInnerStackUpdatesInagent verifies that provisionInnerStack
// writes the .innerstack files and, crucially, refreshes the inagent binary
// when called again against an already-initialized .innerstack mount (e.g. a
// host-side inagent upgrade picked up on the next start).
func TestProvisionInnerStackUpdatesInagent(t *testing.T) {
	tmp := t.TempDir()
	appBasePath := path.Join(tmp, "apps")
	prefix := tmp

	// Stage a host-side inagent source binary (Go build, amd64 fallback).
	srcInagent := path.Join(prefix, "bin", "inagent-linux-amd64")
	if err := os.MkdirAll(path.Dir(srcInagent), 0755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.WriteFile(srcInagent, []byte("inagent-v1"), 0755); err != nil {
		t.Fatalf("write inagent source: %v", err)
	}

	// Point hostlet config at the temp tree.
	prevAppPath := config.Config.Hostlet.AppPath
	prevPrefix := config.Prefix
	config.Config.Hostlet.AppPath = appBasePath
	config.Prefix = prefix
	t.Cleanup(func() {
		config.Config.Hostlet.AppPath = prevAppPath
		config.Prefix = prevPrefix
	})

	rep := newRevisionTestRep("provapp", 1, 5)

	// First provision: initializes .innerstack and writes inagent.
	if err := provisionInnerStack(rep); err != nil {
		t.Fatalf("first provisionInnerStack: %v", err)
	}

	appPaths := hostapi.NewContainerPath(appBasePath, rep.ContainerName())
	for _, p := range []string{
		appPaths.AppReplicaFile(),
		appPaths.IninitFile(),
		appPaths.InagentFile(),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s after provision: %v", p, err)
		}
	}

	got, err := os.ReadFile(appPaths.InagentFile())
	if err != nil {
		t.Fatalf("read inagent: %v", err)
	}
	if string(got) != "inagent-v1" {
		t.Fatalf("inagent content = %q, want %q", got, "inagent-v1")
	}

	// Simulate a host-side inagent upgrade.
	if err := os.WriteFile(srcInagent, []byte("inagent-v2"), 0755); err != nil {
		t.Fatalf("upgrade inagent source: %v", err)
	}

	// Second provision against the already-initialized .innerstack must
	// overwrite inagent with the upgraded binary.
	if err := provisionInnerStack(rep); err != nil {
		t.Fatalf("second provisionInnerStack: %v", err)
	}

	got, err = os.ReadFile(appPaths.InagentFile())
	if err != nil {
		t.Fatalf("read inagent after re-provision: %v", err)
	}
	if string(got) != "inagent-v2" {
		t.Fatalf("inagent content = %q, want %q (not refreshed)", got, "inagent-v2")
	}
}
