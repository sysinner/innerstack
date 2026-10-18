// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// NewAlertCommand returns the "alert" command group for the built-in alert
// rules evaluated by the zone leader.
func NewAlertCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alert",
		Short: "Inspect built-in zone alerts",
	}
	cmd.AddCommand(newAlertListCommand())
	return cmd
}

func newAlertListCommand() *cobra.Command {

	var (
		stateStr string
		rule     string
		showJson bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		_, zc, err := zoneClient()
		if err != nil {
			return err
		}

		req := &inapi.AlertListRequest{
			State: stateStr,
			Rule:  rule,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.AlertList(ctx, req)
		if err != nil {
			return fmt.Errorf("alert list failed: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if !showJson && len(resp.Items) > 0 {
			tableBase := tablewriter.NewTable(&tbuf)
			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
				config.Header.Formatting.AutoWrap = tw.WrapNormal
				config.Header.Formatting.AutoFormat = tw.Off
			})
			tableBase.Header([]any{
				"Rule", "Target", "State",
				"FirstSeen", "LastSeen", "Count", "Message",
			}...)

			for _, v := range resp.Items {
				tableBase.Append([]any{
					v.Rule,
					v.Target,
					v.State,
					cliTime(v.StartTime),
					cliTime(v.UpdateTime),
					fmt.Sprintf("%d", v.BreachCount),
					v.Message,
				}...)
			}
			tableBase.Render()
		} else {
			if len(resp.Items) == 0 {
				tbuf.WriteString("No alerts\n")
			}
			if showJson {
				js, _ := json.MarshalIndent(resp, "", "  ")
				tbuf.Write(js)
			}
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List built-in alert records (host_down, disk_full, replica_crashloop)",
		RunE:  run,
	}

	cmd.Flags().StringVar(&stateStr, "state", "",
		"filter by state: firing | resolved (default all)")
	cmd.Flags().StringVar(&rule, "rule", "",
		"filter by rule id: host_down | disk_full | replica_crashloop (default all)")
	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false,
		"show raw response with json")

	return cmd
}
