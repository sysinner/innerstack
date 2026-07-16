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
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/internal/client"
	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func NewAppInfoCommand() *cobra.Command {

	var (
		instanceName string
		showJson     bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		if instanceName == "" {
			return fmt.Errorf("instance name is required")
		}

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

		req := &inapi.AppInstanceInfoRequest{
			Name: instanceName,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.AppInstanceInfo(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to get app instance: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if !showJson && resp.Instance != nil {
			renderAppInstance(&tbuf, resp.Instance)
		} else {
			fmt.Fprintf(&tbuf, "Get app instance '%s' successfully\n", instanceName)

			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "app-info",
		Short: "Show app instance details",
		RunE:  run,
	}

	cmd.Flags().StringVarP(&instanceName, "name", "n", "", "App instance name (required)")
	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "show raw response with json")

	cmd.MarkFlagRequired("name")

	return cmd
}

// renderAppInstance writes a multi-section, human-readable view of an app
// instance to buf, modeled after `kubectl describe`: an Overview key/value
// block followed by per-concern tables (resources, replicas, dependencies,
// packages, deploy stages). Empty sections are skipped to keep output tidy.
func renderAppInstance(buf *bytes.Buffer, inst *inapi.AppInstance) {
	if inst == nil {
		return
	}

	meta := inst.Meta
	if meta == nil {
		meta = &inapi.Metadata{}
	}
	spec := inst.Spec
	if spec == nil {
		spec = &inapi.AppSpec{}
	}
	deploy := inst.Deploy
	if deploy == nil {
		deploy = &inapi.AppDeploy{}
	}

	// --- Overview ---
	specVer := spec.Name
	if spec.Version != "" {
		if specVer != "" {
			specVer += " / "
		} else {
			specVer = "/ "
		}
		specVer += spec.Version
	}

	buf.WriteString("\n== Overview ==\n")
	writeKV(buf, "Name", strOrDash(inst.InstanceName()))
	writeKV(buf, "Spec/Version", strOrDash(specVer))
	writeKV(buf, "Image", strOrDash(spec.Image))
	writeKV(buf, "Action", strOrDash(deploy.Action))
	writeKV(buf, "Replicas", fmt.Sprintf("%d / %d", len(deploy.Replicas), deploy.ReplicaCap))
	writeKV(buf, "User", strOrDash(meta.User))
	writeKV(buf, "Created", cliTime(meta.Created))
	writeKV(buf, "Updated", cliTime(meta.Updated))

	// --- Resources (always shown; "-" when unset) ---
	buf.WriteString("\n== Resources ==\n")
	rt := cliNewTable(buf)
	rt.Header([]any{"CPU Limit", "Memory Limit", "Volume Limit"})
	rt.Append([]any{
		cpuOrDash(deploy.CpuLimit),
		bytesOrDash(deploy.MemoryLimit),
		bytesOrDash(deploy.VolumeLimit),
	}...)
	rt.Render()

	// --- Replicas ---
	if len(deploy.Replicas) > 0 {
		metrics := replicaMetricsMap(inst)
		buf.WriteString("\n== Replicas ==\n")
		vt := cliNewTable(buf)
		vt.Header([]any{"ID", "State", "Host Id", "Host IP", "VPC IP", "Ports",
			"CPU", "Memory", "Net (read/write)", "Uptime"})
		for _, r := range deploy.Replicas {
			if r == nil {
				continue
			}
			m := metrics[r.Id]
			vt.Append([]any{
				fmt.Sprintf("%d", r.Id),
				strOrDash(r.State),
				strOrDash(r.HostId),
				strOrDash(r.HostIpv4),
				strOrDash(r.VpcIpv4),
				cliPorts(r.ServicePorts),
				replicaCPU(m),
				replicaMem(m),
				replicaNet(m),
				replicaUptime(m),
			}...)
		}
		vt.Render()
	}

	// --- Dependencies (forward bindings + reverse refs) ---
	if len(deploy.Depends) > 0 || len(inst.RefByInstances) > 0 {
		buf.WriteString("\n== Dependencies ==\n")
		if len(deploy.Depends) > 0 {
			dt := cliNewTable(buf)
			dt.Header([]any{"Spec Name", "Instance Name"})
			for _, d := range deploy.Depends {
				if d == nil {
					continue
				}
				dt.Append([]any{
					strOrDash(d.SpecName),
					strOrDash(d.InstanceName),
				}...)
			}
			dt.Render()
		}
		if len(inst.RefByInstances) > 0 {
			fmt.Fprintf(buf, "Referenced by: %s\n", strings.Join(inst.RefByInstances, ", "))
		}
	}

	// --- Packages ---
	if len(spec.Packages) > 0 {
		buf.WriteString("\n== Packages ==\n")
		pt := cliNewTable(buf)
		pt.Header([]any{"Name", "Version"})
		for _, p := range spec.Packages {
			if p == nil {
				continue
			}
			pt.Append([]any{
				strOrDash(p.Name),
				strOrDash(p.Version),
			}...)
		}
		pt.Render()
	}

	// --- Deploy Stages (recursive, depth-first) ---
	if deploy.Stages != nil {
		var rows [][]any
		cliAppendStages(&rows, deploy.Stages, 0)
		if len(rows) > 0 {
			buf.WriteString("\n== Deploy Stages ==\n")
			st := cliNewTable(buf)
			st.Header([]any{"Name", "Owner", "State", "Attempt", "Duration", "Message"})
			for _, row := range rows {
				st.Append(row...)
			}
			st.Render()
		}
	}
}

// cliNewTable returns a left-aligned tablewriter writing to buf. Trimming of
// leading/trailing spaces is disabled so that indent prefixes (e.g. nested
// deploy stages) are preserved in the rendered cells.
func cliNewTable(buf *bytes.Buffer) *tablewriter.Table {
	t := tablewriter.NewTable(buf)
	t.Configure(func(config *tablewriter.Config) {
		config.Header.Alignment.Global = tw.AlignLeft
		config.Behavior.TrimSpace = tw.Off
	})
	return t
}

// writeKV writes a left-aligned " Key : value" line, padding the key column
// so the colons line up across the Overview block.
func writeKV(buf *bytes.Buffer, key, val string) {
	fmt.Fprintf(buf, " %-14s: %s\n", key, val)
}

func strOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func cpuOrDash(millicores int64) string {
	if millicores <= 0 {
		return "-"
	}
	return inutil.PrettyCPUs(millicores)
}

func bytesOrDash(b int64) string {
	if b <= 0 {
		return "-"
	}
	return inutil.PrettyBytes(b, 1024)
}

// cliTime formats a unix-seconds timestamp as a local readable string.
func cliTime(unixSec int64) string {
	if unixSec <= 0 {
		return "-"
	}
	return time.Unix(unixSec, 0).Local().Format("2006-01-02 15:04:05")
}

// cliPorts renders a replica's service ports as a comma-separated list of
// "name:port->host_port" entries.
func cliPorts(ports []*inapi.AppDeployServicePort) string {
	if len(ports) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		if p == nil {
			continue
		}
		s := ""
		if p.Name != "" {
			s = p.Name + ":"
		}
		s += fmt.Sprintf("%d", p.Port)
		if p.HostPort > 0 {
			s += "->" + fmt.Sprintf("%d", p.HostPort)
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

// replicaMetricsMap indexes the per-replica runtime metrics carried on
// Status.Replicas by replica id, so the deploy-replica view can join in the
// latest usage. Returns an empty map when no metrics have been reported.
func replicaMetricsMap(inst *inapi.AppInstance) map[uint32]*inapi.NodeMetrics {
	out := map[uint32]*inapi.NodeMetrics{}
	if inst == nil || inst.Status == nil {
		return out
	}
	for _, r := range inst.Status.Replicas {
		if r == nil || r.Metrics == nil {
			continue
		}
		out[r.Id] = r.Metrics
	}
	return out
}

// replicaCPU renders a replica's CPU usage as cores, derived from the 60s
// windowed ms counters (cores = (cpu_user + cpu_sys) / 60000). "-" when no
// usage has been reported.
func replicaCPU(m *inapi.NodeMetrics) string {
	if m == nil || (m.CpuUser == 0 && m.CpuSys == 0) {
		return "-"
	}
	cores := float64(m.CpuUser+m.CpuSys) / 60000.0
	return inutil.PrettyCPUs(int64(cores * 1000))
}

// replicaMem renders a replica's instantaneous memory usage. "-" when no usage
// has been reported.
func replicaMem(m *inapi.NodeMetrics) string {
	if m == nil || m.MemUsed <= 0 {
		return "-"
	}
	return inutil.PrettyBytes(m.MemUsed, 1024)
}

// replicaNet renders a replica's receive/transmit throughput, derived from its
// 60s windowed byte counters (bytes/sec = window delta / 60). "-" when no
// traffic has been reported.
func replicaNet(m *inapi.NodeMetrics) string {
	if m == nil || (m.NetRecvBytes <= 0 && m.NetSentBytes <= 0) {
		return "-"
	}
	return netRate(m.NetRecvBytes/60, m.NetSentBytes/60)
}

// replicaUptime renders a replica's container uptime. "-" when unknown.
func replicaUptime(m *inapi.NodeMetrics) string {
	if m == nil || m.Uptime <= 0 {
		return "-"
	}
	return inutil.FormatUptime(m.Uptime)
}

// cliStageDuration returns a readable duration between created and finished
// millisecond timestamps, or "--" when the stage has not reached a terminal
// state yet.
func cliStageDuration(createdMs, finishedMs int64) string {
	if finishedMs <= 0 || finishedMs < createdMs || createdMs <= 0 {
		return "--"
	}
	ms := finishedMs - createdMs
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%.1fs", float64(ms)/1000.0)
	default:
		return inutil.FormatUptime(ms / 1000)
	}
}

// cliAppendStages recursively flattens the nested deploy-stage tree into
// table rows in depth-first order, indenting each child stage's name by two
// spaces per depth level.
func cliAppendStages(rows *[][]any, stage *inapi.AppDeployStage, depth int) {
	if stage == nil {
		return
	}
	name := stage.Name
	if depth > 0 {
		name = strings.Repeat("  ", depth) + name
	}
	*rows = append(*rows, []any{
		strOrDash(name),
		strOrDash(stage.Owner),
		strOrDash(stage.State),
		fmt.Sprintf("%d", stage.Attempt),
		cliStageDuration(stage.Created, stage.Finished),
		clipMsg(stage.Message),
	})
	for _, child := range stage.Stages {
		cliAppendStages(rows, child, depth+1)
	}
}

// clipMsg collapses newlines and clips overly long stage messages so a single
// verbose entry does not blow out the table width. The full text is available
// via --show-json.
func clipMsg(msg string) string {
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	msg = strings.TrimSpace(msg)
	if len(msg) > 80 {
		msg = msg[:77] + "..."
	}
	if msg == "" {
		return "-"
	}
	return msg
}
