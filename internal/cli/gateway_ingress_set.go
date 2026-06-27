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
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/internal/client"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func NewGatewayIngressSetCommand() *cobra.Command {

	var (
		domain      string
		description string
		action      string
		letsencrypt bool
		routes      bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		if domain == "" {
			return fmt.Errorf("domain is required")
		}

		item := &inapi.GatewayIngress{
			Meta:        &inapi.Metadata{},
			Domain:      domain,
			Description: description,
			Action:      action,
			Options: &inapi.GatewayIngress_Options{
				LetsencryptEnable: letsencrypt,
			},
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
			return fmt.Errorf("failed to connect to zone server %s: %w", zone.Addr, err)
		}
		defer conn.Close()

		zc := inapi.NewZoneServiceClient(conn)

		// Fetch existing ingress by domain to preserve fields not explicitly
		// set by the user, preventing accidental overwrites.
		infoCtx, infoCancel := context.WithTimeout(context.Background(), 10*time.Second)
		infoResp, infoErr := zc.GatewayIngressInfo(infoCtx, &inapi.GatewayIngressInfoRequest{
			Name: item.Domain,
		})
		infoCancel()

		if infoErr == nil && infoResp.Item != nil {
			existing := infoResp.Item

			if existing.Meta != nil {
				item.Meta.Id = existing.Meta.Id
				item.Meta.Created = existing.Meta.Created
			}

			// Only override fields explicitly set by user
			if !cmd.Flags().Changed("description") {
				item.Description = existing.Description
			}
			if !cmd.Flags().Changed("action") {
				item.Action = existing.Action
			}
			if !cmd.Flags().Changed("letsencrypt") {
				item.Options = existing.Options
			}

			// Preserve existing routes
			if len(existing.Routes) > 0 {
				item.Routes = existing.Routes
			}
		}

		// Interactive routes editing mode
		if routes {
			editedRoutes, err := interactiveRoutesEdit(item.Routes)
			if err != nil {
				return err
			}
			item.Routes = editedRoutes
		}

		req := &inapi.GatewayIngressSetRequest{
			Item: item,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		resp, err := zc.GatewayIngressSet(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to set gateway ingress: %w", err)
		}

		if resp.Item != nil {
			fmt.Printf("Gateway ingress '%s' set successfully\n\n", resp.Item.Domain)
			printIngressItem(resp.Item)
		} else {
			fmt.Printf("Gateway ingress set successfully\n")
		}

		return nil
	}

	cmd := &cobra.Command{
		Use:   "gw-ingress-set",
		Short: "Create or update a gateway ingress rule",
		Long:  `Create or update a gateway ingress rule using individual flags (--domain, etc.).`,
		RunE:  run,
		Example: `  # Set ingress with flags
  innerstack gw-ingress-set --domain example.com

  # Set ingress with interactive routes editing
  innerstack gw-ingress-set --domain example.com --routes`,
	}

	cmd.Flags().StringVarP(&domain, "domain", "d", "", "Domain name for the ingress (required)")
	cmd.Flags().StringVarP(&description, "description", "", "", "Description of the ingress")
	cmd.Flags().StringVarP(&action, "action", "", inapi.GatewayIngressActionEnable, "Action for the ingress (enable|disable)")
	cmd.Flags().BoolVarP(&letsencrypt, "letsencrypt", "", false, "Enable Let's Encrypt TLS certificate")
	cmd.Flags().BoolVarP(&routes, "routes", "r", false, "Interactively edit routes")

	return cmd
}

