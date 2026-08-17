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
	"sync"
	"time"
)

// replaySweepInterval is the number of inserts between two lazy sweeps of
// expired nonces. Sweeping on insert keeps the cache bounded without a
// background goroutine.
const replaySweepInterval = 1024

// replayCache remembers consumed token nonces so that a token can be used
// only once. Access tokens are minted per RPC with a unique Claims.Nonce
// nonce; once a token passes verification its nonce is recorded for longer
// than the token can possibly be accepted, so any replay is denied.
//
// The cache is node-local (leader-verified RPC topology) and lazily cleaned:
// expired entries are ignored on read and swept on insert.
type replayCache struct {
	mu      sync.Mutex
	items   map[string]int64 // kid + "\x00" + nonce -> expireAt (unix seconds)
	inserts int64
}

func newReplayCache() *replayCache {
	return &replayCache{
		items: map[string]int64{},
	}
}

// consume records the nonce of a verified token and reports whether it is
// used for the first time. An empty nonce (legacy token) is always accepted
// and not tracked. A nonce is accepted again once its retention window has
// passed, at which point the underlying token is long expired.
func (it *replayCache) consume(kid, nonce string) bool {
	if nonce == "" {
		return true
	}

	now := time.Now().Unix()
	k := kid + "\x00" + nonce

	it.mu.Lock()
	defer it.mu.Unlock()

	if exp, ok := it.items[k]; ok && exp > now {
		return false
	}
	it.items[k] = now + tokenReplayRetention

	it.inserts++
	if it.inserts%replaySweepInterval == 0 {
		it.sweepLocked(now)
	}

	return true
}

func (it *replayCache) sweepLocked(now int64) {
	for k, exp := range it.items {
		if exp <= now {
			delete(it.items, k)
		}
	}
}
