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

// Package audit records management-plane change events into an append-only
// kvgo namespace with a hash chain for tamper evidence.
//
// Parts: gRPC interceptor (collection), single-writer goroutine with a
// bounded queue (storage), list/verify queries (store.go).
package audit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

const (
	// queueCap bounds the enqueue buffer; overflow drops records (surfaced
	// as DroppedBefore) rather than blocking callers.
	queueCap = 1024

	// closeDrainTimeout bounds the Close flush wait.
	closeDrainTimeout = 5 * time.Second

	// dropLogEvery: one overflow log line per N dropped records.
	dropLogEvery = 32

	// idTsHexWidth is the hex width of the timestamp prefix of a record id.
	idTsHexWidth = 12

	// idRandWidth is the random suffix width of a record id (guard against
	// clock anomalies; the writer ts is already monotonic).
	idRandWidth = 4
)

// Mgr is the process-wide manager; nil means audit disabled (every entry
// point is a no-op).
var Mgr *Manager

// Manager serializes records into the kvgo store. Server goroutines only
// touch the queue and atomics; chain/timestamp state is owned by the writer
// goroutine except where mu is taken.
type Manager struct {
	db          kvapi.Client
	retentionMs int64 // record TTL, 0 = retain forever

	queue  chan *inapi.AuditRecord
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// stats
	droppedTotal   atomic.Int64
	droppedPending atomic.Int64
	writtenTotal   atomic.Int64

	// writer state, guarded by mu: the writer goroutine updates, the daily
	// anchor task and ChainHead readers only read.
	mu         sync.Mutex
	lastTs     int64
	prevHash   string
	anchorDate string // yyyymmdd of the last written anchor

	// authFailures dedups authentication-failure records.
	authFailures authFailureCache
}

// Setup initializes Mgr. A nil db or disabled config leaves Mgr nil (all
// entry points become no-ops).
func Setup(db kvapi.Client, cfg config.AuditConfig) error {

	if db == nil || !cfg.On() {
		slog.Info("audit log disabled")
		return nil
	}

	// Materialize defaults; idempotent for direct callers that skipped
	// config.Setup.
	cfg.Setup()

	m := &Manager{
		db:          db,
		retentionMs: cfg.RetentionMs(),
		queue:       make(chan *inapi.AuditRecord, queueCap),
		done:        make(chan struct{}),
		authFailures: authFailureCache{
			items: map[string]*authFailureEntry{},
		},
	}

	if err := m.recoverChainHead(); err != nil {
		// Non-fatal: the chain restarts from genesis, verify reports the break.
		slog.Error("audit chain head recovery failed", "err", err.Error())
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())

	Mgr = m

	go m.run()

	slog.Info("audit log enabled",
		"retention_days", *cfg.RetentionDays,
		"chain_head", m.ChainHead(),
	)

	return nil
}

// recoverChainHead seeds the chain from the most recent record across both
// zone candidates ("" covers pre-ZoneInit events); an empty store starts a
// genesis chain.
func (m *Manager) recoverChainHead() error {

	for _, zone := range zoneCandidates() {

		prefix := inapi.NsZoneletAuditLogPrefix(zone)
		rs := m.db.NewRanger(prefix, append(prefix, 0xff)).
			SetRevert(true).
			SetLimit(1).
			Exec()
		if !rs.OK() {
			if rs.NotFound() {
				continue
			}
			return rs.Error()
		}
		if len(rs.Items) == 0 {
			continue
		}

		var rec inapi.AuditRecord
		if err := rs.Items[0].JsonDecode(&rec); err != nil {
			return fmt.Errorf("decode audit record: %w", err)
		}

		m.mu.Lock()
		if rec.Id > auditIdOfTs(m.lastTs) {
			m.prevHash = rec.Hash
			m.lastTs = rec.Ts
		}
		m.mu.Unlock()
	}

	return nil
}

func (m *Manager) run() {
	defer close(m.done)
	for {
		select {
		case <-m.ctx.Done():
			m.drain()
			return
		case rec := <-m.queue:
			m.write(rec)
		}
	}
}

