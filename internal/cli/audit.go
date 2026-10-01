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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/internal/client"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// exportPageSize is the AuditList page size used by export.
const exportPageSize = 500

// NewAuditCommand returns the "audit" command group.
func NewAuditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query, export and verify the audit log",
	}
	cmd.AddCommand(newAuditListCommand())
	cmd.AddCommand(newAuditExportCommand())
	cmd.AddCommand(newAuditVerifyCommand())
	return cmd
}

func newAuditListCommand() *cobra.Command {

	var (
		actorId  string
		action   string
		targetId string
		status   string
		since    string
		until    string
		limit    uint32
		showJson bool
	)

	run := func(cmd *cobra.Command, args []string) error {

		tsStart, tsEnd, err := parseTimeWindow(since, until)
		if err != nil {
			return err
		}

		_, zc, err := auditZoneClient()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := zc.AuditList(ctx, &inapi.AuditListRequest{
			TsStart:  tsStart,
			TsEnd:    tsEnd,
			ActorId:  actorId,
			Action:   action,
			TargetId: targetId,
			Status:   status,
			Limit:    limit,
			Revert:   false, // newest first
		})
		if err != nil {
			return fmt.Errorf("failed to list audit records: %s", err.Error())
		}

		var tbuf bytes.Buffer

		if !showJson && len(resp.Items) > 0 {

			table := tablewriter.NewTable(&tbuf)
			table.Configure(func(config *tablewriter.Config) {
				config.Header.Alignment.Global = tw.AlignLeft
			})
			table.Header([]any{
				"Time", "Actor", "Action", "Target", "Status", "Error",
			}...)

			for _, v := range resp.Items {
				table.Append([]any{
					time.UnixMilli(v.Ts).Format("2006-01-02 15:04:05"),
					auditActorLabel(v),
					shortAction(v.Action),
					v.TargetId,
					v.Status + auditSuppressed(v),
					truncateRunes(v.Error, 40),
				}...)
			}
			table.Render()
		} else {
			js, _ := json.MarshalIndent(resp, "", "  ")
			tbuf.Write(js)
			tbuf.WriteString("\n")
		}

		fmt.Print(tbuf.String())

		if resp.HasMore {
			fmt.Fprintln(os.Stderr, "more records exist, narrow the window or raise --limit")
		}

		return nil
	}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List audit records (newest first)",
		RunE:  run,
	}

	cmd.Flags().StringVar(&actorId, "actor", "", "filter by access key id (exact)")
	cmd.Flags().StringVar(&action, "action", "", "filter by action (exact, e.g. ZoneService/AppInstanceDeploy)")
	cmd.Flags().StringVar(&targetId, "target", "", "filter by target id (prefix, e.g. app-instance/web)")
	cmd.Flags().StringVar(&status, "status", "", "filter by status: ok | denied | error | unauthenticated")
	cmd.Flags().StringVar(&since, "since", "", "window start: RFC3339 timestamp or duration ago (e.g. 24h)")
	cmd.Flags().StringVar(&until, "until", "", "window end: RFC3339 timestamp")
	cmd.Flags().Uint32Var(&limit, "limit", 100, "max records to return (server cap 1000)")
	cmd.Flags().BoolVarP(&showJson, "show-json", "j", false, "show raw response with json")

	return cmd
}

func newAuditExportCommand() *cobra.Command {

	var (
		output string
		since  string
		until  string
	)

	run := func(cmd *cobra.Command, args []string) error {

		tsStart, tsEnd, err := parseTimeWindow(since, until)
		if err != nil {
			return err
		}

		zone, zc, err := auditZoneClient()
		if err != nil {
			return err
		}

		out := io.Writer(os.Stdout)
		if output != "" {
			f, err := os.Create(output)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			defer f.Close()
			out = f
		}

		hasher := sha256.New()

		var (
			count               int64
			offset              string
			firstHash, lastHash string
			firstId, lastId     string
		)

		for {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

			resp, err := zc.AuditList(ctx, &inapi.AuditListRequest{
				TsStart: tsStart,
				TsEnd:   tsEnd,
				Limit:   exportPageSize,
				Revert:  true, // oldest first for a stable export stream
				Offset:  offset,
			})
			cancel()
			if err != nil {
				return fmt.Errorf("failed to list audit records: %s", err.Error())
			}

			for _, v := range resp.Items {
				line, err := json.Marshal(v)
				if err != nil {
					return fmt.Errorf("failed to encode audit record: %w", err)
				}
				line = append(line, '\n')
				if _, err := out.Write(line); err != nil {
					return fmt.Errorf("failed to write audit record: %w", err)
				}
				hasher.Write(line)

				count++
				if firstHash == "" {
					firstHash, firstId = v.Hash, v.Id
				}
				lastHash, lastId = v.Hash, v.Id
			}

			if !resp.HasMore || len(resp.Items) == 0 {
				break
			}
			offset = resp.Items[len(resp.Items)-1].Id
		}

		// The manifest is the tamper-evidence root: stderr keeps it out of
		// the JSONL stream; archive it with the export externally.
		fmt.Fprintf(os.Stderr,
			"manifest: zone=%s count=%d sha256=%s first_id=%s first_hash=%s last_id=%s last_hash=%s\n",
			zone.Addr,
			count,
			hex.EncodeToString(hasher.Sum(nil)),
			firstId, firstHash,
			lastId, lastHash,
		)

		return nil
	}

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export audit records as JSONL (manifest to stderr)",
		RunE:  run,
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "output file (default stdout)")
	cmd.Flags().StringVar(&since, "since", "", "window start: RFC3339 timestamp or duration ago (e.g. 24h)")
	cmd.Flags().StringVar(&until, "until", "", "window end: RFC3339 timestamp")

	return cmd
}

