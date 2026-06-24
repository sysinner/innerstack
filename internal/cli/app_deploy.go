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
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hooto/htoml4g/htoml"
	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/inutil/autofill"
	"github.com/sysinner/incore/v2/pkg/inapi"
)

func NewAppDeployCommand() *cobra.Command {

	var (
		specFile     string
		instanceName string
		replicaCap   uint32
		skipConfig   bool
		action       string
	)

	var deployRun = func(cmd *cobra.Command, args []string) error {
		// name is the primary logical key for both create and update.
		if instanceName == "" {
			return fmt.Errorf("--name is required")
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
			Name:       instanceName,
			Spec:       spec,
			ReplicaCap: replicaCap,
			Deploy:     &inapi.AppDeploy{},
		}

		zone, err := Config.Zone("")
		if err != nil {
			return err
		}

		ak, err := zone.AccessKey()
		if err != nil {
			return fmt.Errorf("invalid access key: %w", err)
		}

		conn, err := client.Connect(zone.Addr, ak, false)
		if err != nil {
			return fmt.Errorf("failed to connect to zone leader %s: %w", zone.Addr, err)
		}
		defer conn.Close()

		zc := inapi.NewZoneServiceClient(conn)

		// Fetch existing instance info by name to determine create vs update
		// and to load existing configs/depends for update.
		var (
			existingConfigs []*inapi.AppDeployConfigItem
			existingDepends []*inapi.AppDeployDepend
			isUpdate        bool
		)
		{
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			infoResp, err := zc.AppInstanceInfo(ctx, &inapi.AppInstanceInfoRequest{
				Name: instanceName,
			})
			cancel()
			if err == nil && infoResp.Instance != nil {
				isUpdate = true
				if infoResp.Instance.Deploy != nil {
					existingConfigs = infoResp.Instance.Deploy.Configs
					existingDepends = infoResp.Instance.Deploy.Depends
				}
			}
		}

		reader := bufio.NewReader(os.Stdin)

		// Interactive app name confirmation for new instance (first prompt)
		if !isUpdate {
			instanceReq.Name = instanceName
			if spec != nil {
				// For new instances with spec, confirm the name interactively
				if err := promptAppName(reader, instanceName, instanceReq); err != nil {
					return err
				}
			}
		}

		// Interactive dependency resolution
		if spec != nil && len(spec.Depends) > 0 {
			depBounds, err := promptDependencyInstances(spec.Depends, existingDepends, zc)
			if err != nil {
				return fmt.Errorf("dependency resolution failed: %w", err)
			}
			instanceReq.Deploy.Depends = depBounds
		}

		// Interactive config input
		var configs []*inapi.AppDeployConfigItem
		if !skipConfig && spec != nil && len(spec.Configs) > 0 {
			fmt.Printf("\nConfig:\n")
			fmt.Println(strings.Repeat("-", 60))

			cfgItems, err := promptConfigItems(spec, existingConfigs)
			if err != nil {
				return fmt.Errorf("config input failed: %w", err)
			}
			configs = cfgItems

			fmt.Println(strings.Repeat("-", 60))
			fmt.Println("Configuration summary:")
			for _, item := range cfgItems {
				if strings.Contains(item.Value, "\n") {
					fmt.Printf("  %s =\n", item.Name)
					for _, line := range strings.Split(item.Value, "\n") {
						fmt.Printf("    %s\n", line)
					}
				} else {
					fmt.Printf("  %s = %s\n", item.Name, item.Value)
				}
			}
			fmt.Println()
		} else if isUpdate && len(existingConfigs) > 0 {
			// Use existing configs when skipping config input for update
			configs = existingConfigs
		}

		// Set deploy configs if config was provided
		if len(configs) > 0 {
			instanceReq.Deploy.Configs = configs
		}

		// Set deploy action if provided
		if action != "" {
			instanceReq.Deploy.Action = action
		}

		// Submit loop: retry on app name conflict
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			instanceResp, err := zc.AppInstanceDeploy(ctx, instanceReq)
			cancel()

			if err != nil {
				errMsg := err.Error()
				if !isUpdate && strings.Contains(errMsg, "already in use") {
					fmt.Printf("\n  Error: %s\n", errMsg)
					defaultName := instanceName
					if spec != nil {
						defaultName = spec.Name
					}
					if err := promptAppName(reader, defaultName, instanceReq); err != nil {
						return err
					}
					continue
				}
				return fmt.Errorf("failed to deploy app instance: %w", err)
			}

			if isUpdate {
				fmt.Printf("App instance '%s' updated successfully\n", instanceResp.Name)
			} else {
				fmt.Printf("App instance '%s' deployed successfully\n", instanceResp.Name)
			}
			break
		}

		return nil
	}

	cmd := &cobra.Command{
		Use:   "app-deploy",
		Short: "Deploy or update an app from spec file",
		Long: `Deploy an app from spec file (in TOML format) to API server.
An app instance will be created based on the spec.
If the instance name already exists, the existing app instance will be updated.`,
		RunE: deployRun,
		Example: `  # Deploy a new app from spec file
  app deploy --spec app-spec.toml --name myapp

  # Deploy with 3 replicas
  app deploy --spec app-spec.toml --name myapp --replica-cap 3

  # Update an existing app instance
  app deploy --spec app-spec.toml --name myapp

  # Update replica count of existing instance
  app deploy --spec app-spec.toml --name myapp --replica-cap 5

  # Skip interactive config input
  app deploy --spec app-spec.toml --name myapp --skip-config

  # Set action on existing instance (start, stop, destroy)
  app deploy --name myapp --action start`,
	}

	cmd.Flags().StringVarP(&specFile, "spec", "s",
		"", "Path to app spec file (TOML format)")
	cmd.Flags().StringVarP(&instanceName, "name", "n",
		"", "App instance name (required, unique within the zone)")
	cmd.Flags().Uint32VarP(&replicaCap, "replica-cap", "r",
		0, "Number of replicas (default: 1 for new, unchanged for update)")
	cmd.Flags().BoolVarP(&skipConfig, "skip-config", "k",
		false, "Skip interactive config input")
	cmd.Flags().StringVarP(&action, "action", "",
		"", "Deploy action (start, stop, destroy)")

	cmd.MarkFlagRequired("name")

	return cmd
}

