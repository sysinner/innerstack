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
	"bytes"
	"testing"
	"time"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sysinner/innerstack/v2/internal/auth"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

// tokenValidates reports whether a freshly minted token for the key
// authenticates against the live auth key manager.
func tokenValidates(ak *inauth.AccessKey) bool {
	av, err := inauth.NewAppValidator(inauth.NewAppCredential(ak).AuthToken())
	if err != nil {
		return false
	}
	return av.Verify(auth.AuthMgr.KeyMgr()) == nil
}

// TestUserLifecycle covers create, update, disable/enable cascade and the
// delete guards.
func TestUserLifecycle(t *testing.T) {

	ctx := setupAuditTest(t) // shares the audit fixture: temp db, zone, leader state, admin ctx
	srv := &zoneServer{}

	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{
		Name:        "tim",
		Description: "first user",
	}); err != nil {
		t.Fatal(err)
	}

	// Re-set without state keeps the user; description update applies.
	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{
		Name:        "tim",
		Description: "updated",
	}); err != nil {
		t.Fatal(err)
	}

	rsp, err := srv.UserList(ctx, &inapi.UserListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Items) != 1 || rsp.Items[0].Name != "tim" ||
		rsp.Items[0].Description != "updated" ||
		rsp.Items[0].State != inapi.UserStateActive {
		t.Fatalf("user list = %+v", rsp.Items)
	}

	// Validation: bad name and bad state.
	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{Name: "x"}); err == nil {
		t.Fatal("expected invalid name error")
	}
	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{
		Name: "tim", State: "paused",
	}); err == nil {
		t.Fatal("expected invalid state error")
	}

	// Delete is refused while keys exist.
	if _, err := srv.AccessKeySet(ctx, &inapi.AccessKeySetRequest{
		User:   "tim",
		Scopes: []string{inapi.AuthScope_App_Read},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.UserDelete(ctx, &inapi.UserDeleteRequest{Name: "tim"}); err == nil {
		t.Fatal("expected delete refusal while keys exist")
	}

	// Disable cascades: keys revoked from the auth manager.
	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{
		Name: "tim", State: inapi.UserStateDisabled,
	}); err != nil {
		t.Fatal(err)
	}
	keys, _, err := auth.AuthMgr.AccessKeysOf("tim", "", inapi.AccessKeyListLimitMax)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].State != inauth.AccessKey_State_Disable {
		t.Fatalf("keys after disable = %+v", keys)
	}
	if tokenValidates(keys[0]) {
		t.Fatal("disabled user key still authenticates")
	}

	// Disabled users take no new keys.
	if _, err := srv.AccessKeySet(ctx, &inapi.AccessKeySetRequest{
		User:   "tim",
		Scopes: []string{inapi.AuthScope_App_Read},
	}); err == nil {
		t.Fatal("expected key creation rejection for disabled user")
	}

	// Re-enable restores the keys.
	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{
		Name: "tim", State: inapi.UserStateActive,
	}); err != nil {
		t.Fatal(err)
	}
	if keys, _, err = auth.AuthMgr.AccessKeysOf(
		"tim",
		"",
		inapi.AccessKeyListLimitMax,
	); err != nil {
		t.Fatal(err)
	} else if len(keys) != 1 ||
		keys[0].State != inauth.AccessKey_State_Active {
		t.Fatalf("keys after enable = %+v", keys)
	}
	if !tokenValidates(keys[0]) {
		t.Fatal("re-enabled user key does not authenticate")
	}

	// Revoke the key, then the user is deletable.
	if _, err := srv.AccessKeyDelete(ctx, &inapi.AccessKeyDeleteRequest{
		KeyId: keys[0].Id,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.UserDelete(ctx, &inapi.UserDeleteRequest{Name: "tim"}); err != nil {
		t.Fatal(err)
	}
	if got, err := userGet("tim"); err != nil || got != nil {
		t.Fatalf("user still present after delete: err=%v item=%+v", err, got)
	}
}

// TestAccessKeySetFlow covers the creation guards and the one-time
// credential response.
func TestAccessKeySetFlow(t *testing.T) {

	ctx := setupAuditTest(t) // shares the audit fixture: temp db, zone, leader state, admin ctx
	srv := &zoneServer{}

	// Unknown user.
	if _, err := srv.AccessKeySet(ctx, &inapi.AccessKeySetRequest{
		User:   "nobody",
		Scopes: []string{inapi.AuthScope_App_Read},
	}); err == nil {
		t.Fatal("expected unknown user error")
	}

	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{Name: "tony"}); err != nil {
		t.Fatal(err)
	}

	// Missing scopes.
	if _, err := srv.AccessKeySet(ctx, &inapi.AccessKeySetRequest{
		User: "tony",
	}); err == nil {
		t.Fatal("expected missing scopes error")
	}

	rsp, err := srv.AccessKeySet(ctx, &inapi.AccessKeySetRequest{
		User:        "tony",
		Scopes:      []string{inapi.AuthScope_App_Read, inapi.AuthScope_Package_Read},
		Description: "laptop",
	})
	if err != nil {
		t.Fatal(err)
	}

	ak, err := inauth.ParseAccessKey(rsp.AccessKey)
	if err != nil {
		t.Fatalf("response credential unparsable: %v", err)
	}
	if ak.Id != rsp.KeyId {
		t.Fatalf("credential id %q != key_id %q", ak.Id, rsp.KeyId)
	}
	if !tokenValidates(ak) {
		t.Fatal("created key does not authenticate")
	}

	// The list view never carries the secret.
	lrsp, err := srv.AccessKeyList(ctx, &inapi.AccessKeyListRequest{
		User: "tony",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lrsp.Items) != 1 {
		t.Fatalf("key list = %+v", lrsp.Items)
	}
	item := lrsp.Items[0]
	if item.KeyId != rsp.KeyId || item.User != "tony" || len(item.Scopes) != 2 {
		t.Fatalf("key item = %+v", item)
	}
	if item.Description != "laptop" || item.State != "" {
		t.Fatalf("key item metadata = %+v", item)
	}
}

// TestAccessKeyDeleteIdempotent: revoking an unknown key succeeds.
func TestAccessKeyDeleteIdempotent(t *testing.T) {

	ctx := setupAuditTest(t) // shares the audit fixture: temp db, zone, leader state, admin ctx
	srv := &zoneServer{}

	if _, err := srv.AccessKeyDelete(ctx, &inapi.AccessKeyDeleteRequest{
		KeyId: "000000000000",
	}); err != nil {
		t.Fatalf("unknown key delete must be a no-op success: %v", err)
	}
}

// TestUserListStateFilter covers the state filter and cursor pagination.
func TestUserListStateFilter(t *testing.T) {

	ctx := setupAuditTest(t) // shares the audit fixture: temp db, zone, leader state, admin ctx
	srv := &zoneServer{}

	for _, name := range []string{"uca", "ucb", "ucc"} {
		if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{
		Name: "ucc", State: inapi.UserStateDisabled,
	}); err != nil {
		t.Fatal(err)
	}

	rsp, err := srv.UserList(ctx, &inapi.UserListRequest{
		State: inapi.UserStateActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Items) != 2 {
		t.Fatalf("active users = %+v", rsp.Items)
	}

	// Page through all users with a 1-item cursor.
	seen := map[string]bool{}
	offset := ""
	for {
		page, err := srv.UserList(ctx, &inapi.UserListRequest{
			Limit:  1,
			Offset: offset,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) == 0 {
			break
		}
		if seen[page.Items[0].Name] {
			t.Fatalf("user %s returned twice", page.Items[0].Name)
		}
		seen[page.Items[0].Name] = true
		if !page.HasMore {
			break
		}
		offset = page.Items[0].Name
	}
	if len(seen) != 3 {
		t.Fatalf("pagination covered %d users, want 3", len(seen))
	}
}

// TestUserListFilteredSkewPagination: users filtered out by the state
// filter must not end pagination early (empty page / missing has_more).
func TestUserListFilteredSkewPagination(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	// Key order uda < udb < udc: the disabled user sits between two active
	// ones, so the first 1-item window is dominated by filtered-out rows.
	for _, name := range []string{"uda", "udb", "udc"} {
		if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{
		Name: "udb", State: inapi.UserStateDisabled,
	}); err != nil {
		t.Fatal(err)
	}

	rsp, err := srv.UserList(ctx, &inapi.UserListRequest{
		State: inapi.UserStateActive,
		Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Items) != 1 || rsp.Items[0].Name != "uda" || !rsp.HasMore {
		t.Fatalf("first active page = %+v has_more=%v", rsp.Items, rsp.HasMore)
	}

	rsp, err = srv.UserList(ctx, &inapi.UserListRequest{
		State:  inapi.UserStateActive,
		Limit:  1,
		Offset: rsp.Items[0].Name,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rsp.Items) != 1 || rsp.Items[0].Name != "udc" || rsp.HasMore {
		t.Fatalf("second active page = %+v has_more=%v", rsp.Items, rsp.HasMore)
	}
}

// failKeyWritesDB fails writes to the access-key namespace, simulating a
// storage error part-way through the user-disable key cascade. Reads and
// ranges pass through untouched.
type failKeyWritesDB struct {
	kvapi.Client
}

func (d *failKeyWritesDB) NewWriter(key []byte, value interface{}) kvapi.ClientWriter {
	if bytes.HasPrefix(key, []byte(inapi.NsZoneletAccessKey(auditTestZone, ""))) {
		return failKeyWriter{}
	}
	return d.Client.NewWriter(key, value)
}

type failKeyWriter struct{}

func (failKeyWriter) SetJsonValue(v interface{}) kvapi.ClientWriter    { return failKeyWriter{} }
func (failKeyWriter) SetCreateOnly(b bool) kvapi.ClientWriter          { return failKeyWriter{} }
func (failKeyWriter) SetTTL(ttl int64) kvapi.ClientWriter              { return failKeyWriter{} }
func (failKeyWriter) SetAttrs(attrs uint64) kvapi.ClientWriter         { return failKeyWriter{} }
func (failKeyWriter) SetIncr(id uint64, ns string) kvapi.ClientWriter  { return failKeyWriter{} }
func (failKeyWriter) SetPrevVersion(v uint64) kvapi.ClientWriter       { return failKeyWriter{} }
func (failKeyWriter) SetPrevChecksum(v interface{}) kvapi.ClientWriter { return failKeyWriter{} }
func (failKeyWriter) Exec() *kvapi.ResultSet {
	return &kvapi.ResultSet{
		StatusCode:    kvapi.Status_ServerError,
		StatusMessage: "injected key-write failure",
	}
}

// TestUserSetDisablePartialFailure: a key-cascade failure must leave the
// user row at its old state, so a retry re-runs the cascade and converges
// instead of reporting success with keys still active.
func TestUserSetDisablePartialFailure(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{Name: "tim"}); err != nil {
		t.Fatal(err)
	}
	rsp, err := srv.AccessKeySet(ctx, &inapi.AccessKeySetRequest{
		User:   "tim",
		Scopes: []string{inapi.AuthScope_App_Read},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Disable with the key write failing: the RPC errors and the user row
	// keeps its old (active) state.
	prevDB := data.Zonelet
	data.Zonelet = &failKeyWritesDB{Client: prevDB}
	_, err = srv.UserSet(ctx, &inapi.UserSetRequest{
		Name: "tim", State: inapi.UserStateDisabled,
	})
	data.Zonelet = prevDB
	if err == nil {
		t.Fatal("expected cascade failure error")
	}
	user, err := userGet("tim")
	if err != nil {
		t.Fatal(err)
	}
	if user == nil || user.State != inapi.UserStateActive {
		t.Fatalf("user row mutated despite cascade failure: %+v", user)
	}

	// Storage recovers; the retry converges and the key stops validating.
	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{
		Name: "tim", State: inapi.UserStateDisabled,
	}); err != nil {
		t.Fatal(err)
	}
	ak, err := inauth.ParseAccessKey(rsp.AccessKey)
	if err != nil {
		t.Fatal(err)
	}
	if tokenValidates(ak) {
		t.Fatal("disabled user key still authenticates after retry")
	}
}

// TestAccessKeyDeleteConfigDeclaredRefused: a config-declared key (memory
// only, no DB row) must not be silently evicted by AccessKeyDelete.
func TestAccessKeyDeleteConfigDeclaredRefused(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	ak := inauth.NewUserAccessKey()
	ak.User = "sysadmin"
	if err := auth.AuthMgr.KeyMgr().Set(ak); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { auth.AuthMgr.KeyMgr().Del(ak.Id) })

	if _, err := srv.AccessKeyDelete(ctx, &inapi.AccessKeyDeleteRequest{
		KeyId: ak.Id,
	}); err == nil {
		t.Fatal("expected config-declared key delete refusal")
	}
	if auth.AuthMgr.KeyMgr().Key(ak.Id) == nil {
		t.Fatal("config-declared key evicted from key manager")
	}
	if !tokenValidates(ak) {
		t.Fatal("config-declared key no longer authenticates")
	}
}

// TestAccessKeySetSerializedAgainstCascade: key creation must block while
// the disable/enable cascade holds userKeysMu, so a fresh key cannot slip
// past the cascade's snapshot of the user's keys.
func TestAccessKeySetSerializedAgainstCascade(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	if _, err := srv.UserSet(ctx, &inapi.UserSetRequest{Name: "tony"}); err != nil {
		t.Fatal(err)
	}

	userKeysMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := srv.AccessKeySet(ctx, &inapi.AccessKeySetRequest{
			User:   "tony",
			Scopes: []string{inapi.AuthScope_App_Read},
		})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("AccessKeySet ignored the cascade lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	userKeysMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// TestUserGetNotFoundCode: a missing user reports gRPC NotFound so
// clients can distinguish create from update.
func TestUserGetNotFoundCode(t *testing.T) {

	ctx := setupAuditTest(t)
	srv := &zoneServer{}

	_, err := srv.UserGet(ctx, &inapi.UserGetRequest{Name: "nobody"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing user error code = %v, want NotFound", status.Code(err))
	}
}
