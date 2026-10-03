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

package zonelet

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lynkdb/kvgo/v2/pkg/kvrep"
	"github.com/lynkdb/kvgo/v2/pkg/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sysinner/innerstack/v2/internal/audit"
	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	zstatus "github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

const auditTestZone = "audittest"

// setupAuditTest wires a temp zonelet database, the zone config and leader
// state, returning an admin context and a cleanup func.
func setupAuditTest(t *testing.T) context.Context {
	t.Helper()

	db, err := kvrep.NewReplica(&storage.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	prevDB := data.Zonelet
	data.Zonelet = db
	prevZone := config.Config.Zonelet.ZoneName
	config.Config.Zonelet.ZoneName = auditTestZone

	zstatus.ZoneletLeader = config.Config.Hostlet.HostId
	zstatus.ZoneletLeaderUpdated = time.Now().UnixMilli()

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

	t.Cleanup(func() {
		data.Zonelet = prevDB
		config.Config.Zonelet.ZoneName = prevZone
		db.Close()
	})

	return inauth.NewAppContext(context.Background(), av)
}

// seedAuditRecords writes n plain records with the given action/status
// directly to the store (the list handler does not verify the chain). Ids
// use the real {ts_ms_hex12}{rand_hex4} format so key order and the
// time-window bounds behave exactly as in production.
func seedAuditRecords(t *testing.T, n int, action, st string) {
	t.Helper()
	for i := 0; i < n; i++ {
		ts := time.Now().UnixMilli() + int64(i)
		id := audit.AuditIdOfTs(ts) + fmt.Sprintf("%04x", i)
		rec := &inapi.AuditRecord{
			Zone:      auditTestZone,
			Id:        id,
			Ts:        ts,
			Action:    action,
			Status:    st,
			ActorId:   "actor1",
			ActorUser: "sysadmin",
		}
		if rs := data.Zonelet.NewWriter(
			inapi.NsZoneletAuditLog(auditTestZone, id), rec).Exec(); !rs.OK() {
			t.Fatalf("seed record: %s", rs.ErrorMessage())
		}
	}
}

// TestServiceAuditListPagination exercises cursor pagination through the
// RPC handler.
func TestServiceAuditListPagination(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	seedAuditRecords(t, 25, "test/action", inapi.AuditStatusOK)

	seen := map[string]bool{}
	offset := ""
	for {
		rsp, err := srv.AuditList(ctx, &inapi.AuditListRequest{
			Limit:  10,
			Offset: offset,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(rsp.Items) == 0 {
			break
		}
		for _, v := range rsp.Items {
			if seen[v.Id] {
				t.Fatalf("record %s returned twice", v.Id)
			}
			seen[v.Id] = true
		}
		if !rsp.HasMore {
			break
		}
		offset = rsp.Items[len(rsp.Items)-1].Id
	}

	if len(seen) != 25 {
		t.Fatalf("pagination covered %d records, want 25", len(seen))
	}
}

// TestServiceAuditListFilters covers the filter parameters and the status
// validation.
func TestServiceAuditListFilters(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	seedAuditRecords(t, 2, "ZoneService/ZoneInit", inapi.AuditStatusOK)
	seedAuditRecords(t, 1, "ZoneService/ZoneInit", inapi.AuditStatusDenied)

	fixtures := []struct {
		name      string
		req       *inapi.AuditListRequest
		wantCount int
	}{
		{
			name:      "all",
			req:       &inapi.AuditListRequest{},
			wantCount: 3,
		},
		{
			name:      "status",
			req:       &inapi.AuditListRequest{Status: inapi.AuditStatusDenied},
			wantCount: 1,
		},
		{
			name: "action",
			req: &inapi.AuditListRequest{
				Action: "ZoneService/ZoneInit",
				Status: inapi.AuditStatusOK,
			},
			wantCount: 2,
		},
		{
			name:      "actor",
			req:       &inapi.AuditListRequest{ActorId: "actor1"},
			wantCount: 3,
		},
		{
			name:      "actor_no_match",
			req:       &inapi.AuditListRequest{ActorId: "nobody"},
			wantCount: 0,
		},
		{
			name:      "actor_user",
			req:       &inapi.AuditListRequest{ActorUser: "sysadmin"},
			wantCount: 3,
		},
		{
			name:      "actor_user_no_match",
			req:       &inapi.AuditListRequest{ActorUser: "nobody"},
			wantCount: 0,
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			rsp, err := srv.AuditList(ctx, f.req)
			if err != nil {
				t.Fatal(err)
			}
			if len(rsp.Items) != f.wantCount {
				t.Fatalf("got %d items, want %d", len(rsp.Items), f.wantCount)
			}
		})
	}

	if _, err := srv.AuditList(ctx, &inapi.AuditListRequest{
		Status: "bogus",
	}); err == nil {
		t.Fatal("invalid status filter should be rejected")
	}
}

// TestServiceAuditListTimeWindow covers ts_start/ts_end.
func TestServiceAuditListTimeWindow(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	seedAuditRecords(t, 1, "a/x", inapi.AuditStatusOK)

	// Millisecond boundaries: leave a clear gap around the split point.
	time.Sleep(5 * time.Millisecond)
	split := time.Now().UnixMilli()
	time.Sleep(5 * time.Millisecond)

	seedAuditRecords(t, 2, "a/y", inapi.AuditStatusOK)

	rsp, err := srv.AuditList(ctx, &inapi.AuditListRequest{TsStart: split})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Items) != 2 {
		t.Fatalf("got %d items after ts_start, want 2", len(rsp.Items))
	}

	rsp, err = srv.AuditList(ctx, &inapi.AuditListRequest{TsEnd: split})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Items) != 1 {
		t.Fatalf("got %d items before ts_end, want 1", len(rsp.Items))
	}
}

