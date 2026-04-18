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

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

func NewConfigExportCommand() *cobra.Command {

	configExportCommand := func(cmd *cobra.Command, args []string) error {

		appHelper, err := appSetup()
		if err != nil {
			return err
		}

		cfgMap := appHelper.Params()

		js, err := json.MarshalIndent(cfgMap, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println()
		fmt.Println(string(js))
		fmt.Println()

		return nil
	}

	cmd := &cobra.Command{
		Use:   "config-export",
		Short: "export config data",
		Long:  ``,
	}

	cmd.RunE = configExportCommand

	return cmd
}
