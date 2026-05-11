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

	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/inutil"
)

// NewPkgListCommand creates the "pkg-list" command for listing packages.
// Displays packages stored on the zonelet server in a formatted table.
func NewPkgListCommand() *cobra.Command {

	var (
		addr       string
		showJson   bool
		showAll    bool
		filterName string
		filterVer  string
		filterOs   string
		filterArch string
		latestOnly bool
	)

	runE := func(cmd *cobra.Command, args []string) error {
		zone, err := Config.Zone(addr)
		if err != nil {
			return err
		}

		// Connect to zonelet server
		ak, err := zone.AccessKey()
		if err != nil {
			return fmt.Errorf("invalid access key: %w", err)
		}

		conn, err := client.Connect(zone.Addr, ak, false)
		if err != nil {
			return fmt.Errorf("failed to connect to server %s: %w", zone.Addr, err)
		}

		zc := inapi.NewZoneServiceClient(conn)

		// Build list request with filters
		req := &inapi.PackageListRequest{
			All:        showAll,
			Name:       filterName,
			Version:    filterVer,
			Os:         filterOs,
			Arch:       filterArch,
			LatestOnly: latestOnly,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Query package list from server
		resp, err := zc.PackageList(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list packages: %w", err)
		}

		var tbuf bytes.Buffer

		if !showJson && len(resp.Packages) > 0 {
			// Create output table with left-aligned headers
			tableBase := tablewriter.NewTable(&tbuf)

			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
			})

			// Define table columns
			headers := []any{"Name", "Version", "OS", "Arch", "Size", "Built", "Updated"}
			if showAll {
				// Show upload progress column when --all flag is set
				headers = append(headers, "Progress")
			}
			tableBase.Header(headers...)

			// Populate table rows
			for _, pkg := range resp.Packages {
				if pkg.Metadata == nil || pkg.Release == nil || pkg.File == nil {
					continue
				}

				// Format build timestamp
				builtStr := "-"
				if pkg.Release.Built > 0 {
					builtStr = time.Unix(pkg.Release.Built, 0).Format("2006-01-02 15:04")
				}

				// Format update timestamp
				updatedStr := "-"
				if pkg.File.Updated > 0 {
					updatedStr = time.Unix(pkg.File.Updated, 0).Format("2006-01-02 15:04")
				}

				// Build row data
				row := []any{
					pkg.Metadata.Name,
					pkg.Release.Version,
					pkg.Release.Os,
					pkg.Release.Arch,
					inutil.PrettyBytes(pkg.Release.Size, 1024),
					builtStr,
					updatedStr,
				}

				// Calculate and append upload progress if --all flag is set
				if showAll {
					if pkg.File.State == inapi.PackageFileStateComplete {
						row = append(row, "100%")
					} else if pkg.File.ChunkSize > 0 && pkg.File.Size > 0 {
						totalChunks := (pkg.File.Size + pkg.File.ChunkSize - 1) / pkg.File.ChunkSize
						progress := (len(pkg.File.UploadedChunks) * 100) / int(totalChunks)
						row = append(row, fmt.Sprintf("%d%%", progress))
					} else {
						row = append(row, "0%")
					}
				}
				tableBase.Append(row...)
			}

			tableBase.Render()
		} else if showJson {
			// Output raw JSON response
			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)
		} else {
			tbuf.WriteString("No packages found\n")
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "pkg-list",
		Short: "List packages on zonelet server",
		Long: `List all packages stored on the zonelet server.

Displays package name, version, OS, architecture, size, build time, and update time.
By default, only shows fully uploaded packages. Use --all to include incomplete uploads.

Filter options:
  --name    Filter by exact package name
  --version Filter by version with fuzzy matching (e.g., "2.0" matches 2.0.x)
  --os      Filter by operating system (e.g., "linux", "darwin")
  --arch    Filter by architecture (e.g., "amd64", "arm64")
  --latest  Show only the latest version for each (name, os, arch) combination`,
		RunE: runE,
		Example: `  # List complete packages from local server (default)
  cli pkg-list

  # List all packages including incomplete uploads
  cli pkg-list --all

  # List packages from remote server
  cli pkg-list --addr 192.168.1.100:9533

  # Filter by package name
  cli pkg-list --name myapp

  # Filter by version (fuzzy match: 2.0 matches 2.0.0, 2.0.1, etc.)
  cli pkg-list --version 2.0

  # Filter by exact version
  cli pkg-list --version 2.0.0

  # Filter by OS and architecture
  cli pkg-list --os linux --arch amd64

  # Combine multiple filters
  cli pkg-list --name myapp --version 2.0 --os linux --arch amd64

  # Show only the latest version for each (name, os, arch) combination
  cli pkg-list --name myapp --latest

  # Show latest version for a specific os/arch combination
  cli pkg-list --name myapp --os linux --arch amd64 --latest

  # Show raw JSON output
  cli pkg-list --json`,
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", "", "Zonelet server address")
	cmd.Flags().BoolVarP(&showJson, "json", "j", false, "Output in JSON format")
	cmd.Flags().BoolVarP(&showAll, "all", "", false, "Show all packages including incomplete uploads")
	cmd.Flags().StringVar(&filterName, "name", "", "Filter by package name (exact match)")
	cmd.Flags().StringVar(&filterVer, "version", "", "Filter by version (fuzzy match, e.g., \"2.0\" matches 2.0.x)")
	cmd.Flags().StringVar(&filterOs, "os", "", "Filter by operating system (e.g., linux, darwin)")
	cmd.Flags().StringVar(&filterArch, "arch", "", "Filter by architecture (e.g., amd64, arm64)")
	cmd.Flags().BoolVarP(&latestOnly, "latest", "l", false, "Show only the latest version for each (name, os, arch) combination (requires --name)")

	return cmd
}
