// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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

import "github.com/spf13/cobra"

// Noun-verb command tree: "zone init", "app deploy", "pkg push", ...
// Help-section group IDs are assigned in cmd/cli/main.go.

// NewZoneCommand returns the "zone" command group.
func NewZoneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "zone",
		Short: "Manage the zone",
	}
	cmd.AddCommand(newZoneInitCommand())
	cmd.AddCommand(newZoneInfoCommand())
	cmd.AddCommand(newZoneSetCommand())
	return cmd
}

// NewHostCommand returns the "host" command group.
func NewHostCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "host",
		Short: "Manage zone hosts",
	}
	cmd.AddCommand(newHostJoinCommand())
	cmd.AddCommand(newHostListCommand())
	return cmd
}

// NewAppCommand returns the "app" command group.
func NewAppCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Manage app instances and specs",
	}
	cmd.AddCommand(newAppDeployCommand())
	cmd.AddCommand(newAppListCommand())
	cmd.AddCommand(newAppInfoCommand())
	cmd.AddCommand(newAppDeleteCommand())
	cmd.AddCommand(newAppSpecListCommand())
	cmd.AddCommand(newAppSpecInfoCommand())
	return cmd
}

// NewIngressCommand returns the "ingress" command group.
func NewIngressCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingress",
		Short: "Manage gateway ingress rules",
	}
	cmd.AddCommand(newGatewayIngressListCommand())
	cmd.AddCommand(newGatewayIngressInfoCommand())
	cmd.AddCommand(newGatewayIngressSetCommand())
	return cmd
}

// NewPkgCommand returns the "pkg" command group.
func NewPkgCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pkg",
		Short: "Build, upload and manage .ipk packages",
	}
	cmd.AddCommand(newPkgBuildCommand())
	cmd.AddCommand(newPkgPushCommand())
	cmd.AddCommand(newPkgListCommand())
	cmd.AddCommand(newPkgInfoCommand())
	cmd.AddCommand(newPkgExportCommand())
	cmd.AddCommand(newPkgDelCommand())
	return cmd
}
