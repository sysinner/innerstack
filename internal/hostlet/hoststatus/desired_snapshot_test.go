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

package hoststatus

import "testing"

// TestDesiredSnapshot exercises the freshness/emptiness gating that the orphan
// sweep relies on: action must be suspended until a fresh, non-empty desired set
// arrives, and a failed or stale sync (or an empty set) must never authorize
// orphan decisions.
func TestDesiredSnapshot(t *testing.T) {
	var d desiredSnapshot

	// Before any sync the snapshot is empty and not fresh.
	if !d.Empty() {
		t.Fatalf("Empty() = false before any sync, want true")
	}
	if d.IsFresh(1000, 60) {
		t.Fatalf("IsFresh() = true before any sync, want false")
	}
	if d.Contains("i8k_myapp_1") {
		t.Fatalf("Contains() = true on empty snapshot")
	}

	// A failed sync keeps it unusable.
	d.SetFailed()
	if d.IsFresh(1000, 60) {
		t.Fatalf("IsFresh() = true after SetFailed, want false")
	}

	// A successful sync at t=1000 populates the set and marks it fresh.
	d.Replace(map[string]struct{}{
		"i8k_myapp_1": {},
		"i8k_myapp_2": {},
	}, 1000)
	if d.Empty() {
		t.Fatalf("Empty() = true after Replace, want false")
	}
	if !d.Contains("i8k_myapp_1") {
		t.Fatalf("Contains(i8k_myapp_1) = false, want true")
	}
	if d.Contains("i8k_missing_3") {
		t.Fatalf("Contains(missing) = true, want false")
	}

	// Fresh within the stale window, stale just past it.
	if !d.IsFresh(1000+60, 60) {
		t.Fatalf("IsFresh at window boundary = false, want true")
	}
	if d.IsFresh(1000+61, 60) {
		t.Fatalf("IsFresh past window = true, want false")
	}

	// A later failed sync suspends freshness even within the window; the last
	// known set is retained (Contains still answers) but the gate stays closed.
	d.SetFailed()
	if d.IsFresh(1000+10, 60) {
		t.Fatalf("IsFresh() = true after post-sync SetFailed, want false")
	}
	if !d.Contains("i8k_myapp_1") {
		t.Fatalf("Contains() = false after SetFailed; last known set should be retained")
	}
}
