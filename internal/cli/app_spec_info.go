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

	"github.com/hooto/htoml4g/htoml"
	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/internal/client"
	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// NewAppSpecInfoCommand creates the "app-spec-info" command for looking up an
// application spec by name. It calls AppSpecList with a name filter on the
// zonelet server.
func NewAppSpecInfoCommand() *cobra.Command {

	var (
		specName string
		showJson bool
		showToml bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		if specName == "" {
			return fmt.Errorf("spec name is required")
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

		req := &inapi.AppSpecListRequest{
			Name: specName,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.AppSpecList(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list app specs: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if showToml {
			if len(resp.Items) == 0 {
				fmt.Fprintf(&tbuf, "app-spec %q not found\n", specName)
			} else {
				out, err := htoml.Encode(resp.Items[0])
				if err != nil {
					return fmt.Errorf("failed to encode app spec as TOML: %w", err)
				}
				tbuf.Write(out)
			}
		} else if !showJson && len(resp.Items) > 0 {
			renderAppSpec(&tbuf, resp.Items[0])
		} else if showJson {
			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)
		} else {
			fmt.Fprintf(&tbuf, "app-spec %q not found\n", specName)
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "app-spec-info",
		Short: "Show app spec details by name",
		Long: `Look up an application specification by name from the zonelet server.

Internally calls AppSpecList with a name filter, returning the matching spec.

  # Show details of a specific app spec
  cli app-spec-info --name myapp

  # Show the spec as TOML (same format accepted by app-deploy --spec)
  cli app-spec-info --name myapp -t

  # Show raw JSON output
  cli app-spec-info --name myapp --show-json`,
		RunE: run,
	}

	cmd.Flags().StringVarP(&specName, "name", "n", "", "App spec name (required)")
	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "show raw response with json")
	cmd.Flags().BoolVarP(&showToml, "toml", "t", false, "output the spec in TOML format")

	cmd.MarkFlagRequired("name")

	return cmd
}

// renderAppSpec writes a multi-section, human-readable view of an app spec to
// buf, mirroring the layout of app-info: an Overview key/value block followed
// by per-concern tables (resources, depends, service ports, packages, configs,
// tasks). Empty sections are skipped to keep output tidy.
func renderAppSpec(buf *bytes.Buffer, spec *inapi.AppSpec) {
	if spec == nil {
		return
	}

	res := spec.Resources
	if res == nil {
		res = &inapi.AppSpecResources{}
	}

	// --- Overview ---
	buf.WriteString("\n== Overview ==\n")
	writeKV(buf, "Name", strOrDash(spec.Name))
	writeKV(buf, "Version", strOrDash(spec.Version))
	writeKV(buf, "Image", strOrDash(spec.Image))
	writeKV(buf, "Description", strOrDash(spec.Description))

	// --- Resources (always shown; "-" when unset) ---
	buf.WriteString("\n== Resources ==\n")
	rt := cliNewTable(buf)
	rt.Header([]any{"CPU", "Memory", "Volume"})
	rt.Append([]any{
		specRangeCpu(res.CpuMin, res.CpuMax),
		specRangeBytes(res.MemoryMin, res.MemoryMax),
		specRangeBytes(res.VolumeMin, res.VolumeMax),
	}...)
	rt.Render()

	// --- Depends (app + service level, distinguished by Kind) ---
	if len(spec.Depends) > 0 || len(spec.ServiceDepends) > 0 {
		buf.WriteString("\n== Depends ==\n")
		dt := cliNewTable(buf)
		dt.Header([]any{"Kind", "Name", "Version"})
		for _, d := range spec.Depends {
			if d == nil {
				continue
			}
			dt.Append([]any{"app", strOrDash(d.Name), strOrDash(d.Version)}...)
		}
		for _, d := range spec.ServiceDepends {
			if d == nil {
				continue
			}
			dt.Append([]any{"service", strOrDash(d.Name), strOrDash(d.Version)}...)
		}
		dt.Render()
	}

	// --- Service Ports ---
	if len(spec.ServicePorts) > 0 {
		buf.WriteString("\n== Service Ports ==\n")
		st := cliNewTable(buf)
		st.Header([]any{"Name", "Port"})
		for _, p := range spec.ServicePorts {
			if p == nil {
				continue
			}
			st.Append([]any{strOrDash(p.Name), fmt.Sprintf("%d", p.Port)}...)
		}
		st.Render()
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
			pt.Append([]any{strOrDash(p.Name), strOrDash(p.Version)}...)
		}
		pt.Render()
	}

	// --- Configs (recursive, depth-first) ---
	if len(spec.Configs) > 0 {
		var rows [][]any
		for _, c := range spec.Configs {
			cliAppendConfigItems(&rows, c, 0)
		}
		if len(rows) > 0 {
			buf.WriteString("\n== Configs ==\n")
			ct := cliNewTable(buf)
			ct.Header([]any{"Name", "Title", "Type", "Default"})
			for _, row := range rows {
				ct.Append(row...)
			}
			ct.Render()
		}
	}

	// --- Tasks ---
	if len(spec.Tasks) > 0 {
		buf.WriteString("\n== Tasks ==\n")
		tt := cliNewTable(buf)
		tt.Header([]any{"Name", "Trigger", "Script"})
		for _, task := range spec.Tasks {
			if task == nil {
				continue
			}
			tt.Append([]any{
				strOrDash(task.Name),
				cliTaskTrigger(task),
				clipMsg(task.Script),
			}...)
		}
		tt.Render()
	}
}

// specCpu pretty-prints a CPU resource string (e.g. "500m") from an app spec,
// falling back to the raw text when it cannot be parsed.
func specCpu(raw string) string {
	if raw == "" {
		return "-"
	}
	v, err := inutil.ParseCPUs(raw)
	if err != nil {
		return raw
	}
	return inutil.PrettyCPUs(v)
}

// specBytes pretty-prints a byte-quantity resource string (e.g. "512Mi") from
// an app spec, falling back to the raw text when it cannot be parsed.
func specBytes(raw string) string {
	if raw == "" {
		return "-"
	}
	v, err := inutil.ParseBytes(raw)
	if err != nil {
		return raw
	}
	return inutil.PrettyBytes(v, 1024)
}

// specRangeCpu formats a CPU min..max range from an app spec, collapsing to a
// single value when min == max (or max is unset). Returns "-" when both are
// empty.
func specRangeCpu(minStr, maxStr string) string {
	return specRange(minStr, maxStr, specCpu)
}

// specRangeBytes formats a memory/volume min..max range from an app spec,
// collapsing to a single value when min == max (or max is unset). Returns "-"
// when both are empty.
func specRangeBytes(minStr, maxStr string) string {
	return specRange(minStr, maxStr, specBytes)
}

// specRange formats a min/max pair using the given pretty-printer. A single
// value is shown when max is empty or equal to min; otherwise "min ~ max".
func specRange(minStr, maxStr string, pretty func(string) string) string {
	if minStr == "" && maxStr == "" {
		return "-"
	}
	if maxStr == "" || minStr == maxStr {
		// Show whichever side is set.
		if minStr != "" {
			return pretty(minStr)
		}
		return pretty(maxStr)
	}
	return pretty(minStr) + " ~ " + pretty(maxStr)
}

// cliTaskTrigger renders the active trigger of a task as a short label. The
// trigger fields are mutually exclusive; the first set one wins.
func cliTaskTrigger(t *inapi.AppSpecTask) string {
	if t == nil {
		return "-"
	}
	switch {
	case t.OnStartup:
		return "on_startup"
	case t.OnShutdown:
		return "on_shutdown"
	case t.IntervalSeconds > 0:
		return fmt.Sprintf("interval=%ds", t.IntervalSeconds)
	case t.Cron != "":
		return "cron=" + t.Cron
	}
	return "-"
}

// cliAppendConfigItems recursively flattens the nested config-item tree into
// table rows in depth-first order, indenting each child item's name by two
// spaces per depth level. array_group items annotate their key_item.
func cliAppendConfigItems(rows *[][]any, item *inapi.AppSpecConfigItem, depth int) {
	if item == nil {
		return
	}
	name := item.Name
	if depth > 0 {
		name = strings.Repeat("  ", depth) + name
	}
	typ := item.Type
	if item.KeyItem != "" {
		if typ != "" {
			typ += " "
		}
		typ += "(key=" + item.KeyItem + ")"
	}
	*rows = append(*rows, []any{
		strOrDash(name),
		strOrDash(item.Title),
		strOrDash(typ),
		strOrDash(item.Default),
	})
	for _, child := range item.Items {
		cliAppendConfigItems(rows, child, depth+1)
	}
}
