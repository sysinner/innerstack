// Copyright 2015 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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
	"runtime"

	"github.com/sysinner/incore/inagent/cmd"
	"github.com/sysinner/incore/inagent/daemon"
	"github.com/sysinner/incore/inapi"
)

var (
	version = "0.9.0"
	release = ""
	Prefix  = "/home/action"
)

var rootCmd = &inapi.BaseCommand{
	Use:   "inagent",
	Short: "An Efficient Enterprise PaaS Engine",
}

func main() {

	runtime.GOMAXPROCS(1)

	rootCmd.PersistentFlags().StringVar(&Prefix, "prefix",
		"/home/action",
		"specify the home directory of project")

	rootCmd.AddCommand(daemon.NewAgentDaemonCommand())

	rootCmd.AddCommand(cmd.NewConfigMergeCommand())
	rootCmd.AddCommand(cmd.NewConfigRenderCommand())
	rootCmd.AddCommand(cmd.NewHealthSyncCommand())
	rootCmd.AddCommand(cmd.NewHealthStatusCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	os.Exit(0)

	/**
	action := "agent"
	if len(os.Args) > 1 {
		action = os.Args[1]
	}

	switch action {

	case "config", "confrender":
		if err := confrender.ActionConfig(); err != nil {
			fmt.Println("cmd error :", err)
			os.Exit(1)
		}

	case "config-merge":
		if err := confrender.MergeAction(); err != nil {
			fmt.Println("cmd error :", err)
			os.Exit(1)
		}

	case "agent":
		if err := daemon.Start(); err != nil {
			fmt.Println("inagent/daemon failed", err)
			os.Exit(1)
		}

	case "health-sync":
		if err := health.HealthSync(); err != nil {
			fmt.Println("inagent/health-sync error", err)
			os.Exit(1)
		}

	case "health-status":
		if err := health.HealthStatus(); err != nil {
			fmt.Println("inagent/health-status error", err)
			os.Exit(1)
		}

	default:
		fmt.Println("invalid command")
		os.Exit(1)
	}
	*/
}
