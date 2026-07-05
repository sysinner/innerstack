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
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// NewLoginCommand wires up the `login` command: a local zone credential
// manager. It never connects to a server; subsequent commands resolve the
// current zone and connect on demand.
//
//	login -n <zone> -a <host:port> -s <access-key>   add or update a zone
//	login <zone-name>                                switch the current zone
//	login                                            list configured zones
func NewLoginCommand() *cobra.Command {

	var (
		name   string
		addr   string
		secret string
	)

	cmd := &cobra.Command{
		Use:   "login [zone-name]",
		Short: "Add/update a zone login, switch the current zone, or list zones",
		Long: `Manage local zone logins. The login command only edits the local config
file; it does not connect to any server.

  # Add a new zone, or update an existing one's address and access key
  innerstack login -n <zone-name> -a <host:port> -s <access-key>

  # Switch the current zone
  innerstack login <zone-name>

  # List configured zones
  innerstack login
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flag-driven upsert mode takes precedence over positional switch.
			if name != "" {
				return upsertZone(name, addr, secret)
			}
			if len(args) == 1 {
				return switchZone(args[0])
			}
			printZoneList()
			return nil
		},
		Example: `  # Add or update a zone
  innerstack login -n myzone -a 192.168.1.10:9533 -s ak_xxx_yyy

  # Switch the current zone
  innerstack login myzone

  # List configured zones
  innerstack login
 `,
	}

	cmd.Flags().StringVarP(&name, "name", "n", "",
		"Zone name; when set with --addr and --secret, adds the zone or updates its config")
	cmd.Flags().StringVarP(&addr, "addr", "a", "", "Zone API address, e.g. 192.168.1.10:9533")
	cmd.Flags().StringVarP(&secret, "secret", "s", "", "Access key in the form ak_<id>_<secret>")

	return cmd
}

// upsertZone adds a new zone, or updates an existing one's address and access
// key, then marks it current and persists the config.
func upsertZone(name, addr, secret string) error {

	if err := inapi.NameValid(name); err != nil {
		return fmt.Errorf("invalid zone name: %s", err.Error())
	}
	if addr == "" {
		return fmt.Errorf("zone address is required (--addr)")
	}
	if secret == "" {
		return fmt.Errorf("access key is required (--secret)")
	}

	zone := &ConfigZone{Name: name, Addr: addr, AK: secret}
	if _, err := zone.AccessKey(); err != nil {
		return fmt.Errorf("invalid access key for zone %s: %s", name, err.Error())
	}

	// Replace an existing entry in place, or append a new one.
	updated := false
	for i, z := range Config.Zones {
		if z.Name == name {
			Config.Zones[i] = zone
			updated = true
			break
		}
	}
	if !updated {
		Config.Zones = append(Config.Zones, zone)
	}

	Config.CurrentZone = name

	if err := Flush(); err != nil {
		return fmt.Errorf("failed to save config (%s): %s", loadedConfigPath, err.Error())
	}

	verb := "added"
	if updated {
		verb = "updated"
	}
	fmt.Printf("Zone %q %s and set as current (%s)\n", name, verb, addr)
	fmt.Printf("Saved to %s\n", loadedConfigPath)
	return nil
}

// switchZone marks the named zone as current and persists the config.
func switchZone(name string) error {

	var zone *ConfigZone
	for _, z := range Config.Zones {
		if z.Name == name {
			zone = z
			break
		}
	}
	if zone == nil {
		return fmt.Errorf("zone %q not found in config", name)
	}

	if Config.CurrentZone != name {
		Config.CurrentZone = name
		if err := Flush(); err != nil {
			return fmt.Errorf("failed to save config (%s): %s", loadedConfigPath, err.Error())
		}
	}

	fmt.Printf("Switched to zone %q (%s)\n", name, zone.Addr)
	return nil
}

// PrintCurrentZoneHint writes a one-line reminder of the active zone to stderr.
// It helps the operator notice which zone a command will operate on when more
// than one zone is configured. It is a no-op when:
//   - the resolved command is the root (no subcommand) or `login` (which
//     manages zone entries rather than operating on one),
//   - fewer than two zones are configured (nothing to disambiguate).
//
// Output goes to stderr so it never pollutes the command's stdout (JSON,
// tables, ...).
func PrintCurrentZoneHint(cmd *cobra.Command) {
	if cmd == nil || !cmd.HasParent() || cmd.Name() == "login" {
		return
	}
	if len(Config.Zones) <= 1 {
		return
	}
	z, err := Config.Zone("")
	if err != nil || z == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "Current zone: %s (%s)\n", z.Name, z.Addr)
}

// printZoneList renders the configured zones, marking the current one.
func printZoneList() {

	if len(Config.Zones) == 0 {
		fmt.Printf("No zones configured. Add one with:\n  %s login -n <zone> -a <host:port> -s <access-key>\n", AppName)
		return
	}

	fmt.Printf("Configured zones (%s):\n", loadedConfigPath)
	for _, z := range Config.Zones {
		mark := " "
		if z.Name == Config.CurrentZone {
			mark = "*"
		}
		fmt.Printf("  %s %-20s %s\n", mark, z.Name, z.Addr)
	}
}
