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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
	"google.golang.org/protobuf/proto"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

const (
	// scanPageSize is the kvgo page size for range scans; a page may be
	// short (kvgo also caps by total size), so scans stop on an empty page.
	scanPageSize = 1000

	// verifyScanMax bounds the verify scan as a runaway guard.
	verifyScanMax = 1_000_000

	// anchorDateFormat is the yyyymmdd anchor key format.
	anchorDateFormat = "20060102"

	// ttlSlackMs is the clock slack when explaining a chain tail by TTL expiry.
	ttlSlackMs = int64(60 * 1000)
)

// protoMarshal is the canonical chain encoding (deterministic field order).
var protoMarshal = proto.MarshalOptions{Deterministic: true}

// recordHash computes hash = sha256(canonical(record, hash cleared)); the
// hash field is saved and restored in place.
func recordHash(rec *inapi.AuditRecord) (string, error) {
	cleared := rec.Hash
	rec.Hash = ""
	b, err := protoMarshal.Marshal(rec)
	rec.Hash = cleared
	if err != nil {
		return "", fmt.Errorf("audit record marshal: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ListOptions filters an audit query. Offset is the exclusive id cursor of
// the previous page's last item (in return order).
type ListOptions struct {
	Zone           string
	TsStart, TsEnd int64 // unix ms, 0 = unbounded
	ActorId        string
	Action         string
	TargetId       string // prefix match
	Status         string
	Limit          int
	Revert         bool // true: oldest first (chronological); false: newest first
	Offset         string
}

// List scans a zone's records and filters in memory (kvgo has no secondary
// index), bounded by inapi.AuditScanLimit keys. hasMore reports possible
// matches beyond the returned page.
func List(db kvapi.Client, opt ListOptions) ([]*inapi.AuditRecord, bool, error) {

	limit := opt.Limit
	if limit <= 0 {
		limit = inapi.AuditListLimitDefault
	}
	if limit > inapi.AuditListLimitMax {
		limit = inapi.AuditListLimitMax
	}

	prefix := inapi.NsZoneletAuditLogPrefix(opt.Zone)

	// kvgo ranges are ascending (lower, upper] / descending [lower, upper):
	// the cursor bound is exclusive in both directions.
	lower := append(bytes.Clone(prefix), minIdOfTs(opt.TsStart)...)
	upper := append(bytes.Clone(prefix), maxIdOfTs(opt.TsEnd)...)

	if opt.Offset != "" {
		cursor := append(bytes.Clone(prefix), opt.Offset...)
		if opt.Revert {
			lower = maxBytes(lower, cursor)
		} else {
			upper = minBytes(upper, cursor)
		}
	}

	var (
		items   []*inapi.AuditRecord
		scanned int
	)

	for scanned < inapi.AuditScanLimit {

		pageSize := min(scanPageSize, inapi.AuditScanLimit-scanned)

		r := db.NewRanger(bytes.Clone(lower), bytes.Clone(upper)).
			SetLimit(int64(pageSize))
		if !opt.Revert {
			// kvgo revert returns newest-first; ListOptions.Revert
			// (chronological) maps inverted.
			r = r.SetRevert(true)
		}

		rs := r.Exec()
		if !rs.OK() {
			if rs.NotFound() {
				break
			}
			return nil, false, rs.Error()
		}
		if len(rs.Items) == 0 {
			break
		}

		for _, kv := range rs.Items {

			scanned++

			var rec inapi.AuditRecord
			if err := kv.JsonDecode(&rec); err != nil {
				slog.Warn("audit record decode failed", "err", err.Error())
				continue
			}
			if !matchRecord(&rec, opt) {
				continue
			}

			items = append(items, &rec)
			if len(items) > limit {
				// One extra matching record proves more pages exist.
				return items[:limit], true, nil
			}
		}

		// Follow the page: the last item in return order is the next
		// exclusive bound.
		last := rs.Items[len(rs.Items)-1].Key
		if opt.Revert {
			lower = bytes.Clone(last)
		} else {
			upper = bytes.Clone(last)
		}
	}

	// Scan budget exhausted: more matching records may exist.
	hasMore := scanned >= inapi.AuditScanLimit

	return items, hasMore, nil
}

func matchRecord(rec *inapi.AuditRecord, opt ListOptions) bool {
	if opt.TsStart > 0 && rec.Ts < opt.TsStart {
		return false
	}
	if opt.TsEnd > 0 && rec.Ts > opt.TsEnd {
		return false
	}
	if opt.ActorId != "" && rec.ActorId != opt.ActorId {
		return false
	}
	if opt.Action != "" && rec.Action != opt.Action {
		return false
	}
	if opt.Status != "" && rec.Status != opt.Status {
		return false
	}
	if opt.TargetId != "" && !strings.HasPrefix(rec.TargetId, opt.TargetId) {
		return false
	}
	return true
}

// minIdOfTs renders the smallest record id for a start timestamp bound
// (empty when unbounded: no lower tightening).
func minIdOfTs(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return AuditIdOfTs(ts)
}

// maxIdOfTs renders the exclusive-past bound for an end timestamp: ids of
// records with ts <= tsEnd sort before prefix + hex(tsEnd) + 0xff.
func maxIdOfTs(ts int64) string {
	if ts <= 0 {
		return string([]byte{0xff})
	}
	return AuditIdOfTs(ts) + string([]byte{0xff})
}

func minBytes(a, b []byte) []byte {
	if bytes.Compare(a, b) <= 0 {
		return a
	}
	return b
}

func maxBytes(a, b []byte) []byte {
	if bytes.Compare(a, b) >= 0 {
		return a
	}
	return b
}

// VerifyOptions parameterizes a chain verification.
type VerifyOptions struct {
	Zone string

	// RetentionMs is the record TTL (0 = keep forever). With a finite
	// retention an unexplained tail may be TTL-expired records; with 0 it
	// must be genesis or an anchor hit.
	RetentionMs int64

	// NowMs overrides the verification clock (tests); 0 = time.Now.
	NowMs int64
}

// VerifyResult is the outcome of a full-chain verification.
type VerifyResult struct {
	Valid    bool
	Count    int64
	FirstId  string
	LastId   string
	HeadHash string
	BrokenAt string
}

// Verify recomputes the chain over all live records and validates the tail
// against genesis, daily anchors, or TTL expiry. Detects tampering with any
// live record or a removed live prefix; a full rewrite is only detectable
// via exported manifests.
func Verify(db kvapi.Client, opt VerifyOptions) VerifyResult {

	res := VerifyResult{Valid: true}

	now := opt.NowMs
	if now == 0 {
		now = time.Now().UnixMilli()
	}

	anchors, err := loadAnchors(db, opt.Zone)
	if err != nil {
		slog.Error("audit verify: load anchors failed", "err", err.Error())
		res.Valid = false
		res.BrokenAt = "(anchors)"
		return res
	}

	prefix := inapi.NsZoneletAuditLogPrefix(opt.Zone)
	lower := bytes.Clone(prefix)
	upper := append(bytes.Clone(prefix), 0xff)

	var prev *inapi.AuditRecord

	for res.Count < verifyScanMax {

		rs := db.NewRanger(bytes.Clone(lower), bytes.Clone(upper)).
			SetLimit(scanPageSize).
			Exec()
		if !rs.OK() {
			if rs.NotFound() {
				break
			}
			res.Valid = false
			res.BrokenAt = fmt.Sprintf("(scan: %s)", rs.ErrorMessage())
			return res
		}
		if len(rs.Items) == 0 {
			break
		}

		for _, kv := range rs.Items {

			var rec inapi.AuditRecord
			if err := kv.JsonDecode(&rec); err != nil {
				res.Valid = false
				res.BrokenAt = string(kv.Key)
				return res
			}

			res.Count++
			if res.FirstId == "" {
				res.FirstId = rec.Id
			}
			res.LastId = rec.Id

			// Recompute the record hash.
			h, err := recordHash(&rec)
			if err != nil || h != rec.Hash {
				res.Valid = false
				res.BrokenAt = rec.Id
				return res
			}

			if prev == nil {
				// Tail: genesis, an anchor hit, or TTL expiry.
				if rec.PrevHash != "" &&
					!anchors[rec.PrevHash] &&
					(opt.RetentionMs <= 0 || rec.Ts > now-opt.RetentionMs+ttlSlackMs) {
					res.Valid = false
					res.BrokenAt = rec.Id
					return res
				}
			} else if rec.PrevHash != prev.Hash {
				res.Valid = false
				res.BrokenAt = rec.Id
				return res
			}

			prev = &rec
		}

		lower = bytes.Clone(rs.Items[len(rs.Items)-1].Key)
	}

	if prev != nil {
		res.HeadHash = prev.Hash
	}

	if res.Count >= verifyScanMax {
		res.Valid = false
		res.BrokenAt = "(scan limit)"
	}

	return res
}

// loadAnchors reads all anchors of a zone into a head-hash set.
func loadAnchors(db kvapi.Client, zone string) (map[string]bool, error) {

	anchors := map[string]bool{}

	prefix := inapi.NsZoneletAuditAnchorPrefix(zone)
	lower := bytes.Clone(prefix)
	upper := append(bytes.Clone(prefix), 0xff)

	prevDate := ""

	for range verifyScanMax / scanPageSize {

		rs := db.NewRanger(bytes.Clone(lower), bytes.Clone(upper)).
			SetLimit(scanPageSize).
			Exec()
		if !rs.OK() {
			if rs.NotFound() {
				break
			}
			return nil, rs.Error()
		}
		if len(rs.Items) == 0 {
			break
		}

		for _, kv := range rs.Items {
			var a inapi.AuditAnchor
			if err := kv.JsonDecode(&a); err != nil {
				continue
			}
			if a.HeadHash != "" {
				anchors[a.HeadHash] = true
			}
			if a.Date != "" && a.Date <= prevDate {
				// Same-day overwrites are legal; keys are date-ordered so
				// this never fires for well-formed data.
				slog.Warn("audit anchor out of order", "date", a.Date)
			}
			if a.Date > prevDate {
				prevDate = a.Date
			}
		}

		lower = bytes.Clone(rs.Items[len(rs.Items)-1].Key)
	}

	return anchors, nil
}

// AnchorRefresh pins the chain head once per local day (no-op otherwise).
// Anchors have no TTL so historical heads stay verifiable after their
// records expire.
func AnchorRefresh() {
	if Mgr == nil {
		return
	}
	Mgr.anchorRefresh()
}

func (m *Manager) anchorRefresh() {

	today := time.Now().Format(anchorDateFormat)

	m.mu.Lock()
	if m.anchorDate == today {
		m.mu.Unlock()
		return
	}
	m.anchorDate = today
	head := m.prevHash
	m.mu.Unlock()

	// No records yet: nothing to pin.
	if head == "" {
		return
	}

	count, err := m.liveCount()
	if err != nil {
		slog.Error("audit anchor live count failed", "err", err.Error())
		return
	}

	anchor := &inapi.AuditAnchor{
		Date:     today,
		HeadHash: head,
		Count:    count,
	}

	key := inapi.NsZoneletAuditAnchor(config.Config.Zonelet.ZoneName, today)
	if rs := m.db.NewWriter(key, anchor).Exec(); !rs.OK() {
		slog.Error("audit anchor write failed", "err", rs.ErrorMessage())
		return
	}

	slog.Info("audit anchor written",
		"date", today,
		"count", count,
	)
}

// liveCount counts records across both zone candidates.
func (m *Manager) liveCount() (int64, error) {

	var total int64

	for _, zone := range zoneCandidates() {

		prefix := inapi.NsZoneletAuditLogPrefix(zone)
		lower := bytes.Clone(prefix)
		upper := append(bytes.Clone(prefix), 0xff)

		for range verifyScanMax / scanPageSize {

			rs := m.db.NewRanger(bytes.Clone(lower), bytes.Clone(upper)).
				SetLimit(scanPageSize).
				Exec()
			if !rs.OK() {
				if rs.NotFound() {
					break
				}
				return 0, rs.Error()
			}
			if len(rs.Items) == 0 {
				break
			}

			total += int64(len(rs.Items))
			lower = bytes.Clone(rs.Items[len(rs.Items)-1].Key)
		}
	}

	return total, nil
}
