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
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inconf"
)

func NewConfigMergeCommand() *cobra.Command {

	var (
		argWithConfigField string
		argConfig          string
	)

	configMergeCommand := func(cmd *cobra.Command, args []string) error {

		argWithConfigField = strings.TrimPrefix(argWithConfigField, "cfg/")
		if argWithConfigField == "" {
			return errors.New("invalid --with-config-field value")
		}

		if argConfig == "" {
			return errors.New("--config file path not found")
		}

		appHelper, err := appSetup()
		if err != nil {
			return err
		}

		field := appHelper.Config(argWithConfigField)
		if field == nil {
			return fmt.Errorf("config field (%s) not found", argWithConfigField)
		}

		field.Value = strings.TrimSpace(field.Value)
		if field.Value == "" {
			return nil
		}

		if sets := appHelper.Params(); len(sets) > 0 {
			field.Value = inconf.RenderWithExpand(field.Value, sets)
		}

		slog.Info("load config field value : " + field.Value)

		cg := viper.New()

		fieldType := field.Type
		// Infer type from target config file extension when field type is empty
		if fieldType == "" {
			switch ext := strings.ToLower(filepath.Ext(argConfig)); ext {
			case ".json":
				fieldType = inapi.SpecFieldTypeTextJSON
			case ".toml":
				fieldType = inapi.SpecFieldTypeTextTOML
			case ".yaml", ".yml":
				fieldType = inapi.SpecFieldTypeTextYAML
			case ".ini", ".cfg", ".conf":
				fieldType = inapi.SpecFieldTypeTextINI
			case ".properties":
				fieldType = inapi.SpecFieldTypeTextJavaProp
			default:
				return fmt.Errorf("cannot infer config type from file extension %q and field type is empty", ext)
			}
			slog.Info("inferred config type from file extension", "type", fieldType)
		}

		switch fieldType {
		case inapi.SpecFieldTypeTextINI:
			// viper does not natively support INI format.
			// Write the rendered config value directly to the target file.
			if err := os.WriteFile(argConfig, []byte(field.Value), 0644); err != nil {
				return fmt.Errorf("write config file failed: %w", err)
			}
			slog.Info("config file written", "path", argConfig)

		case inapi.SpecFieldTypeTextJSON,
			inapi.SpecFieldTypeTextTOML,
			inapi.SpecFieldTypeTextYAML,
			inapi.SpecFieldTypeTextJavaProp:

			switch fieldType {
			case inapi.SpecFieldTypeTextJSON:
				cg.SetConfigType("json")
			case inapi.SpecFieldTypeTextTOML:
				cg.SetConfigType("toml")
			case inapi.SpecFieldTypeTextYAML:
				cg.SetConfigType("yaml")
			case inapi.SpecFieldTypeTextJavaProp:
				cg.SetConfigType("properties")
			}

			cg.SetConfigFile(argConfig)

			if err := cg.ReadInConfig(); err != nil {
				return err
			}

			if err := cg.MergeConfig(bytes.NewBuffer([]byte(field.Value))); err != nil {
				return err
			}

			if err := cg.WriteConfigAs(argConfig); err != nil {
				return err
			}

		default:
			return fmt.Errorf("field type(%s) not support", fieldType)
		}

		return nil
	}

	cmd := &cobra.Command{
		Use:   "config-merge",
		Short: "merge one of input text (json, yaml, toml, ini) to local config file",
		Long:  ``,
	}

	cmd.Flags().StringVar(&argWithConfigField, "with-config-field",
		"",
		`path of config item
format:
  <config field name>
example:
  server_ini
`)

	cmd.Flags().StringVar(&argConfig, "config",
		"",
		`the target config file path that merge to it`,
	)

	cmd.RunE = configMergeCommand

	return cmd
}
