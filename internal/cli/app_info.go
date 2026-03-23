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

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/inutil"
)

func NewAppInfoCommand() *cobra.Command {

	var (
		zoneAddr string
		showJson bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		if len(args) == 0 {
			return fmt.Errorf("instance id is required")
		}

		instanceId := args[0]

		conn, err := client.Connect(zoneAddr, nil, false)
		if err != nil {
			return fmt.Errorf("failed to connect to zone server %s: %w", zoneAddr, err)
		}

		zc := inapi.NewZoneletClient(conn)

		req := &inapi.AppInstanceInfoRequest{
			Id: instanceId,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.AppInstanceInfo(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to get app instance: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if !showJson && resp.Instance != nil {

			tableBase := tablewriter.NewTable(&tbuf)

			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
			})

			tableBase.Header([]any{
				"Id", "Name",
				"CPU Limit", "Memory Limit", "Volume Limit",
			}...)

			values := []any{
				resp.Instance.Id, resp.Instance.Name,
				inutil.PrettyCPUs(resp.Instance.Deploy.CpuLimit),
				inutil.PrettyBytes(resp.Instance.Deploy.MemoryLimit, 1024),
				inutil.PrettyBytes(resp.Instance.Deploy.VolumeLimit, 1024),
			}

			tableBase.Append(values...)
			tableBase.Render()
		} else {
			tbuf.WriteString(fmt.Sprintf("Get app instance '%s' successfully\n", instanceId))

			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "app-info [instance-id]",
		Short: "Show app instance details",
		RunE:  run,
	}

	cmd.Flags().StringVarP(&zoneAddr, "zone-addr", "a", "127.0.0.1:9533", "Zone server address")
	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "show raw response with json")

	return cmd
}
