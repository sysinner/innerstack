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

// NewAppSpecListCommand creates the "app-spec-list" command for listing all
// application specs stored on the zonelet server.
func NewAppSpecListCommand() *cobra.Command {

	var showJson bool

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

		req := &inapi.AppSpecListRequest{}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.AppSpecList(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list app specs: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if !showJson && len(resp.Items) > 0 {

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
			tbuf.WriteString("No app specs found\n")
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "app-spec-list",
		Short: "List all app specs",
		Long: `List all application specifications stored on the zonelet server.

Displays spec name, version, kind, image, resource limits, package and task
counts. Use app-spec-info to look up a single spec by name.

  # List all app specs
  cli app-spec-list

  # Show raw JSON output
  cli app-spec-list --show-json`,
		RunE: run,
	}

	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "show raw response with json")

	return cmd
}
