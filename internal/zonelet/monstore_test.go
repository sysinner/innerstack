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
	"testing"
	"time"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	zstatus "github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// monFieldTableGuard asserts the extractor table stays aligned with the
// stored field list (same order, same length).
func TestMonFieldTableGuard(t *testing.T) {
	if len(monHostExtractors) != len(inapi.MonFields) {
		t.Fatalf("extractor table has %d entries, MonFields has %d",
			len(monHostExtractors), len(inapi.MonFields))
	}
}

func TestMonBlockCodecRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		ts     []int64
		values []int64
	}{
		{"empty", nil, nil},
		{"single", []int64{1700000000}, []int64{0}},
		{"zeros", []int64{1700000000, 1700000030, 1700000060}, []int64{0, 0, 0}},
		{
			"extremes",
			[]int64{1700000000, 1700000030},
			[]int64{1<<62 + 1, -1},
		},
		{
			"negative and back",
			[]int64{1700000000, 1700000030, 1700000060},
			[]int64{-1 << 62, 42, -43},
		},
	}
	for _, tt := range tests {
		blockStart := int64(1699999800)
		buf := monEncodeBlock(blockStart, tt.ts, tt.values)
		gotStart, gotTs, gotVals, err := monDecodeBlock(buf)
		if err != nil {
			t.Fatalf("%s: decode: %v", tt.name, err)
		}
		if gotStart != blockStart {
			t.Fatalf("%s: block start = %d, want %d", tt.name, gotStart, blockStart)
		}
		if len(gotTs) != len(tt.ts) || len(gotVals) != len(tt.values) {
			t.Fatalf("%s: count = %d/%d, want %d/%d", tt.name,
				len(gotTs), len(gotVals), len(tt.ts), len(tt.values))
		}
		for i := range tt.ts {
			if gotTs[i] != tt.ts[i] || gotVals[i] != tt.values[i] {
				t.Fatalf("%s: sample %d = (%d, %d), want (%d, %d)", tt.name, i,
					gotTs[i], gotVals[i], tt.ts[i], tt.values[i])
			}
		}
	}

	// Truncated input errors instead of returning garbage.
	if _, _, _, err := monDecodeBlock([]byte{0x7f}); err == nil {
		t.Fatalf("truncated block should error")
	}
}

func TestMonstoreSampleFlushQuery(t *testing.T) {
	setupAlertsTest(t)

	const host = "h-mon-1"
	t0 := int64(1_700_000_000)
	prevRetention := config.Config.Monstore.RetentionDays
	config.Config.Monstore.RetentionDays = 7
	t.Cleanup(func() {
		config.Config.Monstore.RetentionDays = prevRetention
		monstoreReset()
	})

	// CpuSys+CpuUser = 30000ms over the 1min window -> 500 millicores.
	zstatus.Zonelet_HostStatusSet.Store(host, &inapi.HostStatus{
		HostId: host, Updated: t0,
		CpuSys: 12000, CpuUser: 18000,
		MemUsed: 1 << 30, MemAvailable: 2 << 30,
		DiskFreeBytes: 500, DiskTotalBytes: 1000,
		NetRecvBytes: 6000, NetSentBytes: 12000,
		DiskReadBytes: 30000, DiskWriteBytes: 0,
	})

	// Two samples into the first block, then rotate with a far tick. The
	// flush runs off the loop goroutine; wait for it.
	monstoreTick(t0)
	monstoreTick(t0 + 30)
	monstoreTick(t0 + 700)

	// The flush writes fields serially; wait for the LAST one (disk_write)
	// so every series is durable before the assertions below.
	waitFlushed := func() bool {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			prefix := inapi.NsZoneletMonBlockPrefix("alertstest", host, "disk_write")
			rs := data.Zonelet.NewRanger(prefix, append(prefix, 0xff)).
				SetLimit(10).Exec()
			if rs.OK() && len(rs.Items) > 0 {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if !waitFlushed() {
		t.Fatalf("rotated block was not flushed")
	}

	// Raw query: 2 samples, values derived per the shared formulas.
	points, err := monQuery(host, "cpu_millicores", 0, 0, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 {
		t.Fatalf("cpu points = %d, want 2", len(points))
	}
	for _, pt := range points {
		if pt.Value != 500 {
			t.Fatalf("cpu millicores = %d, want 500", pt.Value)
		}
	}

	points, err = monQuery(host, "net_recv", 0, 0, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0].Value != 100 { // 6000 / 60
		t.Fatalf("net_recv points = %+v, want 100/s", points)
	}

	// Flushed blocks carry the retention TTL.
	prefix := inapi.NsZoneletMonBlockPrefix("alertstest", host, "cpu_millicores")
	rs := data.Zonelet.NewRanger(prefix, append(prefix, 0xff)).SetLimit(10).Exec()
	if !rs.OK() || len(rs.Items) == 0 {
		t.Fatalf("flushed block missing: %v", rs.ErrorMessage())
	}
	if rs.Items[0].Meta == nil || rs.Items[0].Meta.Expired <= 0 {
		t.Fatalf("flushed block has no TTL")
	}

	// Step downsampling keeps the last value of each bucket.
	points, err = monQuery(host, "cpu_millicores", 0, 0, 60, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 {
		t.Fatalf("step 60s should merge 2 samples, got %d", len(points))
	}

	// Time window filtering (also exercises the bounded scan range).
	points, err = monQuery(host, "cpu_millicores", t0+1, 0, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].Time != t0+30 {
		t.Fatalf("window filter points = %+v", points)
	}

	// Point limit.
	points, err = monQuery(host, "cpu_millicores", 0, 0, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 {
		t.Fatalf("limit 1, got %d", len(points))
	}
}

func TestMonstoreDisabledAndStandby(t *testing.T) {
	setupAlertsTest(t)
	const host = "h-mon-2"
	t0 := int64(1_700_000_000)

	// Retention 0 disables sampling entirely.
	monstoreTick(t0)
	if monBuf.blockStart != 0 {
		t.Fatalf("disabled monstore should not open a block")
	}

	prev := config.Config.Monstore.RetentionDays
	config.Config.Monstore.RetentionDays = 7
	t.Cleanup(func() { config.Config.Monstore.RetentionDays = prev })

	// Standby does not sample.
	zstatus.ZoneletLeader = "h-someone-else"
	zstatus.Zonelet_HostStatusSet.Store(host, &inapi.HostStatus{HostId: host})
	monstoreTick(t0)
	if monBuf.blockStart != 0 {
		t.Fatalf("standby should not open a block")
	}
}

func BenchmarkMonstoreEncode(b *testing.B) {
	blockStart := int64(1699999800)
	ts := make([]int64, 1000)
	vals := make([]int64, 1000)
	for i := range ts {
		ts[i] = blockStart + int64(i)*30
		vals[i] = int64(i) * 12345
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		monEncodeBlock(blockStart, ts, vals)
	}
}

func BenchmarkMonstoreDecode(b *testing.B) {
	blockStart := int64(1699999800)
	ts := make([]int64, 1000)
	vals := make([]int64, 1000)
	for i := range ts {
		ts[i] = blockStart + int64(i)*30
		vals[i] = int64(i) * 12345
	}
	buf := monEncodeBlock(blockStart, ts, vals)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := monDecodeBlock(buf); err != nil {
			b.Fatal(err)
		}
	}
}
