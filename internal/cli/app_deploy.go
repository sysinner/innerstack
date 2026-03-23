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
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hooto/htoml4g/htoml"
	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
)

func NewAppDeployCommand() *cobra.Command {

	var (
		zoneAddr   string
		specFile   string
		instanceId string
		replicaCap uint32
		skipConfig bool
		action     string
	)

	var deployRun = func(cmd *cobra.Command, args []string) error {
		// spec is required only when creating new instance (no --id)
		if specFile == "" && instanceId == "" {
			return fmt.Errorf("spec file is required for new instance")
		}

		var spec *inapi.AppSpec
		if specFile != "" {
			var s inapi.AppSpec
			if err := htoml.DecodeFromFile(specFile, &s); err != nil {
				return fmt.Errorf("failed to parse TOML: %w", err)
			}

			if s.Resources == nil {
				return fmt.Errorf("resources is required")
			}

			if s.Resources.CpuLimit == "" {
				return fmt.Errorf("resources.cpu_limit is required")
			}

			if s.Resources.MemoryLimit == "" {
				return fmt.Errorf("resources.memory_limit is required")
			}

			if s.Resources.VolumeLimit == "" {
				return fmt.Errorf("resources.volume_limit is required")
			}

			// Validate task trigger fields uniqueness
			for _, task := range s.Tasks {
				if task == nil {
					continue
				}
				if err := inapi.ValidateTaskTrigger(task); err != nil {
					return fmt.Errorf("task %q: %w", task.Name, err)
				}
			}

			spec = &s
		}

		instanceReq := &inapi.AppInstanceDeployRequest{
			Id:         instanceId,
			Spec:       spec,
			ReplicaCap: replicaCap,
			Deploy:     &inapi.AppDeploy{},
		}

		conn, err := client.Connect(zoneAddr, nil, false)
		if err != nil {
			return fmt.Errorf("failed to connect to zone leader %s: %w", zoneAddr, err)
		}
		defer conn.Close()

		zc := inapi.NewZoneletClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Fetch existing instance options if updating
		var existingOptions []*inapi.AppDeployOption
		if instanceReq.Id != "" {
			infoResp, err := zc.AppInstanceInfo(ctx, &inapi.AppInstanceInfoRequest{
				Id: instanceReq.Id,
			})
			if err != nil {
				return fmt.Errorf("failed to get existing instance info: %w", err)
			}
			if infoResp.Instance != nil && infoResp.Instance.Deploy != nil {
				existingOptions = infoResp.Instance.Deploy.Options
			}
		}

		// Interactive config input
		var options []*inapi.AppDeployOption
		if !skipConfig && spec != nil && spec.Config != nil && len(spec.Config.Fields) > 0 {
			fmt.Printf("\nConfig: %s\n", spec.Config.Name)
			fmt.Println(strings.Repeat("-", 60))

			cfgValues, err := promptConfigFields(spec.Config.Fields, existingOptions)
			if err != nil {
				return fmt.Errorf("config input failed: %w", err)
			}

			options = append(options, &inapi.AppDeployOption{
				Name:  spec.Config.Name,
				Items: cfgValues,
			})

			fmt.Println(strings.Repeat("-", 60))
			fmt.Println("Configuration summary:")
			for _, item := range cfgValues {
				fmt.Printf("  %s = %s\n", item.Name, item.Value)
			}
			fmt.Println()
		} else if instanceId != "" && len(existingOptions) > 0 {
			// Use existing options when skipping config input for update
			options = existingOptions
		}

		// Set deploy options if config was provided
		if len(options) > 0 {
			instanceReq.Deploy.Options = options
		}

		// Set deploy action if provided
		if action != "" {
			instanceReq.Deploy.Action = action
		}

		instanceResp, err := zc.AppInstanceDeploy(ctx, instanceReq)
		if err != nil {
			return fmt.Errorf("failed to deploy app instance: %w", err)
		}

		if instanceId != "" {
			fmt.Printf("App instance '%s' updated successfully\n", instanceResp.Id)
		} else {
			fmt.Printf("App instance '%s' deployed successfully\n", instanceResp.Id)
		}

		return nil
	}

	cmd := &cobra.Command{
		Use:   "app-deploy",
		Short: "Deploy or update an app from spec file",
		Long: `Deploy an app from spec file (in TOML format) to API server.
An app instance will be created based on the spec.
If --id is provided, the existing app instance will be updated.`,
		RunE: deployRun,
		Example: `  # Deploy a new app from spec file
  app deploy --spec app-spec.toml

  # Deploy with 3 replicas
  app deploy --spec app-spec.toml --replica-cap 3

  # Update an existing app instance
  app deploy --spec app-spec.toml --id <instance_id>

  # Update replica count of existing instance
  app deploy --spec app-spec.toml --id <instance_id> --replica-cap 5

  # Skip interactive config input
  app deploy --spec app-spec.toml --skip-config

  # Set action on existing instance (start, stop, destroy)
  app deploy --id <instance_id> --action start`,
	}

	cmd.Flags().StringVarP(&zoneAddr, "zone-addr", "a",
		"127.0.0.1:9533", "Zone server address")
	cmd.Flags().StringVarP(&specFile, "spec", "s",
		"", "Path to app spec file (TOML format, required)")
	cmd.Flags().StringVarP(&instanceId, "id", "i",
		"", "App instance ID (if provided, updates existing instance)")
	cmd.Flags().Uint32VarP(&replicaCap, "replica-cap", "r",
		0, "Number of replicas (default: 1 for new, unchanged for update)")
	cmd.Flags().BoolVarP(&skipConfig, "skip-config", "k",
		false, "Skip interactive config input")
	cmd.Flags().StringVarP(&action, "action", "",
		"", "Deploy action (start, stop, destroy)")

	return cmd
}

