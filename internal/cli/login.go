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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c-bata/go-prompt"
	"github.com/mattn/go-shellwords"
	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/pkg/inapi"
)

func NewLoginCommand(rootCmd *cobra.Command) *cobra.Command {

	var loginRun = func(cmd *cobra.Command, args []string) error {

		if len(Config.Zones) == 0 {
			return fmt.Errorf("No zones configured in %s", defaultConfigPath)
		}

		// Always list all configured zones
		fmt.Printf("Current configured zones (%s):\n", loadedConfigPath)
		for _, z := range Config.Zones {
			mark := " "
			if z.Name == Config.CurrentZone {
				mark = "*"
			}
			fmt.Printf("  %s %s (%s)\n", mark, z.Name, z.Addr)
		}

		var zone *ConfigZone

		// If a zone name is provided as argument, look it up directly
		if len(args) > 0 && args[0] != "" {
			for _, z := range Config.Zones {
				if z.Name == args[0] {
					zone = z
					break
				}
			}
			if zone == nil {
				return fmt.Errorf("Zone '%s' not found in config", args[0])
			}
		} else if len(Config.Zones) == 1 {
			// Single zone: auto-select
			zone = Config.Zones[0]
		} else if Config.CurrentZone != "" {
			// Multiple zones with a current zone set: use it
			for _, z := range Config.Zones {
				if z.Name == Config.CurrentZone {
					zone = z
					break
				}
			}
			if zone == nil {
				// Current zone name is invalid, reset and prompt
				Config.CurrentZone = ""
			}
		}

		// If still not resolved, prompt for selection
		if zone == nil {
			suggests := make([]prompt.Suggest, len(Config.Zones))
			for i, z := range Config.Zones {
				suggests[i] = prompt.Suggest{
					Text:        z.Name,
					Description: z.Addr,
				}
			}

			selected := prompt.Input("Select zone: ",
				func(d prompt.Document) []prompt.Suggest {
					return prompt.FilterHasPrefix(suggests, d.GetWordBeforeCursor(), true)
				},
			)
			selected = strings.TrimSpace(selected)

			for _, z := range Config.Zones {
				if z.Name == selected {
					zone = z
					break
				}
			}

			if zone == nil {
				return fmt.Errorf("Zone '%s' not found in config", selected)
			}
		}

		// Parse access key
		ak, err := zone.AccessKey()
		if err != nil {
			return fmt.Errorf("Invalid access key for zone %s: %s", zone.Name, err.Error())
		}

		// Connect to server
		conn, err := client.Connect(zone.Addr, ak, false)
		if err != nil {
			return fmt.Errorf("Failed to connect to %s: %s", zone.Addr, err.Error())
		}

		// Ping to verify connectivity
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		zc := inapi.NewZoneServiceClient(conn)
		resp, err := zc.Ping(ctx, &inapi.PingRequest{})
		if err != nil {
			return fmt.Errorf("Ping to %s failed: %s", zone.Addr, err.Error())
		} else if resp.Message != "pong" {
			return fmt.Errorf("Unexpected ping response from %s: %s", zone.Addr, resp.Message)
		}

		fmt.Printf("Connected to zone %s (%s)\n", zone.Name, zone.Addr)

		// Update current zone and persist to config file
		if Config.CurrentZone != zone.Name {
			Config.CurrentZone = zone.Name
			if err := Flush(); err != nil {
				return fmt.Errorf("Warning: failed to save config (%s): %s",
					loadedConfigPath, err.Error())
			}
		}

		fmt.Println("Type 'exit' or 'quit' to leave the terminal.")
		fmt.Println("")

		// Enter interactive loop
		enterInteractiveMode(rootCmd)

		return nil
	}

	cmd := &cobra.Command{
		Use:   "login [zone-name]",
		Short: "Login to a zone and enter interactive session",
		Long:  "Connect to a configured zone and start an interactive shell with auto-completion.",
		RunE:  loginRun,
		Example: `  # Login with the current default zone
  instack login

  # Login to a specific zone by name
  instack login myzone

  # Inside the interactive session, type 'exit' or 'quit' to leave
 `,
	}

	return cmd
}

// Interactive mode core logic
func enterInteractiveMode(rootCmd *cobra.Command) {
	// Temporarily silence Cobra usage and error output to avoid cluttering the terminal
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true

	// Create an executor triggered when the user presses Enter
	executor := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}

		if line == "exit" || line == "quit" {
			fmt.Println("Goodbye!")
			os.Exit(0) // Exit interactive terminal
		}

		// Parse arguments
		args, err := shellwords.Parse(line)
		if err != nil {
			fmt.Printf("Argument parsing failed: %v\n", err)
			return
		}

		// Delegate to Cobra for execution
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	}

	// Create a completer: triggered on user input or Tab press, returns dynamic suggestions
	completer := func(d prompt.Document) []prompt.Suggest {
		// 1. Get the full text before the cursor
		text := d.TextBeforeCursor()
		if text == "" {
			return nil
		}

		// 2. If no space is present, auto-complete Cobra subcommands
		if !strings.Contains(text, " ") {
			var suggests []prompt.Suggest
			for _, cmd := range rootCmd.Commands() {
				// Filter out the login command itself and match the current input prefix
				if cmd.Name() != "login" && strings.HasPrefix(cmd.Name(), text) {
					suggests = append(suggests, prompt.Suggest{
						Text:        cmd.Name(),
						Description: cmd.Short,
					})
				}
			}
			return suggests
		}

		// 3. If a space is present (e.g. "deploy --loc"), start file path completion
		// Extract the text after the last space as the file search prefix
		lastSpaceIdx := strings.LastIndex(text, " ")
		currentInput := text[lastSpaceIdx+1:]

		// Use Glob to match local files or directories
		matches, err := filepath.Glob(currentInput + "*")
		if err != nil || len(matches) == 0 {
			return nil
		}

		var suggests []prompt.Suggest
		for _, match := range matches {
			desc := "file"
			// Mark as directory if applicable
			if info, err := os.Stat(match); err == nil && info.IsDir() {
				desc = "directory"
			}
			suggests = append(suggests, prompt.Suggest{
				Text:        match,
				Description: desc,
			})
		}
		return suggests
	}

	// Start go-prompt interactive terminal
	p := prompt.New(
		executor,
		completer,
		prompt.OptionPrefix(AppName+" "),
		prompt.OptionTitle(AppName+"-interactive-shell"),
	)
	p.Run()
}
