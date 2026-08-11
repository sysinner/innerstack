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
	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// NewPkgListCommand creates the "pkg-list" command for listing packages.
// Displays packages stored on the zonelet server in a formatted table.
func NewPkgListCommand() *cobra.Command {

	var (
		showJson   bool
		showAll    bool
		filterName string
		filterVer  string
		filterOs   string
		filterArch string
	)

	runE := func(cmd *cobra.Command, args []string) error {
		zone, err := Config.Zone("")
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
			// --all opts out of latest-only dedup and includes incomplete uploads.
			LatestOnly: !showAll,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Query package list from server
		resp, err := zc.PackageList(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list packages: %w", err)
		}

		var tbuf bytes.Buffer

		if !showJson && len(resp.Items) > 0 {
			// Create output table with left-aligned headers
			tableBase := tablewriter.NewTable(&tbuf)

			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
			})

			// Define table columns
			headers := []any{"Name", "Version", "OS", "Arch", "Size", "Built"}
			tableBase.Header(headers...)

			// Populate table rows
			for _, pkg := range resp.Items {
				if pkg.Metadata == nil || pkg.Release == nil || pkg.File == nil {
					continue
				}

				// Format build timestamp
				builtStr := "-"
				if pkg.Release.Built > 0 {
					builtStr = time.Unix(pkg.Release.Built, 0).Format("2006-01-02")
				}

				// Build row data
				row := []any{
					pkg.Metadata.Name,
					pkg.Release.Version,
					pkg.Release.Os,
					pkg.Release.Arch,
					inutil.PrettyBytes(pkg.Release.Size, 1024),
					builtStr,
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
		Long: `List packages stored on the zonelet server.

By default, shows only the latest version for each (name, os, arch) combination;
packages built for different os/arch are listed as separate entries. Only fully
uploaded packages are included. Use --all to list every version, including
incomplete uploads.

Filter options:
  --name    Filter by exact package name
  --version Filter by version with fuzzy matching (e.g., "2.0" matches 2.0.x)
  --os      Filter by operating system (e.g., "linux", "darwin")
  --arch    Filter by architecture (e.g., "amd64", "arm64")`,
		RunE: runE,
		Example: `  # List the latest version of each package (default)
  cli pkg-list

  # List every version, including incomplete uploads
  cli pkg-list --all

  # List packages from remote server
  cli pkg-list --addr 192.168.1.100:9533

  # Latest version of a specific package
  cli pkg-list --name myapp

  # All versions of a specific package
  cli pkg-list --name myapp --all

  # Filter by version (fuzzy match: 2.0 matches 2.0.0, 2.0.1, etc.)
  cli pkg-list --version 2.0

  # Filter by exact version
  cli pkg-list --version 2.0.0

  # Filter by OS and architecture
  cli pkg-list --os linux --arch amd64

  # Combine multiple filters
  cli pkg-list --name myapp --version 2.0 --os linux --arch amd64

  # Show raw JSON output
  cli pkg-list --json`,
	}

	cmd.Flags().BoolVarP(&showJson, "json", "j", false, "Output in JSON format")
	cmd.Flags().BoolVarP(&showAll, "all", "", false, "List every version, including incomplete uploads")
	cmd.Flags().StringVar(&filterName, "name", "", "Filter by package name (exact match)")
	cmd.Flags().StringVar(&filterVer, "version", "", "Filter by version (fuzzy match, e.g., \"2.0\" matches 2.0.x)")
	cmd.Flags().StringVar(&filterOs, "os", "", "Filter by operating system (e.g., linux, darwin)")
	cmd.Flags().StringVar(&filterArch, "arch", "", "Filter by architecture (e.g., amd64, arm64)")

	return cmd
}
