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

func TestAppDeployStagesRoot(t *testing.T) {
	var d AppDeploy
	if d.Stages != nil {
		t.Fatal("initial stages should be nil")
	}
	root := d.StagesRoot()
	if root == nil || root.Name != AppDeployStageNameDeploy {
		t.Fatalf("unexpected root: %+v", root)
	}
	if d.Stages != root {
		t.Fatal("StagesRoot did not attach root to AppDeploy")
	}
	// Second call returns the same root.
	if d.StagesRoot() != root {
		t.Fatal("StagesRoot returned a different root on second call")
	}
}

func TestAppDeployStageChildFind(t *testing.T) {
	root := &AppDeployStage{}
	c1 := root.Child("a", AppStageOwnerZonelet)
	c2 := root.Child("a", AppStageOwnerZonelet)
	if c1 != c2 {
		t.Fatal("Child should be idempotent")
	}
	if c1.Owner != AppStageOwnerZonelet {
		t.Fatalf("owner=%q want %q", c1.Owner, AppStageOwnerZonelet)
	}
	if root.Find("a") != c1 {
		t.Fatal("Find should return existing child")
	}
	if root.Find("missing") != nil {
		t.Fatal("Find of missing should be nil")
	}
	if len(root.Stages) != 1 {
		t.Fatalf("expected 1 child, got %d", len(root.Stages))
	}
}

func TestAppDeployStageSetRunningSuccessFailed(t *testing.T) {
	s := &AppDeployStage{}

	// First SetRunning sets created and running state.
	s.SetRunning("go")
	if s.State != AppStageStateRunning {
		t.Fatalf("state=%s want running", s.State)
	}
	if s.Created == 0 {
		t.Fatal("created should be set on first run")
	}
	if s.Finished != 0 {
		t.Fatal("finished should be 0 while running")
	}
	created := s.Created

	// SetSuccess records finish time, keeps created.
	s.SetSuccess("done")
	if s.State != AppStageStateSuccess {
		t.Fatalf("state=%s want success", s.State)
	}
	if s.Created != created {
		t.Fatal("created must not change on success")
	}
	if s.Finished < created {
		t.Fatal("finished must be >= created")
	}

	// A retry path: failed -> running increments attempt.
	s.SetFailed("boom")
	if s.State != AppStageStateFailed {
		t.Fatalf("state=%s want failed", s.State)
	}
	if s.Attempt != 0 {
		t.Fatalf("attempt=%d want 0", s.Attempt)
	}
	s.SetRunning("retry")
	if s.Attempt != 1 {
		t.Fatalf("attempt=%d want 1 after failed->running", s.Attempt)
	}
	if s.State != AppStageStateRunning {
		t.Fatalf("state=%s want running", s.State)
	}
}

func TestAppDeployStageSetInstant(t *testing.T) {
	s := &AppDeployStage{}
	s.SetInstant("evt")
	if s.State != AppStageStateSuccess {
		t.Fatalf("state=%s want success", s.State)
	}
	if s.Created == 0 || s.Finished == 0 {
		t.Fatal("instant stage must set both created and finished")
	}
	if s.Created != s.Finished {
		t.Fatal("instant stage created should equal finished")
	}
}

func TestAppDeployStageReplicaStage(t *testing.T) {
	root := &AppDeployStage{}
	r3 := root.ReplicaStage(3)
	if r3 == nil || r3.Name != AppDeployStageNameReplica {
		t.Fatal("ReplicaStage should create replica node")
	}
	if r3.Attrs[AppDeployStageReplicaAttrRepId] != "3" {
		t.Fatalf("rep_id attr=%q want 3", r3.Attrs[AppDeployStageReplicaAttrRepId])
	}
	// Same repId returns same node.
	if root.ReplicaStage(3) != r3 {
		t.Fatal("ReplicaStage not idempotent for same repId")
	}
	// Different repId returns a new node.
	r4 := root.ReplicaStage(4)
	if r4 == r3 {
		t.Fatal("ReplicaStage returned same node for different repId")
	}
	if len(root.Stages) != 2 {
		t.Fatalf("expected 2 replica nodes, got %d", len(root.Stages))
	}
}

func TestAppDeployStagePruneChildren(t *testing.T) {
	root := &AppDeployStage{}
	root.Child(AppDeployStageNameSchedule, AppStageOwnerZonelet)
	root.Child(AppDeployStageNameHostRecv, AppStageOwnerHostlet)
	root.Child(AppDeployStageNameImagePull, AppStageOwnerHostlet)

	root.PruneChildren(func(name string) bool {
		_, isHost := AppDeployStageHostSideNames[name]
		return !isHost // keep non-host-side, drop host-side
	})

	if root.Find(AppDeployStageNameSchedule) == nil {
		t.Fatal("zone-side child should be kept")
	}
	if root.Find(AppDeployStageNameHostRecv) != nil {
		t.Fatal("host-side child should be pruned")
	}
	if root.Find(AppDeployStageNameImagePull) != nil {
		t.Fatal("host-side child should be pruned")
	}
}

func TestStageNowMsMonotonic(t *testing.T) {
	a := stageNowMs()
	b := stageNowMs()
	if b < a {
		t.Fatalf("stageNowMs not monotonic: %d > %d", a, b)
	}
}
