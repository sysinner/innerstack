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
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func newAppLogsCommand() *cobra.Command {

	var (
		replica uint32
		tail    uint32
		since   string
	)

	run := func(cmd *cobra.Command, args []string) error {

		if len(args) == 0 {
			return fmt.Errorf("instance name required")
		}

		_, zc, err := zoneClient()
		if err != nil {
			return err
		}

		req := &inapi.AppLogsRequest{
			Name:      args[0],
			ReplicaId: replica,
			Tail:      tail,
		}
		if since != "" {
			secs, err := parseSinceSeconds(since)
			if err != nil {
				return err
			}
			req.StartTime = time.Now().Unix() - secs
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := zc.AppLogs(ctx, req)
		if err != nil {
			return fmt.Errorf("app logs failed: %s", err.Error())
		}

		if len(resp.Lines) == 0 {
			fmt.Println("(no log lines)")
		}
		for _, line := range resp.Lines {
			fmt.Println(line)
		}
		if resp.Truncated {
			fmt.Printf("(output truncated at 1MB; narrow --since or --tail)\n")
		}

		return nil
	}

	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Fetch recent logs of an app replica (proxied via the zone leader)",
		RunE:  run,
	}

	cmd.Flags().Uint32Var(&replica, "replica", 0,
		"replica id to read (default 0)")
	cmd.Flags().Uint32Var(&tail, "tail", inapi.AppLogsTailDefault,
		fmt.Sprintf("number of last lines (default %d, max %d)",
			inapi.AppLogsTailDefault, inapi.AppLogsTailMax))
	cmd.Flags().StringVar(&since, "since", "",
		"only show logs newer than this duration, e.g. 1h")

	return cmd
}
