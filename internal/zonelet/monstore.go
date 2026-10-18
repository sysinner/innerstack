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
	"encoding/binary"
	"log/slog"
	"sync/atomic"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// Built-in host metrics history. The leader
// samples the host status report cache into in-memory 10-minute blocks and
// flushes each rotated block to kvgo per (host, field) series with a
// retention TTL, so no sweeper code exists. The extractor table below is
// the single source pairing each stored field (inapi.MonFields order) with
// its derivation from a host status report; the /metrics host gauges use
// the same formulas.
//
// Leader-switch semantics: flushed blocks live in the shared store; the
// unflushed in-memory buffer is lost, at most one block (<= 10 minutes of
// samples). A non-leader term drops the buffer. Flushes run off the loop
// goroutine (a rotated block is self-contained); a persistently failing
// flush drops the block, bounded like every other loss mode.

// monHostExtractors derives the stored field values from one host status
// report, indexed to match inapi.MonFields: windowed cpu-ms / 60 =
// millicores, 1-minute deltas / 60 = bytes per second.
var monHostExtractors = []func(*inapi.HostStatus) int64{
	func(hs *inapi.HostStatus) int64 { return (hs.CpuSys + hs.CpuUser) / inutil.Window1Min },
	func(hs *inapi.HostStatus) int64 { return hs.MemUsed },
	func(hs *inapi.HostStatus) int64 { return hs.MemAvailable },
	func(hs *inapi.HostStatus) int64 { return hs.DiskFreeBytes },
	func(hs *inapi.HostStatus) int64 { return hs.DiskTotalBytes },
	func(hs *inapi.HostStatus) int64 { return hs.NetRecvBytes / inutil.Window1Min },
	func(hs *inapi.HostStatus) int64 { return hs.NetSentBytes / inutil.Window1Min },
	func(hs *inapi.HostStatus) int64 { return hs.DiskReadBytes / inutil.Window1Min },
	func(hs *inapi.HostStatus) int64 { return hs.DiskWriteBytes / inutil.Window1Min },
}

// monSample is one sampling pass over one host: the host it belongs to, the
// leader-clock timestamp and one value per monstore field.
type monSample struct {
	host   string
	ts     int64
	values []int64
}

// monSampleOf derives the monstore field values from one host status
// report.
func monSampleOf(hostId string, hs *inapi.HostStatus, now int64) *monSample {
	s := &monSample{host: hostId, ts: now, values: make([]int64, len(monHostExtractors))}
	for i, extract := range monHostExtractors {
		s.values[i] = extract(hs)
	}
	return s
}

// monstoreBuf is the leader's sampling buffer. It is only touched from the
// zonelet loop goroutine; the HostMetrics RPC reads the store instead.
type monstoreBuf struct {
	// blockStart is the unix-second start of the block being filled; 0
	// means no block is open.
	blockStart int64
	// samples holds the current block's points across all hosts.
	samples []*monSample
}

var monBuf monstoreBuf

// monFlushInFlight bounds concurrent flush goroutines; blocks beyond the
// cap are dropped (run data, not compliance data).
var monFlushInFlight atomic.Int32

// monstoreReset drops the in-memory buffer (non-leader term or leader
// switch); flushed blocks stay in the shared store.
func monstoreReset() {
	monBuf = monstoreBuf{}
}

// monstoreTick runs one sampling beat from the zonelet loop: append one
// sample per reported host, and hand the block to the flusher when it
// rotates.
func monstoreTick(now int64) {

	if config.Config.Zonelet.ZoneName == "" ||
		config.Config.Monstore.RetentionMs() <= 0 {
		return
	}

	if !status.IsZoneletLeader() {
		if monBuf.blockStart != 0 {
			monstoreReset()
		}
		return
	}

	if monBuf.blockStart == 0 {
		monBuf.blockStart = monBlockStartOf(now)
	}

	// Rotate before appending so the new sample lands in the fresh block,
	// never in the one being closed.
	if now-monBuf.blockStart >= inapi.MonBlockSeconds {
		block := monBlock{
			blockStart: monBuf.blockStart,
			samples:    monBuf.samples,
		}
		monBuf.blockStart = monBlockStartOf(now)
		monBuf.samples = nil
		monFlushAsync(block)
	}

	// Append one sample per reported host. Stale hosts are sampled too:
	// their last reported values repeat, and the outage is expressed by the
	// freshness alert and the report-age signal, not by data gaps.
	status.Zonelet_HostStatusSet.Range(func(key, value any) bool {
		hs, ok := value.(*inapi.HostStatus)
		if !ok || hs.HostId == "" {
			return true
		}
		monBuf.samples = append(monBuf.samples, monSampleOf(hs.HostId, hs, now))
		return true
	})
}

// monBlock is one closed, self-contained block window.
type monBlock struct {
	blockStart int64
	samples    []*monSample
}

// monBlockStartOf aligns a timestamp to its block window start.
func monBlockStartOf(ts int64) int64 {
	return ts - ts%inapi.MonBlockSeconds
}

