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

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/sysinner/incore/v2/internal/cli"
)

func main() {

	var rootCmd = &cobra.Command{
		Use: "app",
		// SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("欢迎使用 App！尝试运行 'app hello'")
		},
	}

	rootCmd.AddCommand(cli.NewZoneInitCommand())
	rootCmd.AddCommand(cli.NewZoneInfoCommand())

	rootCmd.AddCommand(cli.NewHostJoinCommand())
	rootCmd.AddCommand(cli.NewHostListCommand())

	rootCmd.AddCommand(cli.NewAppListCommand())
	rootCmd.AddCommand(cli.NewAppInfoCommand())
	rootCmd.AddCommand(cli.NewAppDeployCommand())
	rootCmd.AddCommand(cli.NewAppDeleteCommand())

	rootCmd.AddCommand(cli.NewPkgBuildCommand())

	if err := rootCmd.Execute(); err != nil {
		// fmt.Println(err)
		os.Exit(1)
	}
}
