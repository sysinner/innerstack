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
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func NewGatewayIngressInfoCommand() *cobra.Command {

	var (
		showJson bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		if len(args) == 0 {
			return fmt.Errorf("ingress name is required")
		}

		ingressName := args[0]

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
		defer conn.Close()

		zc := inapi.NewZoneServiceClient(conn)

		req := &inapi.GatewayIngressInfoRequest{
			Name: ingressName,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.GatewayIngressInfo(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to get gateway ingress: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if !showJson && resp.Item != nil && resp.Item.Meta != nil {

			tableBase := tablewriter.NewTable(&tbuf)

			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
			})

			tableBase.Header([]any{
				"Domain", "Description", "Action", "Path", "Type", "Targets",
			}...)

			if len(resp.Item.Routes) == 0 {
				tableBase.Append([]any{
					resp.Item.Domain,
					resp.Item.Description,
					resp.Item.Action,
					"", "", "",
				})
			} else {
				for i, route := range resp.Item.Routes {
					targets := formatTargetAddrs(route.Targets)
					if i == 0 {
						tableBase.Append([]any{
							resp.Item.Domain,
							resp.Item.Description,
							resp.Item.Action,
							route.Path,
							route.Type,
							targets,
						})
					} else {
						tableBase.Append([]any{
							"", "", "",
							route.Path,
							route.Type,
							targets,
						})
					}
				}
			}

			tableBase.Render()
		} else {
			tbuf.WriteString(fmt.Sprintf("Get gateway ingress '%s' successfully\n", ingressName))

			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "gw-ingress-info [name]",
		Short: "Show gateway ingress details",
		RunE:  run,
	}

	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "show raw response with json")

	return cmd
}
