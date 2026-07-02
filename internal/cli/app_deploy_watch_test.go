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

package cli

import (
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// instanceWithStage builds an AppInstance whose replica 0 has a successful
// task_run stage stamped at stageRev, and whose root/deploy carry the given
// revisions. Used to exercise the renderDeployStages sync-barrier guard.
func instanceWithStage(rootRev, deployRev, stageRev uint64) *inapi.AppInstance {
	inst := &inapi.AppInstance{
		Deploy: &inapi.AppDeploy{
			Revision:   deployRev,
			ReplicaCap: 1,
		},
	}
	root := inst.Deploy.StagesRoot()
	root.Revision = rootRev

	rep := root.ReplicaStage(0)
	task := rep.Child(inapi.AppDeployStageNameTaskRun, inapi.AppStageOwnerInagent)
	task.SetSuccess("")
	task.Revision = stageRev
	return inst
}

// TestRenderDeployStagesRevisionGuard verifies the watch does not treat the
// stage tree as terminal when the root revision lags the desired Deploy.Revision
// (stale stages from a prior deploy), and does evaluate normally once synced.
func TestRenderDeployStagesRevisionGuard(t *testing.T) {
	const terminal = inapi.AppDeployStageNameTaskRun

	// Stale: root at rev 1, deploy at rev 2. The replica's task_run success
	// belongs to the previous revision and must NOT trigger a premature done.
	inst := instanceWithStage(1, 2, 1)
	gotTerminal, gotFailed, gotFallback := renderDeployStages(
		inst, nil, "app", "start", terminal, 0, 0)
	if gotTerminal || gotFailed || gotFallback {
		t.Fatalf("stale root: got (terminal=%v, failed=%v, fallback=%v), want all false",
			gotTerminal, gotFailed, gotFallback)
	}

	// Synced: root at rev 2 == deploy rev 2. The task_run success now counts
	// and the deploy is terminal.
	inst = instanceWithStage(2, 2, 2)
	gotTerminal, gotFailed, _ = renderDeployStages(
		inst, nil, "app", "start", terminal, 0, 0)
	if !gotTerminal {
		t.Fatal("synced root with task_run success: expected terminal=true")
	}
	if gotFailed {
		t.Fatal("synced root: expected failed=false")
	}
}
