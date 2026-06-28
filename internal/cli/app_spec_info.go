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

	"github.com/hooto/htoml4g/htoml"
	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
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

			tableBase := tablewriter.NewTable(&tbuf)

			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
			})

			tableBase.Header([]any{
				"Name", "Version", "Image",
				"CPU Limit", "Memory Limit", "Volume Limit",
				"Packages", "Tasks",
			}...)

			for _, spec := range resp.Items {
				if spec == nil {
					continue
				}

				cpuLimit, memLimit, volLimit := "-", "-", "-"
				if spec.Resources != nil {
					if v, err := inutil.ParseCPUs(spec.Resources.CpuLimit); err == nil {
						cpuLimit = inutil.PrettyCPUs(v)
					} else if spec.Resources.CpuLimit != "" {
						cpuLimit = spec.Resources.CpuLimit
					}
					if v, err := inutil.ParseBytes(spec.Resources.MemoryLimit); err == nil {
						memLimit = inutil.PrettyBytes(v, 1024)
					} else if spec.Resources.MemoryLimit != "" {
						memLimit = spec.Resources.MemoryLimit
					}
					if v, err := inutil.ParseBytes(spec.Resources.VolumeLimit); err == nil {
						volLimit = inutil.PrettyBytes(v, 1024)
					} else if spec.Resources.VolumeLimit != "" {
						volLimit = spec.Resources.VolumeLimit
					}
				}

				tableBase.Append([]any{
					spec.Name,
					spec.Version,
					spec.Image,
					cpuLimit,
					memLimit,
					volLimit,
					fmt.Sprintf("%d", len(spec.Packages)),
					fmt.Sprintf("%d", len(spec.Tasks)),
				}...)
			}

			tableBase.Render()
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
