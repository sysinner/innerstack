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

package inauth

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestKeyMgr(t *testing.T) (*AccessKeyManager, *AccessKey) {
	t.Helper()

	ak := NewAccessKey()
	km := NewAccessKeyManager()
	if err := km.Set(ak); err != nil {
		t.Fatalf("keyMgr.Set: %v", err)
	}
	return km, ak
}

func verifyToken(t *testing.T, km *AccessKeyManager, token string) error {
	t.Helper()

	at, err := ParseAccessToken(token)
	if err != nil {
		t.Fatalf("ParseAccessToken: %v", err)
	}
	_, err = at.Verify(km)
	return err
}

func TestVerifySingleUse(t *testing.T) {
	cases := []struct {
		name string
		typ  string
	}{
		{name: "default key type", typ: ""},
		{name: "app key type", typ: "App"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			km, ak := newTestKeyMgr(t)
			ak.Type = c.typ

			ac := NewAppCredential(ak)
			token := ac.AuthToken()

			if err := verifyToken(t, km, token); err != nil {
				t.Fatalf("first verify: %v", err)
			}
			err := verifyToken(t, km, token)
			if err == nil || !strings.Contains(err.Error(), "replayed") {
				t.Fatalf("second verify error = %v, want replay denial", err)
			}

			// A freshly minted token carries a fresh nonce and verifies
			// again, even within the same second.
			token2 := ac.AuthToken()
			if token2 == token {
				t.Fatalf("consecutive mints produced identical tokens")
			}
			if err := verifyToken(t, km, token2); err != nil {
				t.Fatalf("verify of freshly minted token: %v", err)
			}
		})
	}
}

// Legacy tokens without a nonce are exempt from single-use enforcement so
// old clients keep working across a rolling upgrade.
func TestVerifyLegacyTokenWithoutNonce(t *testing.T) {
	km, _ := newTestKeyMgr(t)

	token, err := NewAccessToken().SignToken(km)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	for i := range 2 {
		if err := verifyToken(t, km, token); err != nil {
			t.Fatalf("verify pass %d: %v", i+1, err)
		}
	}
}

func TestVerifyIatFreshness(t *testing.T) {
	km, _ := newTestKeyMgr(t)

	tn := time.Now().Unix()
	at := NewAccessToken()
	at.Claims.Iat = tn - 120
	at.Claims.Exp = tn + 60
	at.Claims.State = ""

	token, err := at.SignToken(km)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}

	if err := verifyToken(t, km, token); err == nil ||
		!strings.Contains(err.Error(), "iat expired") {
		t.Fatalf("verify error = %v, want iat expiry denial", err)
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	km, ak := newTestKeyMgr(t)

	token := NewAppCredential(ak).AuthToken()
	last := token[len(token)-1]
	if last == 'A' {
		token = token[:len(token)-1] + "B"
	} else {
		token = token[:len(token)-1] + "A"
	}

	if err := verifyToken(t, km, token); err == nil ||
		!strings.Contains(err.Error(), "verify denied") {
		t.Fatalf("verify error = %v, want signature denial", err)
	}
}

func TestReplayCacheExpiry(t *testing.T) {
	rc := newReplayCache()

	if !rc.consume("kid", "n1") {
		t.Fatal("first consume of a nonce should be accepted")
	}
	if rc.consume("kid", "n1") {
		t.Fatal("second consume of a live nonce should be denied")
	}

	rc.mu.Lock()
	rc.items["kid\x00n1"] = time.Now().Unix() - 1
	rc.mu.Unlock()

	if !rc.consume("kid", "n1") {
		t.Fatal("consume of an expired nonce should be accepted")
	}
}

func TestReplayCacheSweep(t *testing.T) {
	rc := newReplayCache()
	rc.consume("kid", "n1")

	rc.mu.Lock()
	rc.items["kid\x00stale"] = time.Now().Unix() - 1
	rc.inserts = replaySweepInterval - 1
	rc.mu.Unlock()

	rc.consume("kid", "n2") // triggers the lazy sweep

	rc.mu.Lock()
	defer rc.mu.Unlock()
	if _, ok := rc.items["kid\x00stale"]; ok {
		t.Fatal("expired nonce should have been swept")
	}
	if _, ok := rc.items["kid\x00n1"]; !ok {
		t.Fatal("live nonce should have been kept")
	}
}

func TestReplayCacheConcurrent(t *testing.T) {
	rc := newReplayCache()

	var (
		mu   sync.Mutex
		wins int
		wg   sync.WaitGroup
	)

	for range 16 {
		wg.Go(func() {
			if rc.consume("kid", "shared") {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("exactly one concurrent consume should win, got %d", wins)
	}
}
