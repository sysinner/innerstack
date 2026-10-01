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

package audit

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

// testCtx builds an authenticated incoming context with a peer address,
// mirroring the production auth interceptor (parse + verify, so the
// AppContext carries a resolved access key).
func testCtx(t *testing.T, ak *inauth.AccessKey) context.Context {
	t.Helper()

	ctx := context.Background()

	if ak != nil {
		av, err := inauth.NewAppValidator(inauth.NewAppCredential(ak).AuthToken())
		if err != nil {
			t.Fatalf("mint credential: %v", err)
		}
		km := inauth.NewAccessKeyManager()
		km.Set(ak)
		if err := av.Verify(km); err != nil {
			t.Fatalf("verify credential: %v", err)
		}
		ctx = inauth.NewAppContext(ctx, av)
	}

	ctx = peer.NewContext(ctx, &peer.Peer{
		Addr: &net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 4242},
	})

	return ctx
}

// TestStatusMapping covers the gRPC-code to audit-status mapping.
func TestStatusMapping(t *testing.T) {

	fixtures := []struct {
		name string
		err  error
		want string
	}{
		{"ok", nil, inapi.AuditStatusOK},
		{"denied", status.Error(codes.PermissionDenied, "no"), inapi.AuditStatusDenied},
		{"unauthenticated", status.Error(codes.Unauthenticated, "who"), inapi.AuditStatusUnauthenticated},
		{"error", status.Error(codes.Internal, "boom"), inapi.AuditStatusError},
		{"plain_error", errTest("plain"), inapi.AuditStatusError},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			if got := statusOf(f.err); got != f.want {
				t.Fatalf("statusOf = %q, want %q", got, f.want)
			}
		})
	}
}

// TestBuildRecordSelection verifies which calls produce a record: audited
// methods always (unless declined), non-audited methods only when denied.
func TestBuildRecordSelection(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	ak := inauth.NewUserAccessKey()
	ak.User = "alice"

	fixtures := []struct {
		name    string
		method  string
		req     any
		err     error
		wantNil bool
	}{
		{
			name:   "audited_method_ok",
			method: "/inapi.ZoneService/ZoneInit",
			req:    &inapi.ZoneInitRequest{Name: "z"},
		},
		{
			name:   "audited_method_error",
			method: "/inapi.ZoneService/ZoneInit",
			req:    &inapi.ZoneInitRequest{Name: "z"},
			err:    errTest("bad name"),
		},
		{
			name:    "readonly_method_ok_skipped",
			method:  "/inapi.ZoneService/ZoneInfo",
			req:     &inapi.ZoneInfoRequest{},
			wantNil: true,
		},
		{
			name:   "readonly_method_denied_recorded",
			method: "/inapi.ZoneService/ZoneInfo",
			req:    &inapi.ZoneInfoRequest{},
			err:    status.Error(codes.PermissionDenied, "missing zone:ro scope"),
		},
		{
			name:    "internal_method_ok_skipped",
			method:  "/inapi.ZoneInternalService/HostStatusUpdate",
			req:     &inapi.HostStatusUpdateRequest{},
			wantNil: true,
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {

			rec := buildRecord(testCtx(t, ak), f.method, f.req, nil, f.err)

			if f.wantNil {
				if rec != nil {
					t.Fatalf("expected no record, got %+v", rec)
				}
				return
			}
			if rec == nil {
				t.Fatal("expected a record")
			}

			if rec.Zone != testZone {
				t.Fatalf("zone = %q, want %q", rec.Zone, testZone)
			}
			if rec.Action != f.method {
				t.Fatalf("action = %q", rec.Action)
			}
			if rec.SourceIp != "10.1.2.3" {
				t.Fatalf("source_ip = %q, want 10.1.2.3", rec.SourceIp)
			}
			if rec.ActorId != ak.Id || rec.ActorUser != "alice" ||
				rec.ActorType != inapi.AuditActorUser {
				t.Fatalf("actor = %q/%q/%q", rec.ActorId, rec.ActorUser, rec.ActorType)
			}
			if f.err != nil && rec.Error == "" {
				t.Fatal("error field empty for failed call")
			}
		})
	}
}

