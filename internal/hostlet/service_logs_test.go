// Copyright 2026 Eryx <evoruri at gmail dot com>, All rights reserved.
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

package hostlet

import (
	"context"
	"strings"
	"testing"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/hostlet/hostapi"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

// fakeLogsDriver implements only the log path; the embedded interface makes
// the remaining driver methods compile (and panic if wrongly used here).
type fakeLogsDriver struct {
	hostapi.Driver
	logs []string
}

func (f *fakeLogsDriver) Name() string { return "fake" }

func (f *fakeLogsDriver) ContainerLogs(
	_ context.Context, _ string, _ uint32, _ int64,
) ([]string, error) {
	return f.logs, nil
}

func logsTestContext(t *testing.T) context.Context {
	t.Helper()
	ak := inauth.NewUserAccessKey()
	ak.Scopes = []string{inapi.AuthScope_Wildcard}
	av, err := inauth.NewAppValidator(inauth.NewAppCredential(ak).AuthToken())
	if err != nil {
		t.Fatal(err)
	}
	km := inauth.NewAccessKeyManager()
	km.Set(ak)
	if err := av.Verify(km); err != nil {
		t.Fatal(err)
	}
	return inauth.NewAppContext(context.Background(), av)
}

func TestContainerLogsHandler(t *testing.T) {
	prevDriver := ctrDriver
	prevHost := config.Config.Hostlet.HostId
	config.Config.Hostlet.HostId = "h-logs-1"
	t.Cleanup(func() {
		ctrDriver = prevDriver
		config.Config.Hostlet.HostId = prevHost
	})

	ctx := logsTestContext(t)
	svr := &hostInternalServer{}

	// Driver not ready.
	if _, err := svr.ContainerLogs(ctx, &inapi.ContainerLogsRequest{
		Container: "i8k_myapp_0",
	}); err == nil {
		t.Fatalf("no driver should error")
	}

	ctrDriver = &fakeLogsDriver{logs: []string{
		"2026-10-17T00:00:00Z line-1",
		"2026-10-17T00:00:01Z line-2",
	}}

	// Invalid container name.
	if _, err := svr.ContainerLogs(ctx, &inapi.ContainerLogsRequest{
		Container: "../etc",
	}); err == nil {
		t.Fatalf("invalid container name should error")
	}

	// Happy path with the default tail.
	resp, err := svr.ContainerLogs(ctx, &inapi.ContainerLogsRequest{
		Container: "i8k_myapp_0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Lines) != 2 || resp.Lines[0] != "2026-10-17T00:00:00Z line-1" {
		t.Fatalf("lines = %+v", resp.Lines)
	}
	if resp.Truncated {
		t.Fatalf("small output must not truncate")
	}

	// Truncation on the 1MB line budget: two 400KB lines fit (800KB), the
	// third would exceed the cap.
	big := strings.Repeat("x", 400<<10)
	ctrDriver = &fakeLogsDriver{logs: []string{big, big, big}}
	resp, err = svr.ContainerLogs(ctx, &inapi.ContainerLogsRequest{
		Container: "i8k_myapp_0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Lines) != 2 || !resp.Truncated {
		t.Fatalf("truncation: lines %d truncated %v", len(resp.Lines), resp.Truncated)
	}
}
