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
	"net/http"
	"testing"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

const (
	testInstance = "myapp"
	testRepId    = uint32(0)
	testSecret   = "s3cret-key"
)

func testContainerName() string {
	return "i8k_myapp_0"
}

func TestMergeInagentStatusAuth(t *testing.T) {
	hoststatus.Active.SetSecretKey(testContainerName(), testSecret)
	defer hoststatus.Active.DeleteSecretKey(testContainerName())

	cases := []struct {
		name   string
		secret string
		want   int
	}{
		{"valid secret", testSecret, http.StatusOK},
		{"wrong secret", "nope", http.StatusUnauthorized},
		{"missing secret", "", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			report := &inapi.InagentStatusReport{
				InstanceName: testInstance,
				ReplicaId:    testRepId,
			}
			code, _ := mergeInagentStatus(report, c.secret)
			if code != c.want {
				t.Fatalf("code=%d want %d", code, c.want)
			}
		})
	}
}

func TestMergeInagentStatusUnknownContainer(t *testing.T) {
	hoststatus.Active.DeleteSecretKey(testContainerName())
	report := &inapi.InagentStatusReport{
		InstanceName: testInstance,
		ReplicaId:    testRepId,
	}
	code, _ := mergeInagentStatus(report, "anything")
	if code != http.StatusUnauthorized {
		t.Fatalf("code=%d want 401", code)
	}
}

func TestMergeInagentStatusMissingInstance(t *testing.T) {
	code, _ := mergeInagentStatus(&inapi.InagentStatusReport{}, testSecret)
	if code != http.StatusBadRequest {
		t.Fatalf("code=%d want 400", code)
	}
}

func TestMergeInagentStatusMerges(t *testing.T) {
	hoststatus.Active.SetSecretKey(testContainerName(), testSecret)
	defer hoststatus.Active.DeleteSecretKey(testContainerName())
	defer hoststatus.ReplicaStageDelete(testInstance, testRepId)

	stages := []*inapi.AppDeployStage{
		{
			Name:    inapi.AppDeployStageNameSpecLoad,
			Owner:   inapi.AppStageOwnerInagent,
			State:   inapi.AppStageStateSuccess,
			Created: 100, Finished: 200,
		},
		{
			Name:    inapi.AppDeployStageNameTaskRun,
			Owner:   inapi.AppStageOwnerInagent,
			State:   inapi.AppStageStateRunning,
			Created: 300,
		},
	}

	report := &inapi.InagentStatusReport{
		InstanceName: testInstance,
		ReplicaId:    testRepId,
		Stages:       stages,
	}
	code, _ := mergeInagentStatus(report, testSecret)
	if code != http.StatusOK {
		t.Fatalf("code=%d want 200", code)
	}

	entry := hoststatus.ReplicaStage(testInstance, testRepId)

	spec := entry.Stage.Find(inapi.AppDeployStageNameSpecLoad)
	if spec == nil || spec.State != inapi.AppStageStateSuccess {
		t.Fatalf("spec_load not merged: %+v", spec)
	}
	tr := entry.Stage.Find(inapi.AppDeployStageNameTaskRun)
	if tr == nil || tr.State != inapi.AppStageStateRunning {
		t.Fatalf("task_run not merged: %+v", tr)
	}
	if tr.Owner != inapi.AppStageOwnerInagent {
		t.Fatalf("owner=%s want inagent", tr.Owner)
	}
	if !entry.Dirty {
		t.Fatal("entry should be dirty after merge")
	}

	snap := entry.SnapshotIfDirty()
	if snap == nil {
		t.Fatal("expected dirty snapshot")
	}
	entry.ClearDirty()
	if entry.SnapshotIfDirty() != nil {
		t.Fatal("expected no snapshot after clear")
	}
}

func TestInagentEndpointURL(t *testing.T) {
	// Non-empty when both peer address and HTTP port are set.
	config.Config.Hostlet.LanAddr = "10.0.0.5"
	config.Config.Server.HttpPort = 9532
	defer func() {
		config.Config.Hostlet.LanAddr = ""
		config.Config.Server.HttpPort = 0
	}()

	url := inagentEndpointURL()
	want := "http://10.0.0.5:9532" + InagentStatusPath
	if url != want {
		t.Fatalf("url=%q want %q", url, want)
	}
}

func TestReplicaStageSyncRevisionResets(t *testing.T) {
	defer hoststatus.ReplicaStageDelete(testInstance, testRepId)
	re := hoststatus.ReplicaStage(testInstance, testRepId)
	re.SyncRevision(1)
	re.SetInstant(inapi.AppDeployStageNameHostRecv, "")
	if re.Stage.Find(inapi.AppDeployStageNameHostRecv) == nil {
		t.Fatal("host_recv should be set")
	}
	// A new revision clears prior stages.
	if !re.SyncRevision(2) {
		t.Fatal("SyncRevision should reset on change")
	}
	if re.Stage.Find(inapi.AppDeployStageNameHostRecv) != nil {
		t.Fatal("stages should be cleared on revision change")
	}
	if re.Revision != 2 {
		t.Fatalf("revision=%d want 2", re.Revision)
	}
	// Same revision is a no-op.
	if re.SyncRevision(2) {
		t.Fatal("SyncRevision should not reset on same revision")
	}
}

func TestMergeInagentSkipsStaleRevision(t *testing.T) {
	hoststatus.Active.SetSecretKey(testContainerName(), testSecret)
	defer hoststatus.Active.DeleteSecretKey(testContainerName())
	defer hoststatus.ReplicaStageDelete(testInstance, testRepId)

	// Entry is at the current revision 2.
	re := hoststatus.ReplicaStage(testInstance, testRepId)
	re.SyncRevision(2)

	// A reported inagent stage based on the stale revision 1 must be dropped.
	stale := &inapi.AppDeployStage{
		Name:     inapi.AppDeployStageNameSpecLoad,
		Owner:    inapi.AppStageOwnerInagent,
		State:    inapi.AppStageStateSuccess,
		Revision: 1,
	}
	report := &inapi.InagentStatusReport{
		InstanceName: testInstance,
		ReplicaId:    testRepId,
		Stages:       []*inapi.AppDeployStage{stale},
	}
	if code, _ := mergeInagentStatus(report, testSecret); code != http.StatusOK {
		t.Fatalf("code=%d want 200", code)
	}
	if re.Stage.Find(inapi.AppDeployStageNameSpecLoad) != nil {
		t.Fatal("stale-revision stage should be skipped")
	}

	// A current-revision stage is merged.
	fresh := &inapi.AppDeployStage{
		Name:     inapi.AppDeployStageNameSpecLoad,
		Owner:    inapi.AppStageOwnerInagent,
		State:    inapi.AppStageStateSuccess,
		Revision: 2,
	}
	report.Stages = []*inapi.AppDeployStage{fresh}
	mergeInagentStatus(report, testSecret)
	got := re.Stage.Find(inapi.AppDeployStageNameSpecLoad)
	if got == nil {
		t.Fatal("current-revision stage should be merged")
	}
	if got.Revision != 2 {
		t.Fatalf("revision=%d want 2", got.Revision)
	}
}
