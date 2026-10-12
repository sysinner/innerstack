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

// Package main provides the command-line interface (CLI) for the InnerStack
// system: managing zones, hosts, applications, packages and access control.
//
// Commands follow a noun-verb layout ("zone init", "app deploy", "pkg push")
// and are grouped in the root help output:
//
//   - Common Commands: zone, host, app, login
//   - Gateway Commands: ingress
//   - Package Commands: pkg
//   - Security Commands: user, key, audit
//
// Usage:
//
//	innerstack [command] [flags]
//
// Examples:
//
//	innerstack zone init --name myzone
//	innerstack app list
//	innerstack pkg build
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/internal/cli"
)

const AppName = "innerstack"

// version is the bare release semver, stamped at build time via
// -ldflags "-X main.version=$(VERSION)"; "dev" marks an unstamped build.
var version = "dev"

// main is the entry point of the InnerStack CLI application.
func main() {

	// rootCmd is the base command for the InnerStack CLI.
	var rootCmd = &cobra.Command{
		Use:     AppName,
		Version: version,
	}
	rootCmd.SetVersionTemplate(fmt.Sprintf("%s %s\n", AppName, version))

	{
		initConfig := func() {

			if err := cli.Setup(); err != nil {
				fmt.Fprintf(os.Stderr, "Init Config Fail : %s\n", err.Error())
				os.Exit(1)
			}
		}

		cobra.OnInitialize(initConfig)
	}

	loginCmd := cli.NewLoginCommand()

	// Without a subcommand: prompt for a login when no zone is configured,
	// otherwise show help.
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(cli.Config.Zones) == 0 {
			fmt.Println("No zone configured. Set up a zone login first:")
			fmt.Println()
			return loginCmd.Help()
		}
		return cmd.Help()
	}

	// Remind the operator of the active zone when several zones are
	// configured; PrintCurrentZoneHint skips the login command itself.
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cli.PrintCurrentZoneHint(cmd)
		return nil
	}

	// Help-output sections; commands without a group land in the trailing
	// "Additional Commands" section (completion, help).
	rootCmd.AddGroup(
		&cobra.Group{ID: "common", Title: "Common Commands:"},
		&cobra.Group{ID: "gateway", Title: "Gateway Commands:"},
		&cobra.Group{ID: "pkg", Title: "Package Commands:"},
		&cobra.Group{ID: "security", Title: "Security Commands:"},
	)

	// Register the command tree, one help section per entry.
	cmds := []struct {
		cmd *cobra.Command
		grp string
	}{
		{cli.NewZoneCommand(), "common"},
		{cli.NewHostCommand(), "common"},
		{cli.NewAppCommand(), "common"},
		{loginCmd, "common"},
		{cli.NewIngressCommand(), "gateway"},
		{cli.NewPkgCommand(), "pkg"},
		{cli.NewUserCommand(), "security"},
		{cli.NewKeyCommand(), "security"},
		{cli.NewAuditCommand(), "security"},
	}
	for _, c := range cmds {
		c.cmd.GroupID = c.grp
		rootCmd.AddCommand(c.cmd)
	}

	// Exit with code 1 on command failure
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
