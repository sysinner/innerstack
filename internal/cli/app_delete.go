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

	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/internal/client"
)

func NewAppDeleteCommand() *cobra.Command {

	var (
		zoneAddr   string
		instanceId string
	)

	var deleteRun = func(cmd *cobra.Command, args []string) error {
		if instanceId == "" {
			return fmt.Errorf("instance id is required")
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
			return fmt.Errorf("failed to connect to zone leader %s: %s", zone.Addr, err.Error())
		}
		defer conn.Close()

		zc := inapi.NewZoneServiceClient(conn)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		deleteReq := &inapi.AppInstanceDeleteRequest{
			Id: instanceId,
		}

		_, err = zc.AppInstanceDelete(ctx, deleteReq)
		if err != nil {
			return fmt.Errorf("failed to delete app instance: %w", err)
		}

		fmt.Printf("App instance '%s' deleted successfully\n", instanceId)

		return nil
	}

	cmd := &cobra.Command{
		Use:   "app-delete",
		Short: "Delete an app instance",
		Long: `Delete an app instance by its ID from the zone server.
This action cannot be undone.`,
		RunE: deleteRun,
		Example: `  # Delete an app instance
  app delete --id <instance-id>`,
	}

	cmd.Flags().StringVarP(&zoneAddr, "zone-addr", "a", "", "Zone server address")
	cmd.Flags().StringVarP(&instanceId, "id", "i", "", "App instance ID (required)")

	cmd.MarkFlagRequired("id")

	return cmd
}
