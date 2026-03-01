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

func NewHostJoinCommand() *cobra.Command {

	var (
		zoneAddr  string
		hostAddr  string
		hostToken string
	)

	run := func(cmd *cobra.Command, args []string) error {

		// Connect to gRPC server
		conn, err := client.Connect(zoneAddr, nil, false)
		if err != nil {
			return fmt.Errorf("failed to connect to zone server %s: %w", zoneAddr, err)
		}

		zc := inapi.NewZoneletClient(conn)

		req := &inapi.HostJoinRequest{
			Addr:  hostAddr,
			Token: hostToken,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.HostJoin(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to join host: %s", err.Error())
		}

		// Print success message
		fmt.Printf("Join host '%s' successfully\n", hostAddr)

		js, _ := json.MarshalIndent(resp.Status, "", "  ")
		fmt.Println("Host Status : " + string(js))

		return nil
	}

	cmd := &cobra.Command{
		Use:   "host-join",
		Short: "Join a host to the zone",
		RunE:  run,
	}

	cmd.Flags().StringVarP(&hostAddr, "addr", "a", "", "Host address (required)")
	cmd.Flags().StringVarP(&hostToken, "token", "t", "", "Host Access Token (required)")
	cmd.Flags().StringVarP(&zoneAddr, "zone-addr", "z", "127.0.0.1:9533", "Zone server address")

	cmd.MarkFlagRequired("addr")
	cmd.MarkFlagRequired("token")

	return cmd
}
