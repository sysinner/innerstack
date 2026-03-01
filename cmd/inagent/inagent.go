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
	"runtime"

	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/internal/inagent/daemon"
)

var (
	version = "2.0.0-dev"
	release = ""
	Prefix  = "/home/action"
)

var rootCmd = &cobra.Command{
	Use:   "inagent",
	Short: "An Efficient Enterprise PaaS Engine",
}

func main() {

	runtime.GOMAXPROCS(1)

	rootCmd.PersistentFlags().StringVar(&Prefix, "prefix",
		"/home/action",
		"specify the home directory of project")

	rootCmd.AddCommand(daemon.NewAgentDaemonCommand())

	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
