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
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
)

func NewZoneInfoCommand() *cobra.Command {

	var (
		addr string
	)

	var run = func(cmd *cobra.Command, args []string) error {

		// Connect to gRPC server
		conn, err := client.Connect(addr, nil, false)
		if err != nil {
			return fmt.Errorf("failed to connect to server %s: %w", addr, err)
		}

		fmt.Println("addr", addr)

		// Create Zonelet client
		zc := inapi.NewZoneletClient(conn)

		// Set timeout context
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Call ZoneInfo method
		resp, err := zc.ZoneInfo(ctx, &inapi.ZoneInfoRequest{})
		if err != nil {
			return fmt.Errorf("failed to get info: %s", err.Error())
		}

		js, _ := json.MarshalIndent(resp, "", "  ")

		fmt.Printf("Zone %s\n", string(js))

		return nil
	}

	cmd := &cobra.Command{
		Use:   "zone-info",
		Short: "Show zone information",
		RunE:  run,
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", "127.0.0.1:9533", "Zonelet server address")

	return cmd
}