// TestBuildRecordDeclinedByExtractor: an intermediate PackagePush chunk
// produces no record even on an audited method.
func TestBuildRecordDeclinedByExtractor(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	rec := buildRecord(
		testCtx(t, inauth.NewUserAccessKey()),
		"/inapi.ZoneService/PackagePush",
		&inapi.PackagePushRequest{Id: "pkg1"},
		&inapi.PackagePushResponse{
			File: &inapi.PackageFile{State: inapi.PackageFileStateUploading},
		},
		nil,
	)
	if rec != nil {
		t.Fatalf("intermediate chunk should not be audited, got %+v", rec)
	}
}

// TestBuildRecordActorTypes covers the actor-type derivation.
func TestBuildRecordActorTypes(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	hostKey := inauth.NewAccessKey()
	hostKey.Id = "6985b60604d7"
	hostKey.Scopes = []string{
		inapi.AuthScope_Host_Write + ":" + hostKey.Id,
		inapi.AuthScope_Package_Read,
	}

	appKey := inauth.NewAppAccessKey()

	fixtures := []struct {
		name string
		ak   *inauth.AccessKey
		want string
	}{
		{"user", inauth.NewUserAccessKey(), inapi.AuditActorUser},
		{"host", hostKey, inapi.AuditActorHost},
		{"app", appKey, inapi.AuditActorApp},
		{"anonymous", nil, inapi.AuditActorAnonymous},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			rec := buildRecord(testCtx(t, f.ak),
				"/inapi.ZoneService/ZoneInit",
				&inapi.ZoneInitRequest{Name: "z"}, nil, nil)
			if rec == nil {
				t.Fatal("no record")
			}
			if rec.ActorType != f.want {
				t.Fatalf("actor_type = %q, want %q", rec.ActorType, f.want)
			}
		})
	}
}

// TestAuthFailureDedupWindow verifies the 60s (kid, method) dedup: one
// record per window, later failures carried as suppressed_count.
func TestAuthFailureDedupWindow(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	m := &Manager{
		db:           nil, // AuthFailure never touches the db directly
		queue:        make(chan *inapi.AuditRecord, 16),
		authFailures: authFailureCache{items: map[string]*authFailureEntry{}},
	}

	oldMgr := Mgr
	Mgr = m
	t.Cleanup(func() { Mgr = oldMgr })

	ak := inauth.NewUserAccessKey()

	mdCtx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		inauth.AppHttpHeaderKey, inauth.NewAppCredential(ak).AuthToken()))
	mdCtx = peer.NewContext(mdCtx, &peer.Peer{
		Addr: &net.TCPAddr{IP: net.ParseIP("10.9.9.9"), Port: 9},
	})

	const method = "/inapi.ZoneService/HostStatusUpdate"

	// Burst of failures within the window: only the first is recorded.
	for range 4 {
		AuthFailure(mdCtx, method, errTest("bad signature"))
	}

	if len(m.queue) != 1 {
		t.Fatalf("queue len = %d, want 1", len(m.queue))
	}

	first := <-m.queue
	if first.SuppressedCount != 0 {
		t.Fatalf("first record suppressed_count = %d, want 0", first.SuppressedCount)
	}
	if first.Status != inapi.AuditStatusUnauthenticated {
		t.Fatalf("status = %q", first.Status)
	}
	if first.ActorId != ak.Id {
		t.Fatalf("actor_id = %q, want %q (parseable token)", first.ActorId, ak.Id)
	}
	if first.ActorType != inapi.AuditActorAnonymous {
		t.Fatalf("actor_type = %q, want Anonymous", first.ActorType)
	}
	if first.SourceIp != "10.9.9.9" {
		t.Fatalf("source_ip = %q", first.SourceIp)
	}

	// Window expires: the next failure carries the suppressed count.
	k := ak.Id + "\x00" + method + "\x00" + codes.Unauthenticated.String()
	m.authFailures.mu.Lock()
	m.authFailures.items[k].window = time.Now().Unix() - 61
	m.authFailures.mu.Unlock()

	AuthFailure(mdCtx, method, errTest("bad signature"))

	if len(m.queue) != 1 {
		t.Fatalf("queue len = %d, want 1", len(m.queue))
	}
	second := <-m.queue
	if second.SuppressedCount != 3 {
		t.Fatalf("suppressed_count = %d, want 3", second.SuppressedCount)
	}
}

// TestAuthFailureNoManager: a nil manager (audit disabled) is a no-op.
func TestAuthFailureNoManager(t *testing.T) {
	oldMgr := Mgr
	Mgr = nil
	t.Cleanup(func() { Mgr = oldMgr })

	AuthFailure(context.Background(), "/m", errTest("x")) // must not panic
}

