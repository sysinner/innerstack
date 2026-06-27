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
	"errors"

	"github.com/spf13/cobra"
	"github.com/sysinner/incore/v2/pkg/inconf"
)

func NewConfigRenderCommand() *cobra.Command {

	var (
		argInput  string
		argOutput string
	)

	configRenderCommand := func(cmd *cobra.Command, args []string) error {

		if argInput == "" {
			return errors.New("--in input file not setup")
		}

		if argOutput == "" {
			return errors.New("--out output file not setup")
		}

		appHelper, err := appSetup()
		if err != nil {
			return err
		}

		return inconf.FileRender(argOutput, argInput, appHelper.Params(), 0640)
	}

	cmd := &cobra.Command{
		Use:     "config-render",
		Aliases: []string{"confrender"},
		Short:   "read input file and render with config data, then write to output file",
		RunE:    configRenderCommand,
	}

	cmd.Flags().StringVar(&argInput, "in",
		"",
		`input file path (template of text, json, toml, yaml, ini)`,
	)

	cmd.Flags().StringVar(&argOutput, "out",
		"",
		`output file path`,
	)

	_ = cmd.MarkFlagRequired("in")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}