// drain flushes the queue after cancellation, bounded by closeDrainTimeout.
func (m *Manager) drain() {
	deadline := time.Now().Add(closeDrainTimeout)
	for {
		select {
		case rec := <-m.queue:
			m.write(rec)
			if !time.Now().Before(deadline) {
				slog.Error("audit close drain timeout, remaining records dropped",
					"left", len(m.queue))
				return
			}
		default:
			return
		}
	}
}

// write is the serialized storage path: monotonic ts clamp, hash chain,
// kvgo append with TTL. Key order equals chain order.
func (m *Manager) write(rec *inapi.AuditRecord) {

	now := time.Now().UnixMilli()

	m.mu.Lock()
	if now <= m.lastTs {
		// Absorb NTP step-back / same-ms bursts via the logical clock.
		now = m.lastTs + 1
	}
	m.lastTs = now

	rec.Ts = now
	rec.Id = AuditIdOfTs(now) + randHex(idRandWidth)
	rec.DroppedBefore = int32(m.droppedPending.Swap(0))
	rec.PrevHash = m.prevHash

	var err error
	rec.Hash, err = recordHash(rec)
	if err != nil {
		m.mu.Unlock()
		slog.Error("audit record hash failed, record dropped", "err", err.Error())
		return
	}
	m.prevHash = rec.Hash
	m.mu.Unlock()

	w := m.db.NewWriter(inapi.NsZoneletAuditLog(rec.Zone, rec.Id), rec)
	if m.retentionMs > 0 {
		w.SetTTL(m.retentionMs)
	}
	if rs := w.Exec(); !rs.OK() {
		slog.Error("audit record write failed",
			"id", rec.Id,
			"err", rs.ErrorMessage(),
		)
		return
	}

	m.writtenTotal.Add(1)
}

// emit enqueues without blocking; overflow drops and counts (surfaced as
// DroppedBefore on the next accepted record).
func (m *Manager) emit(rec *inapi.AuditRecord) {
	select {
	case m.queue <- rec:
	default:
		n := m.droppedTotal.Add(1)
		m.droppedPending.Add(1)
		if n%dropLogEvery == 1 {
			slog.Error("audit queue full, records being dropped",
				"dropped_total", n)
		}
	}
}

// Close flushes and stops the writer. Call after the gRPC server stops and
// before the database closes.
func Close() {
	if Mgr == nil {
		return
	}
	Mgr.close()
}

func (m *Manager) close() {
	m.cancel()
	select {
	case <-m.done:
	case <-time.After(closeDrainTimeout + time.Second):
		slog.Error("audit manager close timeout")
	}
}

// ChainHead returns the hash of the last successfully written record
// ("" for a genesis chain).
func (m *Manager) ChainHead() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prevHash
}

// Stats returns the written and dropped counters (reserved for /metrics).
func (m *Manager) Stats() (written, dropped int64) {
	return m.writtenTotal.Load(), m.droppedTotal.Load()
}

// zoneCandidates lists the zones records may live under: the configured
// zone plus "" (pre-ZoneInit events).
func zoneCandidates() []string {
	zones := []string{config.Config.Zonelet.ZoneName}
	if config.Config.Zonelet.ZoneName != "" {
		zones = append(zones, "")
	}
	return zones
}

// AuditIdOfTs renders the hex timestamp prefix of a record id; lexicographic
// order equals time order.
func AuditIdOfTs(ts int64) string {
	return fmt.Sprintf("%0*x", idTsHexWidth, ts)
}

// auditIdOfTs returns the smallest id for a timestamp (chain-head comparison).
func auditIdOfTs(ts int64) string {
	return AuditIdOfTs(ts) + "0000"
}

func randHex(n int) string {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand does not fail in practice; fall back to time.
		return fmt.Sprintf("%0*x", n, time.Now().UnixNano())
	}
	return hex.EncodeToString(b)[:n]
}