// promptAppName interactively prompts for an app instance name.
// It displays the defaultName as a hint and sets req.Name on success.
func promptAppName(reader *bufio.Reader, defaultName string, req *inapi.AppInstanceDeployRequest) error {
	fmt.Println()
	fmt.Println("App Name")
	fmt.Println(strings.Repeat("-", 60))

	for {
		if defaultName != "" {
			fmt.Printf("  Enter app name [%s]: ", defaultName)
		} else {
			fmt.Printf("  Enter app name: ")
		}

		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		input = strings.TrimSpace(input)

		appName := input
		if appName == "" {
			appName = defaultName
		}
		if appName == "" {
			fmt.Println("  Error: app name is required")
			continue
		}

		req.Name = appName
		fmt.Printf("  App name: %s\n", appName)
		break
	}

	fmt.Println(strings.Repeat("-", 60))
	fmt.Println()
	return nil
}

// isMultiLineType returns true if the given field type represents a multi-line
// text value that should be edited with a multi-line input mode.
func isMultiLineType(t string) bool {
	switch t {
	case inapi.SpecFieldTypeText,
		inapi.SpecFieldTypeTextJSON,
		inapi.SpecFieldTypeTextTOML,
		inapi.SpecFieldTypeTextYAML,
		inapi.SpecFieldTypeTextINI,
		inapi.SpecFieldTypeTextJavaProp,
		inapi.SpecFieldTypeTextMarkdown:
		return true
	default:
		return false
	}
}

