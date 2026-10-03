// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// NewKeyCommand returns the "key" command group.
func NewKeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage user access keys",
	}
	cmd.AddCommand(newKeyCreateCommand())
	cmd.AddCommand(newKeyListCommand())
	cmd.AddCommand(newKeyDeleteCommand())
	return cmd
}

func newKeyCreateCommand() *cobra.Command {

	var (
		user        string
		scopes      []string
		description string
	)

	run := func(cmd *cobra.Command, args []string) error {

		if len(scopes) == 0 {
			return fmt.Errorf("at least one scope is required (--scope)")
		}

		_, zc, err := zoneClient()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		resp, err := zc.AccessKeySet(ctx, &inapi.AccessKeySetRequest{
			User:        user,
			Scopes:      scopes,
			Description: description,
		})
		if err != nil {
			return fmt.Errorf("failed to create access key: %s", err.Error())
		}

		// The credential is shown once; the server never returns it again.
		fmt.Printf("access key created (key_id %s) for user %s\n", resp.KeyId, user)
		fmt.Printf("credential (shown once, store it now):\n  %s\n", resp.AccessKey)
		return nil
	}

	cmd := &cobra.Command{
		Use:   "create --user <name>",
		Short: "Create an access key for a user",
		RunE:  run,
	}
	cmd.Flags().StringVar(&user, "user", "", "owning user (must exist and be active)")
	cmd.Flags().StringSliceVar(&scopes, "scope", nil,
		"granted scope, repeatable or comma separated (e.g. app:rw,pkg:ro)")
	cmd.Flags().StringVar(&description, "description", "", "key description")

	return cmd
}

func newKeyListCommand() *cobra.Command {

	var (
		user  string
		limit uint32
	)

	run := func(cmd *cobra.Command, args []string) error {

		_, zc, err := zoneClient()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		resp, err := zc.AccessKeyList(ctx, &inapi.AccessKeyListRequest{
			User:  user,
			Limit: limit,
		})
		if err != nil {
			return err
		}

		var tbuf bytes.Buffer
		if len(resp.Items) > 0 {
			table := tablewriter.NewTable(&tbuf)
			table.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
			})
			table.Header([]any{"Key Id", "User", "Type", "State", "Scopes", "Description"}...)
			for _, k := range resp.Items {
				state := k.State
				if state == "" {
					state = "active"
				}
				ktype := k.Type
				if ktype == "" {
					ktype = "-"
				}
				table.Append([]any{
					k.KeyId,
					k.User,
					ktype,
					state,
					strings.Join(k.Scopes, ","),
					k.Description,
				}...)
			}
			table.Render()
		} else {
			fmt.Println("no access keys")
		}

		fmt.Print(tbuf.String())
		if resp.HasMore {
			fmt.Println("(more access keys available, increase --limit)")
		}
		return nil
	}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List access keys (secrets are never returned)",
		RunE:  run,
	}
	cmd.Flags().StringVar(&user, "user", "", "filter by owning user")
	cmd.Flags().Uint32Var(&limit, "limit", inapi.AccessKeyListLimitDefault, "max keys to return")

	return cmd
}

func newKeyDeleteCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "delete <key-id>",
		Short: "Revoke an access key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			_, zc, err := zoneClient()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			if _, err := zc.AccessKeyDelete(ctx, &inapi.AccessKeyDeleteRequest{
				KeyId: args[0],
			}); err != nil {
				return fmt.Errorf("failed to delete access key: %s", err.Error())
			}

			fmt.Printf("access key %s deleted\n", args[0])
			return nil
		},
	}
}
