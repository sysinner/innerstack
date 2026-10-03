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
	"encoding/json"
	"fmt"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// NewUserCommand returns the "user" command group.
func NewUserCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage zone users",
	}
	cmd.AddCommand(newUserAddCommand())
	cmd.AddCommand(newUserInfoCommand())
	cmd.AddCommand(newUserSetCommand())
	cmd.AddCommand(newUserListCommand())
	cmd.AddCommand(newUserDeleteCommand())
	return cmd
}

func newUserAddCommand() *cobra.Command {

	var description string

	run := func(cmd *cobra.Command, args []string) error {
		return userSet(args[0], "", description)
	}

	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a user",
		Args:  cobra.ExactArgs(1),
		RunE:  run,
	}
	cmd.Flags().StringVar(&description, "description", "", "user description")

	return cmd
}

func newUserInfoCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "info <name>",
		Short: "Show a user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			_, zc, err := zoneClient()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			resp, err := zc.UserGet(ctx, &inapi.UserGetRequest{Name: args[0]})
			if err != nil {
				return err
			}

			js, _ := json.MarshalIndent(resp.Item, "", "  ")
			fmt.Println(string(js))
			return nil
		},
	}
}

func newUserSetCommand() *cobra.Command {

	var (
		description string
		state       string
	)

	run := func(cmd *cobra.Command, args []string) error {
		if description == "" && state == "" {
			return fmt.Errorf("nothing to update: set --state or --description")
		}
		return userSet(args[0], state, description)
	}

	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Update a user's state or description",
		Args:  cobra.ExactArgs(1),
		RunE:  run,
	}
	cmd.Flags().StringVar(&state, "state", "",
		"active or disabled; disabling revokes all of the user's access keys")
	cmd.Flags().StringVar(&description, "description", "", "user description")

	return cmd
}

func newUserListCommand() *cobra.Command {

	var (
		state string
		limit uint32
	)

	run := func(cmd *cobra.Command, args []string) error {

		_, zc, err := zoneClient()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		resp, err := zc.UserList(ctx, &inapi.UserListRequest{
			State: state,
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
			table.Header([]any{"Name", "State", "Updated", "Description"}...)
			for _, u := range resp.Items {
				table.Append([]any{
					u.Name,
					u.State,
					time.UnixMilli(u.Updated).Format("2006-01-02 15:04:05"),
					u.Description,
				}...)
			}
			table.Render()
		} else {
			fmt.Println("no users")
		}

		fmt.Print(tbuf.String())
		if resp.HasMore {
			fmt.Println("(more users available, increase --limit)")
		}
		return nil
	}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List users",
		RunE:  run,
	}
	cmd.Flags().StringVar(&state, "state", "", "filter by state: active | disabled")
	cmd.Flags().Uint32Var(&limit, "limit", inapi.UserListLimitDefault, "max users to return")

	return cmd
}

func newUserDeleteCommand() *cobra.Command {

	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a user (rejected while access keys remain)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {

			_, zc, err := zoneClient()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			if _, err := zc.UserDelete(ctx, &inapi.UserDeleteRequest{
				Name: args[0],
			}); err != nil {
				return fmt.Errorf("failed to delete user: %s", err.Error())
			}

			fmt.Printf("user %s deleted\n", args[0])
			return nil
		},
	}
}

// userSet creates or updates a user via UserSet. A prior UserGet probe
// distinguishes create from update, so the report states what actually
// happened: keys are only "revoked"/"restored" on a real state transition.
func userSet(name, state, description string) error {

	if err := inapi.NameValid(name); err != nil {
		return fmt.Errorf("invalid user name: %s", err.Error())
	}

	_, zc, err := zoneClient()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var (
		existed   bool
		prevState string
	)
	if prev, err := zc.UserGet(ctx, &inapi.UserGetRequest{Name: name}); err != nil {
		if status.Code(err) != codes.NotFound {
			return err
		}
	} else {
		existed = true
		prevState = prev.Item.State
	}

	resp, err := zc.UserSet(ctx, &inapi.UserSetRequest{
		Name:        name,
		State:       state,
		Description: description,
	})
	if err != nil {
		return fmt.Errorf("failed to save user: %s", err.Error())
	}

	verb := "updated"
	switch {
	case !existed:
		verb = "created"
	case state == inapi.UserStateDisabled && prevState != inapi.UserStateDisabled:
		verb = "disabled (access keys revoked)"
	case state == inapi.UserStateActive && prevState != inapi.UserStateActive:
		verb = "enabled (access keys restored)"
	}
	fmt.Printf("user %s %s\n", resp.Item.Name, verb)
	return nil
}
