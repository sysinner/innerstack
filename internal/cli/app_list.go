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
				"Spec",
				"Action", "Status", "Replicas",
				"CPU", "Memory", "Volume", "Net (read/write)",
				"Uptime",
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

				cpuUsed, memUsed, cpuOk := appAggregateUsage(v)
				rxBytes, txBytes := appAggregateNet(v)

				values := []any{
					v.InstanceName(),
					specNameVersion(v.Spec),
					strOrDash(v.Deploy.Action),
					appListStatus(v.Deploy),
					appListReplicas(v),
					cpuUsageLimit(cpuUsed, v.Deploy.CpuLimit, cpuOk),
					bytesUsageLimit(memUsed, v.Deploy.MemoryLimit),
					bytesOrDash(v.Deploy.VolumeLimit),
					netRate(rxBytes/60, txBytes/60),
					appListUptime(v),
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
//
// A soft-deleted instance (Action == delete) is reported as "deleted"
// regardless of the (frozen, stale) deploy stage state: the hostlet tears the
// container down on delete and the zone does not advance the stage tree for
// deleted instances, so the prior stage state would otherwise mislead.
func appListStatus(deploy *inapi.AppDeploy) string {
	if deploy == nil {
		return "-"
	}
	if deploy.Action == inapi.OpActionDelete {
		return "deleted"
	}
	if deploy.Stages != nil && deploy.Stages.State != "" {
		return deploy.Stages.State
	}
	if deploy.Action != "" {
		return deploy.Action
	}
	return "-"
}

// appListReplicas returns a "ready/desired" summary. Ready is the number of
// replicas with live runtime metrics reported in Status.Replicas -- the hostlet
// only collects metrics for running containers, so a replica carrying metrics is
// a running one. Desired is the deploy ReplicaCap, which the scheduler
// normalizes to at least 1.
func appListReplicas(inst *inapi.AppInstance) string {
	if inst == nil || inst.Deploy == nil {
		return "-"
	}
	desired := inst.Deploy.ReplicaCap
	var ready uint32
	if inst.Status != nil {
		for _, r := range inst.Status.Replicas {
			if r != nil && r.Metrics != nil {
				ready++
			}
		}
	}
	return fmt.Sprintf("%d/%d", ready, desired)
}

// specNameVersion renders an app spec as "name:version", falling back to
// whichever component is set, or "-" when neither is.
func specNameVersion(spec *inapi.AppSpec) string {
	if spec == nil {
		return "-"
	}
	switch {
	case spec.Name != "" && spec.Version != "":
		return spec.Name + ":" + spec.Version
	case spec.Name != "":
		return spec.Name
	case spec.Version != "":
		return spec.Version
	}
	return "-"
}

// appListUptime returns the runtime uptime of the instance's replicas, read
// from the in-memory metrics the zone leader attaches to Status.Replicas (the
// container uptime each hostlet reports). For multi-replica instances it
// reports the youngest (minimum) replica uptime so a recent restart surfaces in
// the aggregate; "-" when no replica has reported metrics yet.
func appListUptime(inst *inapi.AppInstance) string {
	if inst == nil || inst.Status == nil {
		return "-"
	}
	var min int64 = -1
	for _, r := range inst.Status.Replicas {
		if r == nil {
			continue
		}
		if m := r.Metrics; m != nil && m.Uptime > 0 {
			if min < 0 || m.Uptime < min {
				min = m.Uptime
			}
		}
	}
	if min < 0 {
		return "-"
	}
	return inutil.FormatUptime(min)
}

// appAggregateUsage sums the latest per-replica runtime usage for an instance
// into aggregate CPU (millicores) and memory (bytes), read from the in-memory
// metrics the zone leader attaches to Status.Replicas. has reports whether any
// replica contributed metrics, so callers can distinguish a genuine zero usage
// reading from "no data yet" (freshly deployed, metrics not propagated, etc.).
func appAggregateUsage(inst *inapi.AppInstance) (cpuMc, memBytes int64, has bool) {
	if inst == nil || inst.Status == nil {
		return
	}
	for _, r := range inst.Status.Replicas {
		if r == nil {
			continue
		}
		if m := r.Metrics; m != nil {
			has = true
			// cpu_user + cpu_sys are ms of CPU consumed over the 60s window;
			// millicores = ms / 60.
			cpuMc += (m.CpuUser + m.CpuSys) / 60
			memBytes += m.MemUsed
		}
	}
	return
}

// cpuUsageLimit renders CPU usage against the deploy limit. When realtime usage
// was reported (usedOk, even if it is 0) it shows "used/limit" (e.g. "0m/1" for
// an idle container). When no realtime data is available it shows "-", so an
// idle-but-measured replica is distinguishable from one with no metrics.
func cpuUsageLimit(usedMc, limitMc int64, usedOk bool) string {
	if !usedOk {
		return "-"
	}
	if limitMc > 0 {
		return inutil.PrettyCPUs(usedMc) + "/" + inutil.PrettyCPUs(limitMc)
	}
	return inutil.PrettyCPUs(usedMc)
}

// appAggregateNet sums the latest per-replica network counters (60s-windowed
// deltas) into aggregate received/sent byte counts.
func appAggregateNet(inst *inapi.AppInstance) (rxBytes, txBytes int64) {
	if inst == nil || inst.Status == nil {
		return
	}
	for _, r := range inst.Status.Replicas {
		if r == nil {
			continue
		}
		if m := r.Metrics; m != nil {
			rxBytes += m.NetRecvBytes
			txBytes += m.NetSentBytes
		}
	}
	return
}

// netRate formats receive/transmit throughputs (bytes/sec, already divided by
// the metrics window) as "read, write" -- i.e. socket read (recv, rx) then
// socket write (send, tx); "-" when neither direction has traffic.
func netRate(rxBytesPerSec, txBytesPerSec int64) string {
	if rxBytesPerSec <= 0 && txBytesPerSec <= 0 {
		return "-"
	}
	return inutil.PrettyBytes(rxBytesPerSec, 1024) + "/s, " +
		inutil.PrettyBytes(txBytesPerSec, 1024) + "/s"
}

// bytesUsageLimit renders byte usage against the deploy limit as "used/limit",
// falling back to the limit alone when no usage has been reported yet, and "-"
// when neither is set.
func bytesUsageLimit(used, limit int64) string {
	switch {
	case used > 0 && limit > 0:
		return inutil.PrettyBytes(used, 1024) + "/" + inutil.PrettyBytes(limit, 1024)
	case used > 0:
		return inutil.PrettyBytes(used, 1024)
	default:
		return bytesOrDash(limit)
	}
}