// monFlushAsync writes one rotated block off the loop goroutine. A block
// whose write fails is logged and dropped: the loss is bounded to one
// 10-minute window of run data, same as a leader switch.
func monFlushAsync(block monBlock) {

	if monFlushInFlight.Load() >= 2 {
		slog.Warn("monstore flush dropped, too many in flight",
			"block_start", block.blockStart)
		return
	}

	zone := config.Config.Zonelet.ZoneName
	retention := config.Config.Monstore.RetentionMs()

	monFlushInFlight.Add(1)
	go func() {
		defer monFlushInFlight.Add(-1)
		monFlushBlock(zone, block, retention)
	}()
}

// monFlushBlock writes one block window, one key per (host, field) series.
func monFlushBlock(zone string, block monBlock, retention int64) {

	// Group by host so each series encodes in ts order.
	byHost := map[string][]*monSample{}
	for _, s := range block.samples {
		byHost[s.host] = append(byHost[s.host], s)
	}

	for host, samples := range byHost {
		tsList := make([]int64, len(samples))
		for i, s := range samples {
			tsList[i] = s.ts
		}
		for f, field := range inapi.MonFields {
			vals := make([]int64, len(samples))
			for i, s := range samples {
				vals[i] = s.values[f]
			}
			buf := monEncodeBlock(block.blockStart, tsList, vals)
			if rs := data.Zonelet.NewWriter(
				inapi.NsZoneletMonBlock(zone, host, field, block.blockStart),
				buf,
			).SetTTL(retention).Exec(); !rs.OK() {
				slog.Error("monstore block flush failed",
					"host", host, "field", field, "err", rs.ErrorMessage())
				return
			}
		}
	}
}

// monEncodeBlock renders one series block: varint header (sample count,
// block start ts) followed by per-sample [varint ts_delta, varint value].
func monEncodeBlock(blockStart int64, ts []int64, values []int64) []byte {

	var buf bytes.Buffer
	var tmp [binary.MaxVarintLen64]byte

	put := func(v int64) {
		n := binary.PutVarint(tmp[:], v)
		buf.Write(tmp[:n])
	}

	put(int64(len(ts)))
	put(blockStart)
	prev := blockStart
	for i := range ts {
		put(ts[i] - prev)
		prev = ts[i]
		put(values[i])
	}
	return buf.Bytes()
}

// monDecodeBlock is the inverse of monEncodeBlock.
func monDecodeBlock(b []byte) (blockStart int64, ts []int64, values []int64, err error) {

	r := bytes.NewReader(b)
	get := func() (int64, error) {
		return binary.ReadVarint(r)
	}

	count, err := get()
	if err != nil {
		return 0, nil, nil, err
	}
	blockStart, err = get()
	if err != nil {
		return 0, nil, nil, err
	}

	ts = make([]int64, 0, count)
	values = make([]int64, 0, count)
	prev := blockStart
	for i := int64(0); i < count; i++ {
		delta, err := get()
		if err != nil {
			return 0, nil, nil, err
		}
		prev += delta
		v, err := get()
		if err != nil {
			return 0, nil, nil, err
		}
		ts = append(ts, prev)
		values = append(values, v)
	}
	return blockStart, ts, values, nil
}

// monQuery reads one (host, field) series from the store, filters by the
// time window and downsamples by step (last value per bucket). Block ids
// are fixed-width hex timestamps, so the scan range is bounded to the
// query window; the merged points come out ascending.
func monQuery(host, field string, tsStart, tsEnd, step int64, limit int) (
	[]*inapi.HostMetricsPoint, error,
) {

	zone := config.Config.Zonelet.ZoneName
	// Bound the scan to the query window plus one block of margin on each
	// end (the range bounds' inclusivity is not part of the kvgo contract);
	// samples outside the window are filtered after decode.
	prefix := inapi.NsZoneletMonBlockPrefix(zone, host, field)
	from := prefix
	if tsStart > 0 {
		from = inapi.NsZoneletMonBlock(zone, host, field,
			monBlockStartOf(tsStart)-inapi.MonBlockSeconds)
	}
	to := append(prefix, 0xff)
	if tsEnd > 0 {
		to = inapi.NsZoneletMonBlock(zone, host, field,
			monBlockStartOf(tsEnd)+2*inapi.MonBlockSeconds)
	}

	rs := data.Zonelet.NewRanger(from, to).
		// kvgo default Limit is 10; one 7-day series holds ~1008 blocks.
		SetLimit(inapi.MonBlockScanLimit).
		Exec()
	if !rs.OK() && !rs.NotFound() {
		return nil, rs.Error()
	}

	var points []*inapi.HostMetricsPoint
	lastBucket := int64(-1)

	for _, item := range rs.Items {
		_, tsList, values, err := monDecodeBlock(item.Value)
		if err != nil {
			slog.Warn("monstore block decode failed",
				"host", host, "field", field, "err", err.Error())
			continue
		}
		for i, ts := range tsList {
			if tsStart > 0 && ts < tsStart {
				continue
			}
			if tsEnd > 0 && ts > tsEnd {
				continue
			}
			if len(points) >= limit {
				return points, nil
			}
			if step > 0 {
				bucket := ts / step
				if bucket == lastBucket {
					// Keep the last raw sample of the bucket.
					last := points[len(points)-1]
					last.Time = ts
					last.Value = values[i]
					continue
				}
				lastBucket = bucket
			}
			points = append(points, &inapi.HostMetricsPoint{Time: ts, Value: values[i]})
		}
	}
	return points, nil
}
