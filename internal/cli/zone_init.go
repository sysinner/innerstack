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
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/inapi"
	inapi2 "github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
)

func NewZoneInitCommand() *cobra.Command {

	var (
		name string
		addr string
	)

	var initZoneRun = func(cmd *cobra.Command, args []string) error {
		// Validate parameters
		if err := inapi2.NameValid(name); err != nil {
			return fmt.Errorf("zone name : %s", err.Error())
		}

		if addr == "" {
			addr = "127.0.0.1:9533"
		}

		// Connect to gRPC server
		conn, err := client.Connect(addr, nil, false)
		if err != nil {
			return fmt.Errorf("failed to connect to server %s: %w", addr, err)
		}

		// Create Zonelet client
		zc := inapi.NewZoneletClient(conn)

		// Prepare request
		req := &inapi.ZoneInitRequest{
			Name: name,
		}

		// Set timeout context
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Call ZoneInit method
		_, err = zc.ZoneInit(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to init zone: %w", err)
		}

		// Print success message
		fmt.Printf("Zone '%s' initialized successfully\n", name)

		return nil
	}

	cmd := &cobra.Command{
		Use:   "zone-init",
		Short: "Initialize a new zone",
		Long: `Initialize a new zone with the specified name.
This command connects to the zonelet server and creates a new zone configuration.`,
		RunE: initZoneRun,
		Example: `  # Initialize a zone with default server address (127.0.0.1:9533)
  app zone-init --name firefly

  # Initialize a zone with a specific server address
  app zone-init --name firefly --addr 192.168.1.100:9533`,
	}

	cmd.Flags().StringVarP(&name, "name", "n", "", "Zone name (required)")
	cmd.Flags().StringVarP(&addr, "addr", "a", "", "Zonelet server address (default: 127.0.0.1:9533)")

	// Mark name as required parameter
	cmd.MarkFlagRequired("name")

	return cmd
}