// promptConfigItems interactively prompts user for each config field
// existingConfigs is used to provide current values when updating an existing instance
func promptConfigItems(appSpec *inapi.AppSpec,
	existingConfigs []*inapi.AppDeployConfigItem) ([]*inapi.AppDeployConfigItem, error) {
	var (
		reader  = bufio.NewReader(os.Stdin)
		results []*inapi.AppDeployConfigItem
	)

	for _, field := range appSpec.Configs {
		if field == nil {
			continue
		}

		// Calculate default value
		defaultValue := field.Default

		// Check if there's an existing value for this field
		for _, item := range existingConfigs {
			if item != nil && item.Name == field.Name && item.Value != "" {
				defaultValue = item.Value
				break
			}
		}

		if field.AutoFill != "" && defaultValue == field.Default {
			autoValue, err := autofill.Generate(field.AutoFill)
			if err != nil {
				return nil, fmt.Errorf("failed to generate auto-fill value for %s: %w", field.Name, err)
			}
			if defaultValue == "" || field.AutoFill != autofill.DefVal {
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

		var value string

		if isMultiLineType(field.Type) {
			// Multi-line input mode. The current value is printed for
			// reference, then the user picks one of:
			//   <Enter>  -> skip, keep current value
			//   edit     -> open $EDITOR on a temp file (preserves content
			//               byte-for-byte, no terminal paste mangling)
			//   <text>   -> single-line inline override
			if defaultValue != "" {
				fmt.Printf("  Current value:\n")
				for _, line := range strings.Split(defaultValue, "\n") {
					fmt.Printf("    %s\n", line)
				}
			} else {
				fmt.Printf("  (no current value)\n")
			}
			fmt.Printf("  Enter=skip (keep current) | edit=open editor | type text to override\n  > ")

			input, err := reader.ReadString('\n')
			// io.EOF on the final line without a trailing newline is not an
			// error here; treat whatever was read as valid input.
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("failed to read input: %w", err)
			}
			input = strings.TrimRight(input, "\r\n")

			switch {
			case input == "":
				value = defaultValue // skip, keep current
			case input == "edit":
				value, err = Edit(defaultValue, suffixFor(field.Type))
				if err != nil {
					return nil, fmt.Errorf("[promptConfigItems] editor for %s: %w", field.Name, err)
				}
			default:
				value = input // single-line inline override
			}
		} else {
			// Single-line input mode
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
			value = input
			if value == "" {
				value = defaultValue
			}
		}

		// Validate required field
		if value == "" && field.AutoFill == "" && field.Default == "" {
			return nil, fmt.Errorf("field %s is required", field.Name)
		}

		results = append(results, &inapi.AppDeployConfigItem{
			Name:  field.Name,
			Value: value,
		})
	}

	return results, nil
}

// promptDependencyInstances interactively prompts the user to select a deployed
// instance for each AppSpec dependency. It fetches the current instance list
// from the zone and filters candidates by spec.name match.
// existingDepends provides the current dependency bindings from an existing
// instance (if updating). Candidates matching existing bindings are marked with
// "(bound)" and pressing Enter preserves the current binding.
func promptDependencyInstances(
	depends []*inapi.AppSpecDepend,
	existingDepends []*inapi.AppDeployDepend,
	zc inapi.ZoneServiceClient,
) ([]*inapi.AppDeployDepend, error) {

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listResp, err := zc.AppInstanceList(ctx, &inapi.AppInstanceListRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to list app instances: %w", err)
	}

	// Build index: spec.name -> []*AppInstance
	instancesByName := make(map[string][]*inapi.AppInstance)
	for _, inst := range listResp.Items {
		if inst == nil || inst.Spec == nil || inst.Spec.Name == "" {
			continue
		}
		instancesByName[inst.Spec.Name] = append(instancesByName[inst.Spec.Name], inst)
	}

	// Build index: spec.name -> instance_name from existing bindings
	existingBound := make(map[string]string, len(existingDepends))
	for _, dep := range existingDepends {
		if dep != nil && dep.SpecName != "" && dep.InstanceName != "" {
			existingBound[dep.SpecName] = dep.InstanceName
		}
	}

	reader := bufio.NewReader(os.Stdin)
	var results []*inapi.AppDeployDepend

	fmt.Println()
	fmt.Println("App Dependency Resolution")
	fmt.Println(strings.Repeat("-", 60))

	for _, dep := range depends {
		if dep == nil || dep.Name == "" {
			continue
		}

		fmt.Println()
		fmt.Printf("Dependency: %s", dep.Name)
		if dep.Version != "" {
			fmt.Printf(" (version: %s)", dep.Version)
		}
		fmt.Println()

		boundName := existingBound[dep.Name]
		candidates := instancesByName[dep.Name]

		if len(candidates) == 0 {
			if boundName != "" {
				fmt.Printf("  Current binding: %s\n", boundName)
				fmt.Printf("  Enter new instance name (or press Enter to keep): ")
			} else {
				fmt.Printf("  WARNING: no deployed instance found for %q\n", dep.Name)
				fmt.Printf("  Enter instance name (or leave empty to skip): ")
			}

			input, err := reader.ReadString('\n')
			if err != nil {
				return nil, fmt.Errorf("failed to read input: %w", err)
			}
			input = strings.TrimSpace(input)

			if input == "" {
				if boundName != "" {
					fmt.Printf("  Kept binding: %s\n", boundName)
					results = append(results, &inapi.AppDeployDepend{
						SpecName:     dep.Name,
						InstanceName: boundName,
					})
				} else {
					fmt.Printf("  Skipped dependency %q\n", dep.Name)
				}
				continue
			}
			results = append(results, &inapi.AppDeployDepend{
				SpecName:     dep.Name,
				InstanceName: input,
			})
			continue
		}

		// Display candidate instances, marking the currently bound one
		fmt.Println("  Available instances:")
		for i, inst := range candidates {
			marker := ""
			if inst.InstanceName() == boundName {
				marker = " (bound)"
			}
			fmt.Printf("    [%d] Name: %s%s\n", i+1, inst.InstanceName(), marker)
		}

		if len(candidates) == 1 {
			inst := candidates[0]
			fmt.Printf("  Auto-selected (only one instance): %s\n", inst.InstanceName())
			results = append(results, &inapi.AppDeployDepend{
				SpecName:     dep.Name,
				InstanceName: inst.InstanceName(),
			})
			continue
		}

		// Prompt with context for existing binding
		if boundName != "" {
			fmt.Printf("  Enter number [1-%d] or instance name (press Enter to keep %s): ",
				len(candidates), boundName)
		} else {
			fmt.Printf("  Enter number [1-%d] or instance name: ", len(candidates))
		}

		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read input: %w", err)
		}
		input = strings.TrimSpace(input)

		// Empty input preserves existing binding
		if input == "" && boundName != "" {
			fmt.Printf("  Kept binding: %s\n", boundName)
			results = append(results, &inapi.AppDeployDepend{
				SpecName:     dep.Name,
				InstanceName: boundName,
			})
			continue
		}

		// Try to parse as selection number
		var selectedName string
		for i, inst := range candidates {
			if input == fmt.Sprintf("%d", i+1) {
				selectedName = inst.InstanceName()
				break
			}
		}

		// Use as literal instance name if not a number match
		if selectedName == "" && input != "" {
			selectedName = input
		}

		if selectedName == "" {
			return nil, fmt.Errorf("no instance selected for dependency %q", dep.Name)
		}

		results = append(results, &inapi.AppDeployDepend{
			SpecName:     dep.Name,
			InstanceName: selectedName,
		})
		fmt.Printf("  Selected: %s\n", selectedName)
	}

	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("Dependency summary:")
	for _, d := range results {
		fmt.Printf("  %s -> %s\n", d.SpecName, d.InstanceName)
	}
	fmt.Println()

	return results, nil
}
