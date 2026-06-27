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

// Package main provides the command-line interface (CLI) for the InnerStack system.
// InnerStack is a distributed application management tool that enables users to manage
// zones, hosts, applications, and packages through a unified command-line interface.
//
// The CLI is built using the Cobra library and provides the following command groups:
//
//   - Zone Management: zone-init, zone-info
//   - Host Management: host-join, host-list
//   - Application Management: app-list, app-info, app-deploy, app-delete
//   - Package Management: pkg-build, pkg-push, pkg-list, pkg-info, pkg-del
//
// Usage:
//
//	innerstack [command] [flags]
//
// Examples:
//
//	# Initialize a new zone
//	innerstack zone-init --name myzone
//
//	# List all applications in a zone
//	innerstack app-list --zone-addr 127.0.0.1:9533
//
//	# Build a package
//	innerstack pkg-build --spec /path/to/ipk.toml
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/internal/cli"
)

const AppName = "innerstack"

// main is the entry point of the InnerStack CLI application.
// It initializes the root command and registers all subcommands
// for zone, host, application, and package management.
func main() {

	// rootCmd is the base command for the InnerStack CLI.
	// When called without any subcommands, it displays a welcome message.
	var rootCmd = &cobra.Command{
		Use: AppName,
		// SilenceUsage: true,
	}

	{
		initConfig := func() {

			if err := cli.Setup(); err != nil {
				fmt.Fprintf(os.Stderr, "Init Config Fail : %s\n", err.Error())
				os.Exit(1)
			}

			if _, err := cli.Config.Zone(""); err != nil && len(cli.Config.Zones) == 0 {
				fmt.Fprintf(os.Stderr, "Init Config Fail : no zone setup\n")
				os.Exit(1)
			}
		}

		cobra.OnInitialize(initConfig)
	}

	rootCmd.AddCommand(cli.NewLoginCommand(rootCmd))

	// Register zone management commands
	// - zone-init: Initialize a new zone with specified configuration
	// - zone-info: Retrieve and display information about a zone
	// - zone-set: Update zone VPC network configuration
	rootCmd.AddCommand(cli.NewZoneInitCommand())
	rootCmd.AddCommand(cli.NewZoneInfoCommand())
	rootCmd.AddCommand(cli.NewZoneSetCommand())

	// Register host management commands
	// - host-join: Join a host to the specified zone
	// - host-list: List all hosts in the zone
	rootCmd.AddCommand(cli.NewHostJoinCommand())
	rootCmd.AddCommand(cli.NewHostListCommand())

	// Register application management commands
	// - app-list: List all application instances in the zone
	// - app-info: Display detailed information about a specific application
	// - app-deploy: Deploy a new application or update an existing one
	// - app-delete: Remove an application from the zone
	rootCmd.AddCommand(cli.NewAppListCommand())
	rootCmd.AddCommand(cli.NewAppInfoCommand())
	rootCmd.AddCommand(cli.NewAppDeployCommand())
	rootCmd.AddCommand(cli.NewAppDeleteCommand())

	// Register gateway ingress management commands
	// - gw-ingress-list: List all gateway ingress rules
	// - gw-ingress-info: Display detailed information about a specific ingress
	// - gw-ingress-set: Create or update a gateway ingress rule
	rootCmd.AddCommand(cli.NewGatewayIngressListCommand())
	rootCmd.AddCommand(cli.NewGatewayIngressInfoCommand())
	rootCmd.AddCommand(cli.NewGatewayIngressSetCommand())

	// Register package management commands
	// - pkg-build: Build a package from source specification
	// - pkg-push: Upload a package to the package repository
	// - pkg-list: List available packages in the repository
	// - pkg-info: Display detailed information about a specific package
	// - pkg-del: Delete a package from the repository
	rootCmd.AddCommand(cli.NewPkgBuildCommand())
	rootCmd.AddCommand(cli.NewPkgPushCommand())
	rootCmd.AddCommand(cli.NewPkgListCommand())
	rootCmd.AddCommand(cli.NewPkgInfoCommand())
	rootCmd.AddCommand(cli.NewPkgDelCommand())

	// Execute the root command and handle errors
	// Exit with code 1 if any error occurs during command execution
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