// interactiveRoutesEdit provides an interactive prompt-based editor for
// managing HTTP routes on a gateway ingress.
func interactiveRoutesEdit(existing []*inapi.GatewayIngress_HttpRoute) ([]*inapi.GatewayIngress_HttpRoute, error) {
	routes := make([]*inapi.GatewayIngress_HttpRoute, len(existing))
	copy(routes, existing)

	scanner := bufio.NewScanner(os.Stdin)

	validTypes := []string{
		inapi.GatewayIngressType_Instance,
		inapi.GatewayIngressType_Upstream,
		inapi.GatewayIngressType_Redirect,
	}

	for {
		fmt.Println()
		printRoutes(routes)
		fmt.Println()
		fmt.Println("Actions: [a]dd  [e]dit  [d]elete  [q]uit & apply  [c]ancel")
		fmt.Print("Choose action: ")

		if !scanner.Scan() {
			return nil, fmt.Errorf("failed to read input")
		}

		choice := strings.TrimSpace(strings.ToLower(scanner.Text()))

		switch choice {
		case "a", "add":
			route, err := promptRoute(scanner, validTypes, nil)
			if err != nil {
				return nil, err
			}
			if route != nil {
				routes = append(routes, route)
				fmt.Println("Route added.")
			}

		case "e", "edit":
			if len(routes) == 0 {
				fmt.Println("No routes to edit.")
				continue
			}
			fmt.Printf("Enter route number to edit [1-%d]: ", len(routes))
			if !scanner.Scan() {
				return nil, fmt.Errorf("failed to read input")
			}
			idx, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
			if err != nil || idx < 1 || idx > len(routes) {
				fmt.Println("Invalid route number.")
				continue
			}
			route, err := promptRoute(scanner, validTypes, routes[idx-1])
			if err != nil {
				return nil, err
			}
			if route != nil {
				routes[idx-1] = route
				fmt.Println("Route updated.")
			}

		case "d", "delete", "del":
			if len(routes) == 0 {
				fmt.Println("No routes to delete.")
				continue
			}
			fmt.Printf("Enter route number to delete [1-%d]: ", len(routes))
			if !scanner.Scan() {
				return nil, fmt.Errorf("failed to read input")
			}
			idx, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
			if err != nil || idx < 1 || idx > len(routes) {
				fmt.Println("Invalid route number.")
				continue
			}
			routes = append(routes[:idx-1], routes[idx:]...)
			fmt.Println("Route deleted.")

		case "q", "quit", "done":
			return routes, nil

		case "c", "cancel":
			return existing, nil

		default:
			fmt.Println("Invalid action. Please choose a, e, d, q, or c.")
		}
	}
}

// promptRoute interactively prompts for a single HTTP route entry.
// If current is non-nil, its values are shown as defaults (press Enter to keep).
func promptRoute(scanner *bufio.Scanner, validTypes []string, current *inapi.GatewayIngress_HttpRoute) (*inapi.GatewayIngress_HttpRoute, error) {

	// Prompt for path
	promptHint := "  Path (e.g. / or /api/v1)"
	if current != nil {
		promptHint = fmt.Sprintf("  Path [%s]", current.Path)
	}
	fmt.Printf("%s: ", promptHint)
	if !scanner.Scan() {
		return nil, fmt.Errorf("failed to read input")
	}
	path := strings.TrimSpace(scanner.Text())
	if path == "" {
		if current != nil {
			path = current.Path
		} else {
			fmt.Println("Path is required, route skipped.")
			return nil, nil
		}
	}

	// Prompt for type (repeat until a valid type is entered)
	routeType := promptValidType(scanner, validTypes, current)

	// Prompt for targets based on route type
	targets := promptTargets(scanner, routeType, current)

	route := &inapi.GatewayIngress_HttpRoute{
		Path:    path,
		Type:    routeType,
		Targets: targets,
		Action:  inapi.GatewayIngressActionEnable,
	}

	return route, nil
}

// targetPromptConfig holds the prompt message and validation for each route type.
type targetPromptConfig struct {
	hint    string // displayed in the prompt
	example string // example value
}

var targetPrompts = map[string]targetPromptConfig{
	inapi.GatewayIngressType_Instance: {
		hint:    "Name:Port",
		example: "my-app:8080",
	},
	inapi.GatewayIngressType_Upstream: {
		hint:    "IP:Port",
		example: "10.0.0.1:8080",
	},
	inapi.GatewayIngressType_Redirect: {
		hint:    "URL",
		example: "https://example.com/path",
	},
}

