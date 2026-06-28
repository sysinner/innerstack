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

package task

import (
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func resetExecStatuses() {
	for k := range execStatuses {
		delete(execStatuses, k)
	}
}

func TestOnStartupAggregate(t *testing.T) {
	cases := []struct {
		name   string
		app    *inapi.AppReplicaInstance
		setup  func()
		wantSt string
	}{
		{
			name:   "no tasks at all",
			app:    &inapi.AppReplicaInstance{App: &inapi.AppInstance{Spec: &inapi.AppSpec{}}},
			setup:  func() {},
			wantSt: inapi.AppStageStateSuccess,
		},
		{
			name: "no on_startup tasks",
			app: &inapi.AppReplicaInstance{App: &inapi.AppInstance{Spec: &inapi.AppSpec{
				Tasks: []*inapi.AppSpecTask{{Name: "cron1", Cron: "*/5 * * * *"}},
			}}},
			setup:  func() {},
			wantSt: inapi.AppStageStateSuccess, // nothing to wait for -> ready
		},
		{
			name: "on_startup task not yet run",
			app: &inapi.AppReplicaInstance{App: &inapi.AppInstance{Spec: &inapi.AppSpec{
				Tasks: []*inapi.AppSpecTask{{Name: "boot1", OnStartup: true}},
			}}},
			setup:  func() {}, // execStatuses empty -> pending
			wantSt: inapi.AppStageStateRunning,
		},
		{
			name: "on_startup task succeeded",
			app: &inapi.AppReplicaInstance{App: &inapi.AppInstance{Spec: &inapi.AppSpec{
				Tasks: []*inapi.AppSpecTask{{Name: "boot1", OnStartup: true}},
			}}},
			setup: func() {
				execStatuses["boot1"] = &executorStatus{DoneUpdated: 1000}
			},
			wantSt: inapi.AppStageStateSuccess,
		},
		{
			name: "on_startup task failed",
			app: &inapi.AppReplicaInstance{App: &inapi.AppInstance{Spec: &inapi.AppSpec{
				Tasks: []*inapi.AppSpecTask{{Name: "boot1", OnStartup: true}},
			}}},
			setup: func() {
				execStatuses["boot1"] = &executorStatus{DoneUpdated: 500, FailUpdated: 1000}
			},
			wantSt: inapi.AppStageStateFailed,
		},
		{
			name: "one of two on_startup tasks still pending",
			app: &inapi.AppReplicaInstance{App: &inapi.AppInstance{Spec: &inapi.AppSpec{
				Tasks: []*inapi.AppSpecTask{
					{Name: "boot1", OnStartup: true},
					{Name: "boot2", OnStartup: true},
				},
			}}},
			setup: func() {
				execStatuses["boot1"] = &executorStatus{DoneUpdated: 1000}
				// boot2 not run yet -> pending
			},
			wantSt: inapi.AppStageStateRunning,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resetExecStatuses()
			c.setup()
			st, _ := OnStartupAggregate(c.app)
			if st != c.wantSt {
				t.Fatalf("state=%q want %q", st, c.wantSt)
			}
		})
	}
}
