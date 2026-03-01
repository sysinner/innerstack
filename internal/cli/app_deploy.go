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
	"context"
	"fmt"
	"time"

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
	)

	var deployRun = func(cmd *cobra.Command, args []string) error {
		if specFile == "" {
			return fmt.Errorf("spec file is required")
		}

		var spec inapi.AppSpec
		if err := htoml.DecodeFromFile(specFile, &spec); err != nil {
			return fmt.Errorf("failed to parse TOML: %w", err)
		}

		if spec.CpuLimit == "" {
			return fmt.Errorf("cpu_limit is required")
		}

		if spec.MemoryLimit == "" {
			return fmt.Errorf("memory_limit is required")
		}

		if spec.VolumeLimit == "" {
			return fmt.Errorf("volume_limit is required")
		}

		conn, err := client.Connect(zoneAddr, nil, false)
		if err != nil {
			return fmt.Errorf("failed to connect to zone leader %s: %w", zoneAddr, err)
		}
		defer conn.Close()

		zc := inapi.NewZoneletClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		instanceReq := &inapi.AppInstanceDeployRequest{
			Id:         instanceId,
			Spec:       &spec,
			ReplicaCap: replicaCap,
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
  app deploy --spec app-spec.toml --id <instance_id> --replica-cap 5`,
	}

	cmd.Flags().StringVarP(&zoneAddr, "zone-addr", "a", "127.0.0.1:9533", "Zone server address")
	cmd.Flags().StringVarP(&specFile, "spec", "s", "", "Path to app spec file (TOML format, required)")
	cmd.Flags().StringVarP(&instanceId, "id", "i", "", "App instance ID (if provided, updates existing instance)")
	cmd.Flags().Uint32VarP(&replicaCap, "replica-cap", "r", 0, "Number of replicas (default: 1 for new, unchanged for update)")

	cmd.MarkFlagRequired("spec")

	return cmd
}