// promptTargets prompts for targets based on the route type, validating
// each entry and re-prompting on invalid input.
// If current is non-nil and has targets, they are pre-filled as defaults.
// Each target includes an address and an optional weight.
func promptTargets(scanner *bufio.Scanner, routeType string, current *inapi.GatewayIngress_HttpRoute) []*inapi.GatewayIngress_HttpRoute_Target {
	cfg, ok := targetPrompts[routeType]
	if !ok {
		cfg = targetPromptConfig{
			hint:    "Target",
			example: "value",
		}
	}

	var targets []*inapi.GatewayIngress_HttpRoute_Target

	// Pre-fill existing targets when editing
	if current != nil && len(current.Targets) > 0 {
		targets = make([]*inapi.GatewayIngress_HttpRoute_Target, len(current.Targets))
		copy(targets, current.Targets)
		fmt.Printf("  %s (current: %s): ", cfg.hint, formatTargetAddrs(targets))
		if !scanner.Scan() {
			return targets
		}
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			return targets
		}
		// Replace all targets with new input
		targets = targets[:0]
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			addr, weight, err := parseTargetEntry(routeType, entry)
			if err != nil {
				fmt.Printf("  Invalid %s '%s': %s\n", cfg.hint, entry, err)
				continue
			}
			targets = append(targets, &inapi.GatewayIngress_HttpRoute_Target{
				Backend: addr,
				Weight:  weight,
			})
		}
		if len(targets) == 0 {
			fmt.Printf("  At least one target is required, keeping previous values.\n")
			targets = make([]*inapi.GatewayIngress_HttpRoute_Target, len(current.Targets))
			copy(targets, current.Targets)
		}
		return targets
	}

	for {
		if len(targets) > 0 {
			fmt.Printf("  %s (current: %s, enter empty to finish, or continue adding): ", cfg.hint, formatTargetAddrs(targets))
		} else {
			fmt.Printf("  %s (e.g. %s): ", cfg.hint, cfg.example)
		}

		if !scanner.Scan() {
			return targets
		}

		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			if len(targets) == 0 {
				fmt.Printf("  At least one target is required.\n")
				continue
			}
			break
		}

		// Parse comma-separated values
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			addr, weight, err := parseTargetEntry(routeType, entry)
			if err != nil {
				fmt.Printf("  Invalid %s '%s': %s\n", cfg.hint, entry, err)
				continue
			}
			targets = append(targets, &inapi.GatewayIngress_HttpRoute_Target{
				Backend: addr,
				Weight:  weight,
			})
		}

		if len(targets) > 0 && !strings.Contains(raw, ",") {
			// Single entry provided; allow user to add more or finish
			fmt.Printf("  Added. Enter another or empty to finish.\n")
			continue
		}
	}

	return targets
}

// parseTargetEntry parses a target entry string in the format "addr[:port]" or "addr[:port];weight=N".
// Returns the validated address, weight (0 if not specified), and any validation error.
func parseTargetEntry(routeType, entry string) (string, int32, error) {
	var (
		addr   string
		weight int32
	)
	// Check for weight suffix: "addr;weight=N" or "addr;w=N"
	if idx := strings.LastIndex(entry, ";"); idx > 0 {
		suffix := entry[idx+1:]
		entry = entry[:idx]
		if strings.HasPrefix(suffix, "weight=") {
			wStr := strings.TrimPrefix(suffix, "weight=")
			w, err := strconv.ParseInt(wStr, 10, 32)
			if err != nil || w < 0 {
				return "", 0, fmt.Errorf("invalid weight value '%s'", wStr)
			}
			weight = int32(w)
		} else if strings.HasPrefix(suffix, "w=") {
			wStr := strings.TrimPrefix(suffix, "w=")
			w, err := strconv.ParseInt(wStr, 10, 32)
			if err != nil || w < 0 {
				return "", 0, fmt.Errorf("invalid weight value '%s'", wStr)
			}
			weight = int32(w)
		}
	}

	addr = strings.TrimSpace(entry)
	if err := validateTarget(routeType, addr); err != nil {
		return "", 0, err
	}

	return addr, weight, nil
}

