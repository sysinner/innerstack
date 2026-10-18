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

	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// parseSinceSeconds resolves a --since duration flag into seconds; empty is
// 0, invalid or non-positive values error.
func parseSinceSeconds(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid --since duration: %s", s)
	}
	return int64(d.Seconds()), nil
}

func newHostMetricsCommand() *cobra.Command {

	var (
		field    string
		since    string
		step     string
		showJson bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		if len(args) == 0 {
			return fmt.Errorf("host_id required")
		}

		_, zc, err := zoneClient()
		if err != nil {
			return err
		}

		req := &inapi.HostMetricsRequest{
			HostId: args[0],
			Field:  field,
		}
		if secs, err := parseSinceSeconds(since); err != nil {
			return err
		} else if secs > 0 {
			req.StartTime = time.Now().Unix() - secs
		}
		if secs, err := parseSinceSeconds(step); err != nil {
			return fmt.Errorf("invalid --step duration: %s", step)
		} else if secs > 0 {
			req.StepSeconds = secs
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := zc.HostMetrics(ctx, req)
		if err != nil {
			return fmt.Errorf("host metrics failed: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if !showJson {
			tableBase := tablewriter.NewTable(&tbuf)
			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
				config.Header.Formatting.AutoWrap = tw.WrapNormal
				config.Header.Formatting.AutoFormat = tw.Off
			})
			tableBase.Header([]any{"Field", "Time", "Value"}...)

			for _, series := range resp.Series {
				for _, pt := range series.Points {
					tableBase.Append([]any{
						series.Field,
						cliTime(pt.Time),
						fmt.Sprintf("%d (%s)", pt.Value,
							inutil.PrettyBytes(pt.Value, 1024)),
					}...)
				}
			}
			tableBase.Render()
		} else {
			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "metrics <host_id>",
		Short: "Query built-in host metrics history (monstore)",
		RunE:  run,
	}

	cmd.Flags().StringVar(&field, "field", "",
		"metric field (cpu_millicores, mem_used, ...); empty lists all")
	cmd.Flags().StringVar(&since, "since", "",
		"time window, e.g. 24h or 30m; default whole retention")
	cmd.Flags().StringVar(&step, "step", "",
		"downsample step, e.g. 5m; default raw samples")
	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false,
		"show raw response with json")

	return cmd
}
