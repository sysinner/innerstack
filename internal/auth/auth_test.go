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

package auth

import (
	"testing"

	"github.com/lynkdb/kvgo/v2/pkg/kvrep"
	"github.com/lynkdb/kvgo/v2/pkg/storage"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

// TestRefreshAccessKeysFromDbSkipsRevoked: keys revoked via user disable
// (state disabled) stay in the DB for listing but must never re-enter the
// in-memory key manager on a reload.
func TestRefreshAccessKeysFromDbSkipsRevoked(t *testing.T) {

	db, err := kvrep.NewReplica(&storage.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer db.Close()

	prevDB := data.Zonelet
	data.Zonelet = db
	t.Cleanup(func() { data.Zonelet = prevDB })

	prevZone := config.Config.Zonelet.ZoneName
	config.Config.Zonelet.ZoneName = "authtest"
	t.Cleanup(func() { config.Config.Zonelet.ZoneName = prevZone })

	fixtures := []*inauth.AccessKey{
		{Id: "aaa000000001", Secret: "activekeysecret000000000000000000000"},
		{Id: "bbb000000002", Secret: "revokedkeysecret00000000000000000000",
			State: inauth.AccessKey_State_Disable},
	}
	for _, key := range fixtures {
		if rs := data.Zonelet.NewWriter(inapi.NsZoneletAccessKey(
			config.Config.Zonelet.ZoneName, key.Id), key).Exec(); !rs.OK() {
			t.Fatalf("seed key %s: %s", key.Id, rs.ErrorMessage())
		}
	}

	am := &AuthManager{keyMgr: inauth.NewAccessKeyManager()}
	if err := am.RefreshAccessKeysFromDB(); err != nil {
		t.Fatal(err)
	}

	if am.keyMgr.Key("aaa000000001") == nil {
		t.Fatal("active key not loaded")
	}
	if am.keyMgr.Key("bbb000000002") != nil {
		t.Fatal("revoked key loaded into key manager")
	}
}

// TestAccessKeysOfFilteredSkew: keys of other users must not end a
// filtered scan early (wrong has_more / truncated pages).
func TestAccessKeysOfFilteredSkew(t *testing.T) {

	db, err := kvrep.NewReplica(&storage.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	defer db.Close()

	prevDB := data.Zonelet
	data.Zonelet = db
	t.Cleanup(func() { data.Zonelet = prevDB })

	prevZone := config.Config.Zonelet.ZoneName
	config.Config.Zonelet.ZoneName = "authtest"
	t.Cleanup(func() { config.Config.Zonelet.ZoneName = prevZone })

	// Interleave owners in key-id order: every scan window is dominated
	// by keys of the other user.
	fixtures := []struct {
		id   string
		user string
	}{
		{"aaa000000001", "y"},
		{"bbb000000002", "x"},
		{"ccc000000003", "y"},
		{"ddd000000004", "x"},
	}
	for _, f := range fixtures {
		key := &inauth.AccessKey{
			Id:     f.id,
			Secret: "0123456789abcdef0123456789abcdef",
			User:   f.user,
		}
		if rs := data.Zonelet.NewWriter(inapi.NsZoneletAccessKey(
			config.Config.Zonelet.ZoneName, key.Id), key).Exec(); !rs.OK() {
			t.Fatalf("seed key %s: %s", key.Id, rs.ErrorMessage())
		}
	}

	keys, hasMore, err := AuthMgr.AccessKeysOf("x", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Id != "bbb000000002" || !hasMore {
		t.Fatalf("first page keys=%+v has_more=%v", keys, hasMore)
	}

	keys, hasMore, err = AuthMgr.AccessKeysOf("x", "bbb000000002", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Id != "ddd000000004" || hasMore {
		t.Fatalf("second page keys=%+v has_more=%v", keys, hasMore)
	}

	// The unfiltered listing still covers all keys.
	keys, hasMore, err = AuthMgr.AccessKeysOf("", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 4 || hasMore {
		t.Fatalf("unfiltered keys=%d has_more=%v", len(keys), hasMore)
	}
}
