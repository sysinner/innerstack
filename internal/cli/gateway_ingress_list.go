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
)

func NewGatewayIngressListCommand() *cobra.Command {

	var (
		zoneAddr string
		showJson bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		zone, err := Config.Zone(zoneAddr)
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
		defer conn.Close()

		zc := inapi.NewZoneServiceClient(conn)

		req := &inapi.GatewayIngressListRequest{}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.GatewayIngressList(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list gateway ingress: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if !showJson && len(resp.Items) > 0 {

			tableBase := tablewriter.NewTable(&tbuf)

			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
			})

			tableBase.Header([]any{
				"Domain", "Description", "Action", "Routes", "Updated",
			}...)

			for _, v := range resp.Items {
				if v.Meta == nil {
					continue
				}

				routes := fmt.Sprintf("%d", len(v.Routes))
				values := []any{
					v.Domain,
					v.Description,
					v.Action,
					routes,
					time.Unix(v.Meta.Updated, 0).Format(time.DateOnly),
				}
				tableBase.Append(values...)
			}

			tableBase.Render()
		} else {
			tbuf.WriteString(fmt.Sprintf("List gateway ingress '%s' successfully\n", zone.Addr))

			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "gw-ingress-list",
		Short: "List all gateway ingress rules",
		RunE:  run,
	}

	cmd.Flags().StringVarP(&zoneAddr, "zone-addr", "a", "", "Zone server address")
	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "show raw response with json")

	return cmd
}
