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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/internal/client"
	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func NewAppListCommand() *cobra.Command {

	var (
		showJson bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		zone, err := Config.Zone("")
		if err != nil {
			return err
		}

		ak, err := zone.AccessKey()
		if err != nil {
			return fmt.Errorf("invalid access key: %w", err)
		}

		conn, err := client.Connect(zone.Addr, ak, false)
		if err != nil {
			return fmt.Errorf("failed to connect to zone server %s: %w", zone.Addr, err)
		}

		zc := inapi.NewZoneServiceClient(conn)

		req := &inapi.AppInstanceListRequest{}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.AppInstanceList(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list app instances: %s", err.Error())
		}

		var tbuf bytes.Buffer

		switch {
		case showJson:
			fmt.Fprintf(&tbuf, "List app instances '%s' successfully\n", zone.Addr)
			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)

		case len(resp.Items) == 0:
			tbuf.WriteString("No app instances found\n")

		default:
			tableBase := tablewriter.NewTable(&tbuf)

			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
				config.Header.Formatting.AutoFormat = tw.Off
			})

			tableBase.Header([]any{
				"Name",
				"Image", "Version",
				"Status", "Action", "Replicas",
				"CPU", "Memory", "Volume",
				"Host Id", "Age",
			}...)

			for _, v := range resp.Items {
				if v.Meta == nil {
					v.Meta = &inapi.Metadata{}
				}
				if v.Spec == nil {
					v.Spec = &inapi.AppSpec{}
				}
				if v.Deploy == nil {
					v.Deploy = &inapi.AppDeploy{}
				}

				hostId := ""
				for _, rep := range v.Deploy.Replicas {
					if rep.HostId != "" {
						if hostId != "" {
							hostId += ", "
						}
						hostId += rep.HostId
					}
				}

				values := []any{
					v.InstanceName(),
					strOrDash(v.Spec.Image),
					strOrDash(v.Spec.Version),
					appListStatus(v.Deploy),
					strOrDash(v.Deploy.Action),
					appListReplicas(v.Deploy),
					cpuOrDash(v.Deploy.CpuLimit),
					bytesOrDash(v.Deploy.MemoryLimit),
					bytesOrDash(v.Deploy.VolumeLimit),
					hostId,
					appListAge(v),
				}

				tableBase.Append(values...)
			}

			tableBase.Render()
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "app-list",
		Short: "List all app instances",
		Long: `List all application instances deployed in the current zone.

Displays name, image/version, status, action, ready/desired replicas, resource
limits, host placement and age. Use app-info for full detail on a single
instance.

  # List all app instances
  cli app-list

  # Show raw JSON output
  cli app-list --show-json`,
		RunE: run,
	}

	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "show raw response with json")

	return cmd
}

// appListStatus returns the observed lifecycle state of an instance, taken
// from the root deploy stage when available, falling back to the user-set
// action when no stage has recorded a state yet, and finally "-".
func appListStatus(deploy *inapi.AppDeploy) string {
	if deploy != nil && deploy.Stages != nil && deploy.Stages.State != "" {
		return deploy.Stages.State
	}
	if deploy != nil && deploy.Action != "" {
		return deploy.Action
	}
	return "-"
}

// replicaStageRunning reports whether a per-replica stage node has reached the
// container_running stage, and that no later container_stop/container_destroy
// stage has superseded it. The zonelet persists only stage progress (never the
// replica State field), so this child stage is the authoritative zone-side
// signal that a replica's container is actually running.
func replicaStageRunning(repNode *inapi.AppDeployStage) bool {
	if repNode == nil {
		return false
	}
	run := repNode.Find(inapi.AppDeployStageNameContainerRunning)
	if run == nil || run.State != inapi.AppStageStateSuccess {
		return false
	}
	for _, name := range []string{
		inapi.AppDeployStageNameContainerStop,
		inapi.AppDeployStageNameContainerDestroy,
	} {
		if s := repNode.Find(name); s != nil &&
			s.State == inapi.AppStageStateSuccess && s.Finished >= run.Finished {
			return false
		}
	}
	return true
}

// appListReplicas returns a "ready/desired" summary. Ready counts the replicas
// whose container has reached the running stage; desired defaults to ReplicaCap
// and falls back to the placed replica count when ReplicaCap is unset.
func appListReplicas(deploy *inapi.AppDeploy) string {
	if deploy == nil {
		return "-"
	}
	desired := deploy.ReplicaCap
	if desired == 0 {
		desired = uint32(len(deploy.Replicas))
	}
	var ready uint32
	if deploy.Stages != nil {
		for _, repNode := range deploy.Stages.Stages {
			if repNode == nil || repNode.Name != inapi.AppDeployStageNameReplica {
				continue
			}
			if replicaStageRunning(repNode) {
				ready++
			}
		}
	}
	return fmt.Sprintf("%d/%d", ready, desired)
}

// appListAge returns a compact relative age (e.g. "3d 2h") since the instance
// was created. It prefers Meta.Created and, for instances created before that
// field was stamped, falls back to the deploy root stage creation time.
func appListAge(inst *inapi.AppInstance) string {
	var createdSec int64
	if inst != nil {
		if inst.Meta != nil && inst.Meta.Created > 0 {
			createdSec = inst.Meta.Created
		} else if inst.Deploy != nil && inst.Deploy.Stages != nil &&
			inst.Deploy.Stages.Created > 0 {
			createdSec = inst.Deploy.Stages.Created / 1000
		}
	}
	if createdSec <= 0 {
		return "-"
	}
	return inutil.FormatUptime(time.Now().Unix() - createdSec)
}
