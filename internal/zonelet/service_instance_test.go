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

// TestAppDeployStagesReconcile verifies the scheduler-side sync barrier:
// appDeployStagesReconcile lifts root.Revision to the current Deploy.Revision,
// dropping stale relayed replica children while keeping zone-side ones.
func TestAppDeployStagesReconcile(t *testing.T) {

	build := func(rootRev, deployRev uint64) *inapi.AppDeploy {
		deploy := &inapi.AppDeploy{Revision: deployRev}
		root := deploy.StagesRoot()
		root.Revision = rootRev

		// A replica node carrying one zone-side stage (schedule) and one
		// stale relayed stage (task_run) from a prior revision.
		repNode := root.ReplicaStage(0)
		sched := repNode.Child(inapi.AppDeployStageNameSchedule, inapi.AppStageOwnerZonelet)
		sched.SetSuccess("host-a")
		sched.Revision = rootRev

		task := repNode.Child(inapi.AppDeployStageNameTaskRun, inapi.AppStageOwnerInagent)
		task.SetSuccess("")
		task.Revision = rootRev
		return deploy
	}

	t.Run("reconciles_lagging_root", func(t *testing.T) {
		deploy := build(1, 2)
		if !appDeployStagesReconcile(deploy) {
			t.Fatal("expected reconcile to return true for a lagging root")
		}
		root := deploy.Stages
		if root.Revision != 2 {
			t.Fatalf("root.Revision = %d, want 2", root.Revision)
		}
		if root.State != inapi.AppStageStateRunning {
			t.Fatalf("root.State = %q, want %q", root.State, inapi.AppStageStateRunning)
		}

		repNode := root.ReplicaStage(0)
		if repNode.Revision != 2 {
			t.Fatalf("replica node revision = %d, want 2", repNode.Revision)
		}
		// Zone-side child retained and stamped current.
		if s := repNode.Find(inapi.AppDeployStageNameSchedule); s == nil {
			t.Fatal("zone-side schedule stage should be retained")
		} else if s.Revision != 2 {
			t.Fatalf("schedule revision = %d, want 2", s.Revision)
		}
		// Stale relayed child pruned.
		if repNode.Find(inapi.AppDeployStageNameTaskRun) != nil {
			t.Fatal("stale relayed task_run stage should have been pruned")
		}
	})

	t.Run("noop_when_current", func(t *testing.T) {
		deploy := build(2, 2)
		before := deploy.Stages.ReplicaStage(0).Find(inapi.AppDeployStageNameTaskRun)
		if before == nil {
			t.Fatal("test setup: task_run stage missing")
		}
		if appDeployStagesReconcile(deploy) {
			t.Fatal("expected reconcile to return false when root is current")
		}
		// task_run must be untouched (still present) on a no-op.
		if deploy.Stages.ReplicaStage(0).Find(inapi.AppDeployStageNameTaskRun) == nil {
			t.Fatal("task_run stage should not be pruned on a no-op reconcile")
		}
		if deploy.Stages.Revision != 2 {
			t.Fatalf("root.Revision = %d, want 2", deploy.Stages.Revision)
		}
	})

	t.Run("first_create_advances_from_zero", func(t *testing.T) {
		deploy := &inapi.AppDeploy{Revision: 1}
		// Fresh root with no replica nodes yet.
		deploy.StagesRoot().Revision = 0
		if !appDeployStagesReconcile(deploy) {
			t.Fatal("expected reconcile to return true for a fresh deploy")
		}
		if deploy.Stages.Revision != 1 {
			t.Fatalf("root.Revision = %d, want 1", deploy.Stages.Revision)
		}
	})

	t.Run("nil_safe", func(t *testing.T) {
		if appDeployStagesReconcile(nil) {
			t.Fatal("expected false for nil deploy")
		}
	})
}
