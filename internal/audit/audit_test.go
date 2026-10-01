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
	"testing"
	"time"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
	"github.com/lynkdb/kvgo/v2/pkg/kvrep"
	"github.com/lynkdb/kvgo/v2/pkg/storage"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

const testZone = "testzone"

func testDB(t *testing.T) kvapi.Client {
	t.Helper()
	db, err := kvrep.NewReplica(&storage.Options{DataDirectory: t.TempDir()})
	if err != nil {
		t.Fatalf("open test kvgo db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// retentionMsOf converts a retention in days to a TTL in milliseconds.
func retentionMsOf(days int) int64 {
	return int64(days) * 86400000
}

// newTestManager builds a running Manager on the given db (fresh chain
// recovery included) and closes it on cleanup.
func newTestManager(t *testing.T, db kvapi.Client, retentionDays int) *Manager {
	t.Helper()

	m := &Manager{
		db:           db,
		retentionMs:  retentionMsOf(retentionDays),
		queue:        make(chan *inapi.AuditRecord, queueCap),
		done:         make(chan struct{}),
		authFailures: authFailureCache{items: map[string]*authFailureEntry{}},
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())

	if err := m.recoverChainHead(); err != nil {
		t.Fatalf("chain head recovery: %v", err)
	}

	go m.run()

	t.Cleanup(m.close)

	return m
}

func waitWritten(t *testing.T, m *Manager, n int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for m.writtenTotal.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %d written records, got %d", n, m.writtenTotal.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func emit(t *testing.T, m *Manager, action, status string) {
	t.Helper()
	m.emit(&inapi.AuditRecord{
		Zone:   testZone,
		Action: action,
		Status: status,
	})
}

func allRecords(t *testing.T, db kvapi.Client) []*inapi.AuditRecord {
	t.Helper()
	items, _, err := List(db, ListOptions{Zone: testZone, Revert: true, Limit: 1000})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return items
}

// TestManagerWriteOrderAndChain verifies the serialized write path: ids
// follow emission order, the hash chain links every consecutive pair, and
// newest-first listing returns the reverse order.
func TestManagerWriteOrderAndChain(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 365)

	for range 5 {
		emit(t, m, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m, 5)

	asc := allRecords(t, db)
	if len(asc) != 5 {
		t.Fatalf("got %d records, want 5", len(asc))
	}

	for i, rec := range asc {
		if i == 0 {
			if rec.PrevHash != "" {
				t.Fatalf("first record prev_hash = %q, want genesis", rec.PrevHash)
			}
			continue
		}
		if rec.PrevHash != asc[i-1].Hash {
			t.Fatalf("record %d prev_hash %s != previous hash %s",
				i, rec.PrevHash, asc[i-1].Hash)
		}
		if rec.Ts <= asc[i-1].Ts {
			t.Fatalf("ts not monotonic: %d then %d", asc[i-1].Ts, rec.Ts)
		}
	}

	// Newest first by default.
	desc, _, err := List(db, ListOptions{Zone: testZone, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(desc) != 5 || desc[0].Id != asc[4].Id || desc[4].Id != asc[0].Id {
		t.Fatalf("newest-first order wrong")
	}
}

// TestListCursorPagination pages through records with the exclusive id
// cursor in both directions and verifies no overlap and no gaps.
func TestListCursorPagination(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 365)

	for range 25 {
		emit(t, m, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m, 25)

	seen := map[string]bool{}

	// Descending pages.
	offset := ""
	pages := 0
	for {
		items, more, err := List(db, ListOptions{Zone: testZone, Limit: 10, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 {
			break
		}
		for _, r := range items {
			if seen[r.Id] {
				t.Fatalf("record %s returned twice", r.Id)
			}
			seen[r.Id] = true
		}
		pages++
		if !more {
			break
		}
		offset = items[len(items)-1].Id
	}
	if len(seen) != 25 {
		t.Fatalf("descending pagination covered %d records, want 25", len(seen))
	}
	if pages != 3 {
		t.Fatalf("descending pages = %d, want 3", pages)
	}

	// Ascending pages.
	seen = map[string]bool{}
	offset = ""
	for {
		items, more, err := List(db, ListOptions{
			Zone: testZone, Limit: 7, Revert: true, Offset: offset,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 {
			break
		}
		for _, r := range items {
			if seen[r.Id] {
				t.Fatalf("record %s returned twice (asc)", r.Id)
			}
			seen[r.Id] = true
		}
		if !more {
			break
		}
		offset = items[len(items)-1].Id
	}
	if len(seen) != 25 {
		t.Fatalf("ascending pagination covered %d records, want 25", len(seen))
	}
}

// TestListFiltersAndWindow covers the in-memory filters and the time-window
// key bounds.
func TestListFiltersAndWindow(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 365)

	emit(t, m, "ZoneService/AppInstanceDeploy", inapi.AuditStatusOK)
	emit(t, m, "ZoneService/AppInstanceDelete", inapi.AuditStatusError)
	emit(t, m, "ZoneService/AppInstanceDeploy", inapi.AuditStatusDenied)
	waitWritten(t, m, 3)

	recs := allRecords(t, db)
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}

	fixtures := []struct {
		name string
		opt  ListOptions
		want []string // ids, newest first
	}{
		{
			name: "status_denied",
			opt:  ListOptions{Zone: testZone, Status: inapi.AuditStatusDenied},
			want: []string{recs[2].Id},
		},
		{
			name: "action_deploy",
			opt:  ListOptions{Zone: testZone, Action: "ZoneService/AppInstanceDeploy"},
			want: []string{recs[2].Id, recs[0].Id},
		},
		{
			name: "actor",
			opt:  ListOptions{Zone: testZone, ActorId: "no-such-actor"},
			want: nil,
		},
		{
			name: "window_covers_all",
			opt: ListOptions{
				Zone:    testZone,
				TsStart: recs[0].Ts - 1,
				TsEnd:   recs[2].Ts,
			},
			want: []string{recs[2].Id, recs[1].Id, recs[0].Id},
		},
		{
			name: "window_excludes_newest",
			opt: ListOptions{
				Zone:    testZone,
				TsStart: recs[0].Ts,
				TsEnd:   recs[1].Ts,
			},
			want: []string{recs[1].Id, recs[0].Id},
		},
		{
			name: "window_empty",
			opt: ListOptions{
				Zone:    testZone,
				TsStart: recs[2].Ts + 1,
			},
			want: nil,
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			items, _, err := List(db, f.opt)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) != len(f.want) {
				t.Fatalf("got %d items (%v), want %d", len(items), items, len(f.want))
			}
			for i, w := range f.want {
				if items[i].Id != w {
					t.Fatalf("item %d = %s, want %s", i, items[i].Id, w)
				}
			}
		})
	}
}

// TestQueueOverflowBackpressure verifies the non-blocking enqueue: overflow
// drops are counted and the next written record carries the gap marker.
func TestQueueOverflowBackpressure(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)

	// Manager without the writer goroutine: nothing drains the queue.
	m := &Manager{
		db:           db,
		retentionMs:  retentionMsOf(365),
		queue:        make(chan *inapi.AuditRecord, 4),
		authFailures: authFailureCache{items: map[string]*authFailureEntry{}},
	}

	for range 9 {
		m.emit(&inapi.AuditRecord{Zone: testZone, Action: "test", Status: "ok"})
	}

	written, dropped := m.Stats()
	if dropped != 5 {
		t.Fatalf("dropped = %d, want 5", dropped)
	}
	if written != 0 {
		t.Fatalf("written = %d, want 0", written)
	}

	// The next accepted record marks the gap.
	rec := &inapi.AuditRecord{Zone: testZone, Action: "test", Status: "ok"}
	m.write(rec)
	if rec.DroppedBefore != 5 {
		t.Fatalf("DroppedBefore = %d, want 5", rec.DroppedBefore)
	}

	// And the gap marker resets for the following record.
	rec2 := &inapi.AuditRecord{Zone: testZone, Action: "test", Status: "ok"}
	m.write(rec2)
	if rec2.DroppedBefore != 0 {
		t.Fatalf("DroppedBefore = %d, want 0", rec2.DroppedBefore)
	}
}

// TestChainVerifyValid runs a full verification over a healthy chain.
func TestChainVerifyValid(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 365)

	for range 4 {
		emit(t, m, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m, 4)

	rs := Verify(db, VerifyOptions{Zone: testZone, RetentionMs: 365 * 86400000})
	if !rs.Valid {
		t.Fatalf("verify not valid: broken at %s", rs.BrokenAt)
	}
	if rs.Count != 4 || rs.FirstId == "" || rs.LastId == "" || rs.HeadHash == "" {
		t.Fatalf("verify result incomplete: %+v", rs)
	}
}

// TestChainTamperDetection modifies one stored record and expects verify to
// pinpoint it.
func TestChainTamperDetection(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 365)

	for range 5 {
		emit(t, m, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m, 5)

	recs := allRecords(t, db)
	victim := recs[2]

	// Rewrite the stored record with a mutated field.
	victim.Status = inapi.AuditStatusDenied
	if rs := db.NewWriter(
		inapi.NsZoneletAuditLog(testZone, victim.Id), victim).Exec(); !rs.OK() {
		t.Fatalf("tamper write: %s", rs.ErrorMessage())
	}

	rs := Verify(db, VerifyOptions{Zone: testZone, RetentionMs: 365 * 86400000})
	if rs.Valid {
		t.Fatal("verify should fail after tampering")
	}
	if rs.BrokenAt != victim.Id {
		t.Fatalf("broken_at = %s, want %s", rs.BrokenAt, victim.Id)
	}
}

// TestChainDeleteMiddleRecord detects a removed record in the middle of the
// live window (link mismatch at the successor).
func TestChainDeleteMiddleRecord(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 365)

	for range 5 {
		emit(t, m, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m, 5)

	recs := allRecords(t, db)
	victim := recs[1]

	if rs := db.NewDeleter(
		inapi.NsZoneletAuditLog(testZone, victim.Id)).Exec(); !rs.OK() {
		t.Fatalf("delete: %s", rs.ErrorMessage())
	}

	rs := Verify(db, VerifyOptions{Zone: testZone, RetentionMs: 365 * 86400000})
	if rs.Valid {
		t.Fatal("verify should fail after mid-chain deletion")
	}
	if rs.BrokenAt != recs[2].Id {
		t.Fatalf("broken_at = %s, want %s (successor)", rs.BrokenAt, recs[2].Id)
	}
}

// TestChainRestartRecovery closes the writer and continues the same chain
// on the same store.
func TestChainRestartRecovery(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)

	m1 := newTestManager(t, db, 365)
	for range 3 {
		emit(t, m1, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m1, 3)
	head := m1.ChainHead()
	m1.close()

	m2 := newTestManager(t, db, 365)
	if m2.ChainHead() != head {
		t.Fatalf("chain head after restart = %s, want %s", m2.ChainHead(), head)
	}
	for range 3 {
		emit(t, m2, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m2, 3)

	rs := Verify(db, VerifyOptions{Zone: testZone, RetentionMs: 365 * 86400000})
	if !rs.Valid {
		t.Fatalf("verify after restart not valid: broken at %s", rs.BrokenAt)
	}
	if rs.Count != 6 {
		t.Fatalf("count = %d, want 6", rs.Count)
	}
}

// TestChainTTLExpiredTail simulates retention expiry eating the head of the
// chain (records older than the retention window are gone): verify accepts
// a tail explained by TTL, and rejects the same tail when the records were
// younger than the retention window.
func TestChainTTLExpiredTail(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 365)

	for range 4 {
		emit(t, m, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m, 4)

	recs := allRecords(t, db)

	// Remove the two oldest records (simulated TTL expiry).
	for _, r := range recs[:2] {
		if rs := db.NewDeleter(
			inapi.NsZoneletAuditLog(testZone, r.Id)).Exec(); !rs.OK() {
			t.Fatalf("delete: %s", rs.ErrorMessage())
		}
	}

	// Long retention: the missing head is younger than the window, so the
	// tail cannot be explained by expiry.
	rs := Verify(db, VerifyOptions{Zone: testZone, RetentionMs: 365 * 86400000})
	if rs.Valid {
		t.Fatal("verify should reject an unexplained tail within retention")
	}
	if rs.BrokenAt != recs[2].Id {
		t.Fatalf("broken_at = %s, want %s", rs.BrokenAt, recs[2].Id)
	}

	// Retention that covers the removed head: the tail is explained.
	rs = Verify(db, VerifyOptions{Zone: testZone, RetentionMs: 1})
	if !rs.Valid {
		t.Fatalf("verify should accept a TTL-expired tail: broken at %s", rs.BrokenAt)
	}
	if rs.Count != 2 {
		t.Fatalf("count = %d, want 2", rs.Count)
	}
}

// TestChainAnchorHit pins a day's head with an anchor, removes everything
// before it, and expects verify to accept the tail via the anchor even with
// infinite retention.
func TestChainAnchorHit(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 0) // keep forever

	for range 3 {
		emit(t, m, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m, 3)

	recs := allRecords(t, db)
	headAtAnchor := recs[2].Hash

	// Daily anchor for the "first day".
	anchor := &inapi.AuditAnchor{
		Date:     "20260101",
		HeadHash: headAtAnchor,
		Count:    3,
	}
	if rs := db.NewWriter(
		inapi.NsZoneletAuditAnchor(testZone, "20260101"), anchor).Exec(); !rs.OK() {
		t.Fatalf("anchor write: %s", rs.ErrorMessage())
	}

	// Everything before the anchor "expired".
	for _, r := range recs {
		if rs := db.NewDeleter(
			inapi.NsZoneletAuditLog(testZone, r.Id)).Exec(); !rs.OK() {
			t.Fatalf("delete: %s", rs.ErrorMessage())
		}
	}

	// The chain continues from the anchored head.
	for range 2 {
		emit(t, m, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m, 5)

	rs := Verify(db, VerifyOptions{Zone: testZone, RetentionMs: 0})
	if !rs.Valid {
		t.Fatalf("anchor-explained tail should verify: broken at %s", rs.BrokenAt)
	}
	if rs.Count != 2 {
		t.Fatalf("count = %d, want 2", rs.Count)
	}
}

// TestVerifyEmptyStore covers the genesis case.
func TestVerifyEmptyStore(t *testing.T) {
	db := testDB(t)
	rs := Verify(db, VerifyOptions{Zone: testZone})
	if !rs.Valid || rs.Count != 0 || rs.HeadHash != "" {
		t.Fatalf("empty store verify = %+v, want valid/0/empty", rs)
	}
}

// TestManagerCloseDrains verifies Close flushes the queue instead of
// dropping pending records.
func TestManagerCloseDrains(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)

	m := &Manager{
		db:           db,
		retentionMs:  retentionMsOf(365),
		queue:        make(chan *inapi.AuditRecord, queueCap),
		done:         make(chan struct{}),
		authFailures: authFailureCache{items: map[string]*authFailureEntry{}},
	}
	m.ctx, m.cancel = context.WithCancel(context.Background())

	// Writer not started yet: records accumulate in the queue.
	for range 5 {
		m.emit(&inapi.AuditRecord{Zone: testZone, Action: "test", Status: "ok"})
	}

	go m.run()
	m.close()

	if m.writtenTotal.Load() != 5 {
		t.Fatalf("written = %d, want 5 (queue should drain on close)", m.writtenTotal.Load())
	}
}

// TestTsClamp verifies the monotonic timestamp clamp of the writer.
func TestTsClamp(t *testing.T) {

	db := testDB(t)

	m := &Manager{
		db:           db,
		retentionMs:  retentionMsOf(365),
		queue:        make(chan *inapi.AuditRecord, queueCap),
		authFailures: authFailureCache{items: map[string]*authFailureEntry{}},
	}

	a := &inapi.AuditRecord{Zone: testZone, Action: "t", Status: "ok"}
	b := &inapi.AuditRecord{Zone: testZone, Action: "t", Status: "ok"}

	m.write(a)
	// Force the logical clock far ahead of wall time.
	m.mu.Lock()
	m.lastTs = a.Ts + 5000
	m.mu.Unlock()
	m.write(b)

	if b.Ts <= a.Ts+5000 {
		t.Fatalf("ts not clamped monotonic: a=%d forced=%d b=%d", a.Ts, a.Ts+5000, b.Ts)
	}
}

// TestAnchorRefresh verifies the daily anchor task: it pins the current
// chain head under the anchor namespace (no TTL).
func TestAnchorRefresh(t *testing.T) {

	config.Config.Zonelet.ZoneName = testZone

	db := testDB(t)
	m := newTestManager(t, db, 365)

	for range 3 {
		emit(t, m, "test/action", inapi.AuditStatusOK)
	}
	waitWritten(t, m, 3)

	// The cleanup-closed manager already anchored once (first call on a
	// fresh manager); force another run by resetting the date gate.
	m.mu.Lock()
	m.anchorDate = ""
	m.mu.Unlock()
	m.anchorRefresh()

	prefix := inapi.NsZoneletAuditAnchorPrefix(testZone)
	rs := db.NewRanger(prefix, append(prefix, 0xff)).SetLimit(10).Exec()
	if !rs.OK() || len(rs.Items) == 0 {
		t.Fatalf("no anchor written: %v", rs.ErrorMessage())
	}

	var anchor inapi.AuditAnchor
	if err := rs.Items[0].JsonDecode(&anchor); err != nil {
		t.Fatal(err)
	}
	if anchor.HeadHash != m.ChainHead() {
		t.Fatalf("anchor head %s != chain head %s", anchor.HeadHash, m.ChainHead())
	}
	if anchor.Count != 3 {
		t.Fatalf("anchor count = %d, want 3", anchor.Count)
	}
}
