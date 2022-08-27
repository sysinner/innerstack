// Copyright 2019 Eryx <evorui аt gmail dοt com>, All rights reserved.
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
	"path"

	"github.com/spf13/cobra"
)

type baseCommand = cobra.Command

var (
	version = "0.9.0"
	release = ""
	Prefix  = "/opt/sysinner/innerstack"
)

var rootCmd = &baseCommand{
	Use:   "innerstack",
	Short: "An Efficient Enterprise PaaS Engine",
}

func main() {

	rootCmd.PersistentFlags().StringVar(&Prefix, "prefix",
		"/opt/sysinner/innerstack",
		"specify the home directory of project")

	if _, err := os.Stat(Prefix); err != nil {
		prefix := path.Clean(os.Getenv("GOPATH") + "/src/github.com/sysinner/innerstack")
		if _, err = os.Stat(prefix); err == nil {
			Prefix = prefix
		}
	}

	rootCmd.AddCommand(NewInfoCommand())
	rootCmd.AddCommand(NewZoneInitCommand())
	rootCmd.AddCommand(NewHostJoinCommand())

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	os.Exit(0)
}