// TestServiceAuditVerifyEmpty covers the handler wiring on an empty store.
func TestServiceAuditVerifyEmpty(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	rsp, err := srv.AuditVerify(ctx, &inapi.AuditVerifyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !rsp.Valid || rsp.Count != 0 {
		t.Fatalf("empty verify = %+v", rsp)
	}
}

// TestServiceAuditAuthAndLeader covers the scope gate (audit:ro, mapped to
// PermissionDenied) and the leader gate.
func TestServiceAuditAuthAndLeader(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	// Drop the scope: a key with no wildcard must be denied with
	// PermissionDenied so the audit interceptor labels it status=denied.
	ak := inauth.NewUserAccessKey()
	av, err := inauth.NewAppValidator(inauth.NewAppCredential(ak).AuthToken())
	if err != nil {
		t.Fatal(err)
	}
	km := inauth.NewAccessKeyManager()
	km.Set(ak)
	if err := av.Verify(km); err != nil {
		t.Fatal(err)
	}
	noScopeCtx := inauth.NewAppContext(context.Background(), av)

	_, err = srv.AuditList(noScopeCtx, &inapi.AuditListRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("err code = %v, want PermissionDenied", status.Code(err))
	}

	_, err = srv.AuditVerify(noScopeCtx, &inapi.AuditVerifyRequest{})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("err code = %v, want PermissionDenied", status.Code(err))
	}

	// Not the leader: rejected.
	prevLeader := zstatus.ZoneletLeader
	zstatus.ZoneletLeader = "other-host"
	defer func() { zstatus.ZoneletLeader = prevLeader }()

	if _, err := srv.AuditList(ctx, &inapi.AuditListRequest{}); err == nil {
		t.Fatal("non-leader AuditList should fail")
	}
	if _, err := srv.AuditVerify(ctx, &inapi.AuditVerifyRequest{}); err == nil {
		t.Fatal("non-leader AuditVerify should fail")
	}
}
