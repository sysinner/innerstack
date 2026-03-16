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

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/inutil"
)

func NewPkgListCommand() *cobra.Command {

	var (
		addr     string
		showJson bool
		showAll  bool
	)

	runE := func(cmd *cobra.Command, args []string) error {

		conn, err := client.Connect(addr, nil, false)
		if err != nil {
			return fmt.Errorf("failed to connect to server %s: %w", addr, err)
		}

		zc := inapi.NewZoneletClient(conn)

		req := &inapi.PackageListRequest{
			All: showAll,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.PackageList(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to list packages: %w", err)
		}

		var tbuf bytes.Buffer

		if !showJson && len(resp.Packages) > 0 {

			tableBase := tablewriter.NewTable(&tbuf)

			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
			})

			headers := []any{"Name", "Version", "OS", "Arch", "Size"}
			if showAll {
				headers = append(headers, "Progress")
			}
			tableBase.Header(headers...)

			for _, pkg := range resp.Packages {
				if pkg.Metadata == nil || pkg.Release == nil || pkg.File == nil {
					continue
				}

				row := []any{
					pkg.Metadata.Name,
					pkg.Release.Version,
					pkg.Release.Os,
					pkg.Release.Arch,
					inutil.PrettyBytes(pkg.Release.Size, 1024),
				}
				if showAll {
					if pkg.File.State == inapi.PackageFileStateComplete {
						row = append(row, fmt.Sprintf("%d%%", 100))
					} else if pkg.File.ChunkSize > 0 && pkg.Release.Size > 0 {
						totalChunks := (pkg.Release.Size + pkg.File.ChunkSize - 1) / pkg.File.ChunkSize
						row = append(row, fmt.Sprintf("%d%%", (len(pkg.File.UploadedChunks)*100)/int(totalChunks)))
					} else {
						row = append(row, fmt.Sprintf("%d%%", 0))
					}
				}
				tableBase.Append(row...)
			}

			tableBase.Render()
		} else if showJson {
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
		Short: "List all packages",
		Long: `List all packages stored in the zonelet server.

Shows package name, version, OS, architecture and size.`,
		RunE: runE,
		Example: `  # List complete packages from local server (default)
  cli pkg-list

  # List all packages including incomplete ones
  cli pkg-list --all

  # List packages from remote server
  cli pkg-list --addr 192.168.1.100:9533

  # Show raw JSON output
  cli pkg-list --show-json`,
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", "127.0.0.1:9533", "Zonelet server address")
	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "Show raw response with JSON")
	cmd.Flags().BoolVarP(&showAll, "all", "", false, "Show all packages including incomplete ones")

	return cmd
}
