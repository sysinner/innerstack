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
	"strconv"
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// repStage builds a per-replica stage node (a direct child of the deploy root)
// carrying the given child stages.
func repStage(id uint32, children ...*inapi.AppDeployStage) *inapi.AppDeployStage {
	return &inapi.AppDeployStage{
		Name:    inapi.AppDeployStageNameReplica,
		Attrs:   map[string]string{inapi.AppDeployStageReplicaAttrRepId: strconv.Itoa(int(id))},
		Stages:  children,
	}
}

// stage builds a named terminal child stage with a state and finished timestamp.
func stage(name, state string, finished int64) *inapi.AppDeployStage {
	return &inapi.AppDeployStage{Name: name, State: state, Finished: finished}
}

// deployWithRoot wraps replica stage nodes under a deploy root.
func deployWithRoot(caps uint32, replicas ...*inapi.AppDeployReplica) func(...*inapi.AppDeployStage) *inapi.AppDeploy {
	return func(repNodes ...*inapi.AppDeployStage) *inapi.AppDeploy {
		return &inapi.AppDeploy{
			ReplicaCap: caps,
			Replicas:   replicas,
			Stages: &inapi.AppDeployStage{
				Name:   inapi.AppDeployStageNameDeploy,
				Stages: repNodes,
			},
		}
	}
}

func TestAppListStatus(t *testing.T) {
	cases := []struct {
		name   string
		deploy *inapi.AppDeploy
		want   string
	}{
		{"nil deploy", nil, "-"},
		{"empty deploy", &inapi.AppDeploy{}, "-"},
		{"action only", &inapi.AppDeploy{Action: "start"}, "start"},
		{"stage wins over action", &inapi.AppDeploy{
			Action: "start",
			Stages: &inapi.AppDeployStage{State: inapi.AppStageStateRunning},
		}, inapi.AppStageStateRunning},
		{"failed stage", &inapi.AppDeploy{
			Stages: &inapi.AppDeployStage{State: inapi.AppStageStateFailed},
		}, inapi.AppStageStateFailed},
		{"empty stage state falls back to action", &inapi.AppDeploy{
			Action:  "stop",
			Stages:  &inapi.AppDeployStage{},
		}, "stop"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := appListStatus(c.deploy); got != c.want {
				t.Fatalf("appListStatus() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAppListReplicas(t *testing.T) {
	mk := deployWithRoot(1) // ReplicaCap = 1, no replica placeholders

	cases := []struct {
		name   string
		deploy *inapi.AppDeploy
		want   string
	}{
		{"nil deploy", nil, "-"},
		{"empty deploy", &inapi.AppDeploy{}, "0/0"},
		{"single running replica", mk(
			repStage(0, stage(inapi.AppDeployStageNameContainerRunning, inapi.AppStageStateSuccess, 100)),
		), "1/1"},
		{"running then stopped is not ready", mk(
			repStage(0,
				stage(inapi.AppDeployStageNameContainerRunning, inapi.AppStageStateSuccess, 100),
				stage(inapi.AppDeployStageNameContainerStop, inapi.AppStageStateSuccess, 200),
			),
		), "0/1"},
		{"one of two running", deployWithRoot(2)(
			repStage(0, stage(inapi.AppDeployStageNameContainerRunning, inapi.AppStageStateSuccess, 100)),
			repStage(1, stage(inapi.AppDeployStageNameContainerStart, inapi.AppStageStateRunning, 0)),
		), "1/2"},
		{"non-replica child ignored", mk(
			&inapi.AppDeployStage{
				Name:  inapi.AppDeployStageNameSchedule,
				Stages: []*inapi.AppDeployStage{
					stage(inapi.AppDeployStageNameContainerRunning, inapi.AppStageStateSuccess, 100),
				},
			},
		), "0/1"},
		{"cap falls back to placed count", func() *inapi.AppDeploy {
			d := mk() // ReplicaCap set to 1 above; override for this case
			d.ReplicaCap = 0
			d.Replicas = []*inapi.AppDeployReplica{{Id: 0}, {Id: 1}}
			return d
		}(), "0/2"},
		{"container_running not yet success", mk(
			repStage(0, stage(inapi.AppDeployStageNameContainerRunning, inapi.AppStageStateRunning, 0)),
		), "0/1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := appListReplicas(c.deploy); got != c.want {
				t.Fatalf("appListReplicas() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAppListAge(t *testing.T) {
	// Only "-" outcomes are asserted exactly; positive ages depend on the
	// wall clock, so we only check they resolve to a non-dash value.
	cases := []struct {
		name string
		inst *inapi.AppInstance
		dash bool // expect "-"
	}{
		{"nil", nil, true},
		{"empty instance", &inapi.AppInstance{}, true},
		{"meta created set", &inapi.AppInstance{
			Meta: &inapi.Metadata{Created: 1_700_000_000},
		}, false},
		{"falls back to deploy root stage created", &inapi.AppInstance{
			Deploy: &inapi.AppDeploy{
				Stages: &inapi.AppDeployStage{Created: 1_700_000_000_000},
			},
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := appListAge(c.inst)
			if c.dash && got != "-" {
				t.Fatalf("appListAge() = %q, want %q", got, "-")
			}
			if !c.dash && got == "-" {
				t.Fatalf("appListAge() = %q, want a non-dash age", got)
			}
		})
	}
}