// TestInterceptorPassThroughDisabled: with no manager the interceptor just
// forwards to the handler.
func TestInterceptorPassThroughDisabled(t *testing.T) {

	oldMgr := Mgr
	Mgr = nil
	t.Cleanup(func() { Mgr = oldMgr })

	ic := Interceptor()

	called := false
	resp, err := ic(context.Background(), &inapi.ZoneInitRequest{Name: "z"},
		&grpc.UnaryServerInfo{FullMethod: "/inapi.ZoneService/ZoneInit"},
		func(ctx context.Context, req any) (any, error) {
			called = true
			return "ok", nil
		})
	if !called || resp != "ok" || err != nil {
		t.Fatalf("pass-through broken: %v %v", resp, err)
	}
}

// TestInterceptorEndToEnd drives the interceptor with a live manager and
// expects the record in the store.
func TestInterceptorEndToEnd(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 365)

	oldMgr := Mgr
	Mgr = m
	t.Cleanup(func() { Mgr = oldMgr })

	ic := Interceptor()

	_, err := ic(testCtx(t, inauth.NewUserAccessKey()),
		&inapi.ZoneInitRequest{Name: "z1"},
		&grpc.UnaryServerInfo{FullMethod: "/inapi.ZoneService/ZoneInit"},
		func(ctx context.Context, req any) (any, error) {
			return &inapi.ZoneInitResponse{}, nil
		})
	if err != nil {
		t.Fatal(err)
	}

	waitWritten(t, m, 1)

	recs := allRecords(t, db)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	if recs[0].Action != "/inapi.ZoneService/ZoneInit" ||
		recs[0].Status != inapi.AuditStatusOK ||
		recs[0].TargetId != "zone/z1" {
		t.Fatalf("unexpected record: %+v", recs[0])
	}
}

// BenchmarkAuditInterceptor measures the hot-path cost of the interceptor
// (record building + enqueue; the store write happens off-path). Target:
// single-digit microseconds, zero blocking.
func BenchmarkAuditInterceptor(b *testing.B) {

	config.Config.Zonelet.ZoneName = testZone

	ak := inauth.NewUserAccessKey()
	av, err := inauth.NewAppValidator(inauth.NewAppCredential(ak).AuthToken())
	if err != nil {
		b.Fatal(err)
	}
	ctx := inauth.NewAppContext(context.Background(), av)

	m := &Manager{
		queue:        make(chan *inapi.AuditRecord, queueCap),
		authFailures: authFailureCache{items: map[string]*authFailureEntry{}},
	}
	oldMgr := Mgr
	Mgr = m
	b.Cleanup(func() { Mgr = oldMgr })

	ic := Interceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/inapi.ZoneService/AppInstanceDeploy"}
	req := &inapi.AppInstanceDeployRequest{
		Name: "web",
		Spec: &inapi.AppSpec{
			Name: "web", Version: "1.0.0",
			Packages: []*inapi.AppSpecPackage{{Name: "nginx", Version: "1.0"}},
		},
	}

	// Drain in the background so the queue never fills.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range m.queue {
		}
	}()
	b.Cleanup(func() { close(m.queue); <-done })

	b.ResetTimer()
	for b.Loop() {
		ic(ctx, req, info, func(ctx context.Context, req any) (any, error) {
			return &inapi.AppInstanceDeployResponse{}, nil
		})
	}
}

// BenchmarkAuditRecordEncode measures the chain-hash canonical encoding of
// one record.
func BenchmarkAuditRecordEncode(b *testing.B) {

	rec := &inapi.AuditRecord{
		Zone:       testZone,
		Id:         "01870f2c4a1b3c4d",
		Ts:         1790000000000,
		ActorId:    "69cb4cfe4b64",
		ActorUser:  "alice",
		ActorType:  inapi.AuditActorUser,
		Action:     "/inapi.ZoneService/AppInstanceDeploy",
		SourceIp:   "10.1.2.3",
		TargetType: "app-instance",
		TargetId:   "app-instance/web",
		Status:     inapi.AuditStatusOK,
		Detail:     `{"name":"web","packages":"nginx:1.0","replica_cap":3}`,
		PrevHash:   "0000000000000000000000000000000000000000000000000000000000000000",
	}

	b.ResetTimer()
	for b.Loop() {
		if _, err := recordHash(rec); err != nil {
			b.Fatal(err)
		}
	}
}
