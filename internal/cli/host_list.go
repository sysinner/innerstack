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
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/inutil"
)

func NewHostListCommand() *cobra.Command {

	var (
		zoneAddr string
		showJson bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		zone, err := Config.Zone(zoneAddr)
		if err != nil {
			return err
		}

		ak, err := zone.AccessKey()
		if err != nil {
			return fmt.Errorf("invalid access key: %w", err)
		}

		conn, err := client.Connect(zone.Addr, ak, false)
		if err != nil {
			return fmt.Errorf("failed to connect to zone server %s: %w", zone.Addr, err)
		}

		zc := inapi.NewZoneServiceClient(conn)

		req := &inapi.HostListRequest{}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.HostList(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to join host: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if !showJson && len(resp.Hosts) > 0 {

			tableBase := tablewriter.NewTable(&tbuf)

			tableBase.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
				config.Header.Formatting.AutoWrap = tw.WrapNormal
				config.Header.Formatting.AutoFormat = tw.Off
			})

			tableBase.Header([]any{
				"Id", "Address", "Uptime",
				"Sysload",
				"CPU\nModel/Cores/Alloc", "Memory\nTotal/Avail/Alloc",
				"Disk\nTotal/Avail/Alloc",
				"Net\nRecv/Sent",
				"Container\nNum/ImageNum",
			}...)

			for _, v := range resp.Hosts {

				values := []any{v.Id, v.PeerAddr}

				if v.Status != nil {
					values = append(values, inutil.FormatUptime(time.Now().Unix()-v.Status.Uptime))

					values = append(values, fmt.Sprintf("%.2f%%",
						cpuLoad(v.Status.CpuSys, v.Status.CpuUser, int64(v.Status.CpuCores), 60)))

					var cpuAlloc, memAlloc, storageAlloc int64
					if v.Operate != nil {
						cpuAlloc = v.Operate.CpuAlloc
						memAlloc = v.Operate.MemAlloc
						storageAlloc = v.Operate.StorageAlloc
					}

					values = append(values, fmt.Sprintf("%s / %d / %s",
						v.Status.CpuModel, v.Status.CpuCores,
						inutil.PrettyCPUs(cpuAlloc)))

					values = append(values, fmt.Sprintf("%s / %s / %s",
						inutil.PrettyBytes(v.Status.MemTotal, 1024),
						inutil.PrettyBytes(v.Status.MemAvailable, 1024),
						inutil.PrettyBytes(memAlloc, 1024)))

					values = append(values, fmt.Sprintf("%s / %s / %s",
						inutil.PrettyBytes(v.Status.DiskTotalBytes, 1024),
						inutil.PrettyBytes(v.Status.DiskFreeBytes, 1024),
						inutil.PrettyBytes(storageAlloc, 1024)))

					values = append(values, fmt.Sprintf("%s / %s",
						inutil.PrettyBytes(v.Status.NetRecvBytes, 1024),
						inutil.PrettyBytes(v.Status.NetSentBytes, 1024)))

					// Container info
					if len(v.Status.Containers) > 0 {
						var ctrInfos []string
						for name, ctr := range v.Status.Containers {
							ctrInfos = append(ctrInfos, fmt.Sprintf("%s: %d / %d",
								name, ctr.ContainerNum, ctr.ImageNum))
						}
						values = append(values, strings.Join(ctrInfos, ", "))
					} else {
						values = append(values, "-")
					}
				}

				tableBase.Append(values...)
			}

			tableBase.Render()
		} else {
			tbuf.WriteString(fmt.Sprintf("List host '%s' successfully\n", zone.Addr))

			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)
		}

		fmt.Println(tbuf.String())

		return nil
	}

	cmd := &cobra.Command{
		Use:   "host-list",
		Short: "List all hosts in the zone",
		RunE:  run,
	}

	cmd.Flags().StringVarP(&zoneAddr, "zone-addr", "a", "", "Zone server address")
	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "show raw response with json")

	return cmd
}

func cpuLoad(sysMs, userMs, cores, intervalSec int64) float64 {
	if cores <= 0 || intervalSec <= 0 {
		return 0
	}

	totalUsedMs := float64(sysMs + userMs)
	totalAvailableMs := float64(intervalSec * 1000 * cores)

	load := (totalUsedMs / totalAvailableMs) * 100
	return load
}