func newAuditVerifyCommand() *cobra.Command {

	run := func(cmd *cobra.Command, args []string) error {

		_, zc, err := auditZoneClient()
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		resp, err := zc.AuditVerify(ctx, &inapi.AuditVerifyRequest{})
		if err != nil {
			return fmt.Errorf("failed to verify audit chain: %s", err.Error())
		}

		if !resp.Valid {
			fmt.Printf("audit chain BROKEN at %s (count=%d head_hash=%s)\n",
				resp.BrokenAt, resp.Count, resp.HeadHash)
			return fmt.Errorf("audit chain verification failed")
		}

		fmt.Printf("audit chain valid (count=%d first_id=%s last_id=%s head_hash=%s)\n",
			resp.Count, resp.FirstId, resp.LastId, resp.HeadHash)
		return nil
	}

	return &cobra.Command{
		Use:   "verify",
		Short: "Verify the audit hash chain and daily anchors",
		RunE:  run,
	}
}

// auditZoneClient resolves the current zone config and returns a connected
// ZoneService client.
func auditZoneClient() (*ConfigZone, inapi.ZoneServiceClient, error) {

	zone, err := Config.Zone("")
	if err != nil {
		return nil, nil, err
	}

	ak, err := zone.AccessKey()
	if err != nil {
		return nil, nil, fmt.Errorf("invalid access key: %w", err)
	}

	conn, err := client.Connect(zone.Addr, ak, false)
	if err != nil {
		return nil, nil, fmt.Errorf(
			"failed to connect to zone server %s: %w", zone.Addr, err)
	}

	return zone, inapi.NewZoneServiceClient(conn), nil
}

// parseTimeWindow resolves the --since/--until flags to unix milliseconds.
// --since accepts an RFC3339 timestamp or a Go duration relative to now
// ("24h"); --until accepts an RFC3339 timestamp.
func parseTimeWindow(since, until string) (int64, int64, error) {

	parse := func(v, name string) (int64, error) {
		if v == "" {
			return 0, nil
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UnixMilli(), nil
		}
		if d, err := time.ParseDuration(v); err == nil {
			return time.Now().Add(-d).UnixMilli(), nil
		}
		return 0, fmt.Errorf("invalid %s %q: RFC3339 timestamp or duration required", name, v)
	}

	tsStart, err := parse(since, "--since")
	if err != nil {
		return 0, 0, err
	}

	tsEnd, err := parse(until, "--until")
	if err != nil {
		return 0, 0, err
	}

	if tsStart > 0 && tsEnd > 0 && tsStart > tsEnd {
		return 0, 0, errors.New("--since is later than --until")
	}

	return tsStart, tsEnd, nil
}

func auditActorLabel(v *inapi.AuditRecord) string {
	if v.ActorId == "" {
		return v.ActorType
	}
	if v.ActorUser != "" {
		return fmt.Sprintf("%s(%s)", v.ActorUser, v.ActorId)
	}
	return fmt.Sprintf("%s:%s", v.ActorType, v.ActorId)
}

// shortAction trims the service prefix: "ZoneService/AppInstanceDeploy"
// becomes "AppInstanceDeploy".
func shortAction(action string) string {
	if i := strings.LastIndexByte(action, '/'); i >= 0 {
		return action[i+1:]
	}
	return action
}

func auditSuppressed(v *inapi.AuditRecord) string {
	if v.SuppressedCount > 0 {
		return fmt.Sprintf(" (+%d)", v.SuppressedCount)
	}
	return ""
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
