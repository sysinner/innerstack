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
	"github.com/sysinner/incore/v2/internal/client"
)

// NewPkgDelCommand creates the "pkg-del" command for deleting packages.
// Removes a package and all its associated data chunks from the server.
func NewPkgDelCommand() *cobra.Command {

	var (
		addr string
	)

	var runE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("package ID is required")
		}

		pkgId := args[0]

		if pkgId == "" {
			return fmt.Errorf("package ID cannot be empty")
		}

		zone, err := Config.Zone(addr)
		if err != nil {
			return err
		}

		// Connect to zonelet server
		conn, err := client.Connect(zone.Addr, zone.AccessKey(), false)
		if err != nil {
			return fmt.Errorf("failed to connect to server %s: %w", zone.Addr, err)
		}

		zc := inapi.NewZoneServiceClient(conn)

		// Build delete request
		req := &inapi.PackageDeleteRequest{
			Id: pkgId,
		}

		fmt.Printf("Deleting package %s from %s...\n", pkgId, zone.Addr)

		// Send delete request to server
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		resp, err := zc.PackageDelete(ctx, req)
		cancel()

		if err != nil {
			return fmt.Errorf("failed to delete package: %w", err)
		}

		// Display deletion result
		fmt.Printf("Package deleted successfully!\n")
		fmt.Printf("  ID: %s\n", resp.Id)
		fmt.Printf("  Chunks deleted: %d\n", resp.ChunksDeleted)

		return nil
	}

	cmd := &cobra.Command{
		Use:   "pkg-del <package-id>",
		Short: "Delete a package from zonelet server",
		Long: `Delete a package and all its data chunks from the zonelet server.

Package ID format: {name}_{version}_{os}_{arch}
Example: myapp_1.0.0_linux_amd64

WARNING: This operation is irreversible. All package data will be permanently removed.`,
		Args: cobra.ExactArgs(1),
		RunE: runE,
		Example: `  # Delete a package from local server
  cli pkg-del myapp_1.0.0_linux_amd64

  # Delete from remote server
  cli pkg-del myapp_1.0.0_linux_amd64 --addr 192.168.1.100:9533`,
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", "", "Zonelet server address")

	return cmd
}