// promptConfigFields interactively prompts user for each config field
// existingOptions is used to provide current values when updating an existing instance
func promptConfigFields(fields []*inapi.AppSpecConfigField,
	existingOptions []*inapi.AppDeployOption) ([]*inapi.AppDeployOptionField, error) {
	var (
		reader  = bufio.NewReader(os.Stdin)
		results []*inapi.AppDeployOptionField
	)

	for _, field := range fields {
		if field == nil {
			continue
		}

		// Calculate default value
		defaultValue := field.Default

		// Check if there's an existing value for this field
		if existingOptions != nil {
			for _, opt := range existingOptions {
				if opt != nil {
					for _, item := range opt.Items {
						if item != nil && item.Name == field.Name && item.Value != "" {
							defaultValue = item.Value
							break
						}
					}
				}
			}
		}

		if field.AutoFill != "" && defaultValue == field.Default {
			autoValue, err := generateAutoFillValue(field.AutoFill)
			if err != nil {
				return nil, fmt.Errorf("failed to generate auto-fill value for %s: %w", field.Name, err)
			}
			if defaultValue == "" || field.AutoFill != "defval" {
				defaultValue = autoValue
			}
		}

		// Display field info
		fmt.Println()
		if field.Title != "" {
			fmt.Printf("%s", field.Title)
		} else {
			fmt.Printf("%s", field.Name)
		}
		if field.Type != "" {
			fmt.Printf(" (%s)", field.Type)
		}
		fmt.Println()

		if field.Prompt != "" {
			fmt.Printf("  Hint: %s\n", field.Prompt)
		}
		if field.Description != "" {
			fmt.Printf("  Description: %s\n", field.Description)
		}

		// Prompt for input
		if defaultValue != "" {
			fmt.Printf("  Enter value [%s]: ", defaultValue)
		} else {
			fmt.Printf("  Enter value: ")
		}

		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		value := input
		if value == "" {
			value = defaultValue
		}

		// Validate required field
		if value == "" && field.AutoFill == "" && field.Default == "" {
			return nil, fmt.Errorf("field %s is required", field.Name)
		}

		results = append(results, &inapi.AppDeployOptionField{
			Name:  field.Name,
			Value: value,
		})
	}

	return results, nil
}

// generateAutoFillValue generates a value based on the auto_fill type
func generateAutoFillValue(autoFill string) (string, error) {
	switch autoFill {
	case "defval":
		// Use default value, return empty to indicate using default
		return "", nil
	case "hexstr_32":
		// Generate 32-char hex string (16 random bytes)
		bytes := make([]byte, 16)
		if _, err := rand.Read(bytes); err != nil {
			return "", fmt.Errorf("failed to generate random bytes: %w", err)
		}
		return hex.EncodeToString(bytes), nil
	case "hexstr_16":
		// Generate 16-char hex string (8 random bytes)
		bytes := make([]byte, 8)
		if _, err := rand.Read(bytes); err != nil {
			return "", fmt.Errorf("failed to generate random bytes: %w", err)
		}
		return hex.EncodeToString(bytes), nil
	case "uuid":
		// Generate UUID v4 using github.com/google/uuid
		return uuid.New().String(), nil
	default:
		// Unknown auto_fill type, return empty
		return "", nil
	}
}
