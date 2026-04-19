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

func NewZoneSetCommand() *cobra.Command {

	var (
		addr             string
		vpcBridgeCidr    string
		vpcInstanceCidr  string
		vpcNetworkDomain string
	)

	var run = func(cmd *cobra.Command, args []string) error {

		// All three VPC fields must be provided together
		if vpcBridgeCidr == "" || vpcInstanceCidr == "" || vpcNetworkDomain == "" {
			return fmt.Errorf("all three flags are required: --bridge, --instance, --domain")
		}

		zone, err := Config.Zone(addr)
		if err != nil {
			return err
		}

		ak, err := zone.AccessKey()
		if err != nil {
			return fmt.Errorf("invalid access key: %w", err)
		}

		conn, err := client.Connect(zone.Addr, ak, false)
		if err != nil {
			return fmt.Errorf("failed to connect to server %s: %w", zone.Addr, err)
		}

		zc := inapi.NewZoneServiceClient(conn)

		req := &inapi.ZoneSetRequest{
			VpcBridgeCidr:    vpcBridgeCidr,
			VpcInstanceCidr:  vpcInstanceCidr,
			VpcNetworkDomain: vpcNetworkDomain,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.ZoneSet(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to set zone network: %w", err)
		}

		js, _ := json.MarshalIndent(resp.Zone, "", "  ")
		fmt.Printf("Zone network updated:\n%s\n", string(js))

		return nil
	}

	cmd := &cobra.Command{
		Use:   "zone-set",
		Short: "Set zone VPC network configuration",
		Long: `Set zone VPC network configuration.

All three parameters (--bridge, --instance, --domain) are required and
must be provided together. Partial updates are not allowed.

VPC Network Rules:
  --bridge   Private IPv4 /24 (RFC 1918). Host bridge IPs are allocated
             in the fourth octet range [3, 252], supporting up to 250
             host nodes per zone.

  --instance Private IPv4 /16 (RFC 1918). Address layout a.b.{host}.{inst}
             where both {host} and {inst} are allocated in range [3, 252].
             Max capacity: 250 × 250 = 62,500 instances.

  --domain   DNS domain for VPC internal name resolution.
             Internal DNS resolves <app-instance-id>.<domain> to the
             container instance IP. Default: "local".
             Recommended format: <zone-name>.<your-domain.com>

  The bridge and instance CIDRs must not overlap.`,
		RunE: run,
		Example: `  instack zone-set \
    --bridge 192.168.10.0/24 \
    --instance 10.10.0.0/16 \
    --domain local`,
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", "", "Zonelet server address")
	cmd.Flags().StringVarP(&vpcBridgeCidr, "bridge", "b", "192.168.10.0/24", "VPC bridge CIDR (e.g., 192.168.10.0/24) (required)")
	cmd.Flags().StringVarP(&vpcInstanceCidr, "instance", "i", "10.10.0.0/16", "VPC instance CIDR (e.g., 10.10.0.0/16) (required)")
	cmd.Flags().StringVarP(&vpcNetworkDomain, "domain", "d", "local", "VPC network domain (e.g., local) (required)")

	return cmd
}