// validateTarget validates a single target value based on the route type.
func validateTarget(routeType, target string) error {
	switch routeType {
	case inapi.GatewayIngressType_Instance:
		parts := strings.Split(target, ":")
		if len(parts) != 2 {
			return fmt.Errorf("must be in format Name:Port")
		}
		if err := inapi.DNSLabelValid(parts[0]); err != nil {
			return fmt.Errorf("invalid AppInstance Name '%s': %w", parts[0], err)
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil || port < 80 || port > 65505 {
			return fmt.Errorf("port must be between 80 and 65505")
		}

	case inapi.GatewayIngressType_Upstream:
		parts := strings.Split(target, ":")
		if len(parts) != 2 {
			return fmt.Errorf("must be in format IP:Port")
		}
		if ip := net.ParseIP(parts[0]); ip == nil || ip.To4() == nil {
			return fmt.Errorf("invalid IPv4 address '%s'", parts[0])
		}
		port, err := strconv.Atoi(parts[1])
		if err != nil || port < 80 || port > 65505 {
			return fmt.Errorf("port must be between 80 and 65505")
		}

	case inapi.GatewayIngressType_Redirect:
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			return fmt.Errorf("must be a valid URL (http:// or https://)")
		}
		idx := strings.Index(target, "://")
		if idx+3 >= len(target) {
			return fmt.Errorf("invalid URL")
		}
	}

	return nil
}

// promptValidType repeatedly prompts for a route type until a valid value
// is entered. If current is non-nil, its type is used as default on empty input.
func promptValidType(scanner *bufio.Scanner, validTypes []string, current *inapi.GatewayIngress_HttpRoute) string {
	defaultType := inapi.GatewayIngressType_Instance
	if current != nil {
		defaultType = current.Type
	}
	for {
		fmt.Printf("  Type [%s] (default %s): ", strings.Join(validTypes, "/"), defaultType)
		if !scanner.Scan() {
			return defaultType
		}
		routeType := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if routeType == "" {
			fmt.Printf("  Using type: %s\n", defaultType)
			return defaultType
		}
		for _, t := range validTypes {
			if routeType == t {
				return routeType
			}
		}
		fmt.Printf("  Invalid type '%s', please enter one of [%s]\n", routeType, strings.Join(validTypes, "/"))
	}
}

// formatTargetAddrs formats a list of target structs into a display string.
func formatTargetAddrs(ts []*inapi.GatewayIngress_HttpRoute_Target) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		if t.Weight > 0 {
			parts[i] = fmt.Sprintf("%s (weight:%d)", t.Backend, t.Weight)
		} else {
			parts[i] = t.Backend
		}
	}
	return strings.Join(parts, ", ")
}

// printIngressItem displays the ingress item details in a formatted table.
func printIngressItem(item *inapi.GatewayIngress) {
	if item == nil {
		return
	}

	var tbuf bytes.Buffer
	table := tablewriter.NewTable(&tbuf)

	table.Configure(func(config *tablewriter.Config) {
		config.Header.Alignment.Global = tw.AlignLeft
	})

	table.Header([]any{
		"Domain", "Description", "Action", "Path", "Type", "Targets",
	}...)

	if len(item.Routes) == 0 {
		table.Append([]any{
			item.Domain,
			item.Description,
			item.Action,
			"", "", "",
		})
	} else {
		for i, route := range item.Routes {
			targets := formatTargetAddrs(route.Targets)
			if i == 0 {
				table.Append([]any{
					item.Domain,
					item.Description,
					item.Action,
					route.Path,
					route.Type,
					targets,
				})
			} else {
				table.Append([]any{
					"", "", "",
					route.Path,
					route.Type,
					targets,
				})
			}
		}
	}

	table.Render()
	fmt.Println(tbuf.String())
}

// printRoutes displays the current routes list in a numbered format.
func printRoutes(routes []*inapi.GatewayIngress_HttpRoute) {
	if len(routes) == 0 {
		fmt.Println("Current routes: (none)")
		return
	}

	fmt.Println("Current routes:")
	fmt.Printf("  %-4s  %-30s  %-10s  %s\n", "No.", "Path", "Type", "Targets")
	fmt.Printf("  %-4s  %-30s  %-10s  %s\n", "----", "----", "----", "-------")
	for i, r := range routes {
		targets := formatTargetAddrs(r.Targets)
		fmt.Printf("  %-4d  %-30s  %-10s  %s\n", i+1, r.Path, r.Type, targets)
	}
}
