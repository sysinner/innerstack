package inmetrics

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNewBuckets verifies the exponential bucket series and the argument
// validation of NewBuckets.
func TestNewBuckets(t *testing.T) {
	tests := []struct {
		name    string
		start   float64
		factor  float64
		count   int
		want    []float64
		wantPnc bool
	}{
		{
			name:   "exponential series",
			start:  1,
			factor: 2,
			count:  4,
			want:   []float64{1, 2, 4, 8},
		},
		{
			name:   "single bucket",
			start:  0.0001,
			factor: 1.5,
			count:  1,
			want:   []float64{0.0001},
		},
		{
			name:    "zero count",
			start:   1,
			factor:  2,
			count:   0,
			wantPnc: true,
		},
		{
			name:    "non-positive start",
			start:   0,
			factor:  2,
			count:   4,
			wantPnc: true,
		},
		{
			name:    "factor not greater than one",
			start:   1,
			factor:  1,
			count:   4,
			wantPnc: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				err := recover()
				if tc.wantPnc && err == nil {
					t.Fatal("expected panic, got none")
				}
				if !tc.wantPnc && err != nil {
					t.Fatalf("unexpected panic: %v", err)
				}
			}()
			got := NewBuckets(tc.start, tc.factor, tc.count)
			if tc.wantPnc {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("buckets len = %d, want %d (%v)", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("buckets[%d] = %v, want %v (full: %v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

// TestRegisterDuplicatePanics verifies the global name uniqueness check.
func TestRegisterDuplicatePanics(t *testing.T) {
	RegisterCounterMap("dup_metric", "help")
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate metric name, got none")
		}
	}()
	RegisterGaugeMap("dup_metric", "help")
}

// TestRegisterComplexMapIdempotent verifies that repeated registration of
// the same complex name returns the cached instance and that its three
// sub-metrics route values independently.
func TestRegisterComplexMapIdempotent(t *testing.T) {
	buckets := NewBuckets(0.0001, 1.5, 36)

	m1 := RegisterComplexMap("cx_metric", "help", buckets)
	m2 := RegisterComplexMap("cx_metric", "help", buckets)
	if m1 != m2 {
		t.Fatal("repeated RegisterComplexMap returned different instances")
	}

	m1.Add("Svc", "Item", 1, 2, 3*time.Second)

	var counterVal, gaugeVal, histSum float64
	var histCount uint64
	for _, s := range snapshot() {
		switch s.Name {
		case "cx_metric_counter":
			counterVal = s.Value
		case "cx_metric_gauge":
			gaugeVal = s.Value
		case "cx_metric_histogram":
			histSum = s.Value
			for _, c := range s.Counts {
				histCount += c
			}
		}
	}
	if counterVal != 1 {
		t.Errorf("cx counter = %v, want 1", counterVal)
	}
	if gaugeVal != 2 {
		t.Errorf("cx gauge = %v, want 2", gaugeVal)
	}
	if histSum != 3 {
		t.Errorf("cx histogram sum = %v, want 3", histSum)
	}
	if histCount != 1 {
		t.Errorf("cx histogram count = %d, want 1", histCount)
	}
}

// TestCounterGaugeMap verifies Add/Set accounting, label materialization
// and per-label-pair isolation.
func TestCounterGaugeMap(t *testing.T) {
	cm := RegisterCounterMap("unit_counter", "help")
	gm := RegisterGaugeMap("unit_gauge", "help")

	cm.Add("Svc", "Op1", 1)
	cm.Add("Svc", "Op1", 2)
	cm.Add("Svc", "Op2", 5)
	cm.Set("Zone", "Z1", 7)

	gm.Set("Svc", "Mem", 12.5)
	gm.Add("Svc", "Mem", 0.5)
	gm.Set("Svc", "Mem", 3)

	want := map[string]struct {
		typ   metricType
		value float64
	}{
		"unit_counter:{Svc,Op1}": {metricTypeCounter, 3},
		"unit_counter:{Svc,Op2}": {metricTypeCounter, 5},
		"unit_counter:{Zone,Z1}": {metricTypeCounter, 7},
		"unit_gauge:{Svc,Mem}":   {metricTypeGauge, 3},
	}

	got := map[string]*metricSnapshot{}
	for _, s := range snapshot() {
		if !strings.HasPrefix(s.Name, "unit_") {
			continue
		}
		key := fmt.Sprintf("%s:{%s,%s}", s.Name, s.Labels["name"], s.Labels["item"])
		got[key] = s
	}

	if len(got) != len(want) {
		t.Fatalf("snapshot entries = %d, want %d: %v", len(got), len(want), got)
	}
	for key, w := range want {
		s, ok := got[key]
		if !ok {
			t.Fatalf("missing snapshot entry %q", key)
		}
		if s.Type != w.typ {
			t.Errorf("%q type = %v, want %v", key, s.Type, w.typ)
		}
		if s.Value != w.value {
			t.Errorf("%q value = %v, want %v", key, s.Value, w.value)
		}
		if len(s.Labels) != 2 || s.Labels["name"] == "" || s.Labels["item"] == "" {
			t.Errorf("%q labels = %v, want name/item populated", key, s.Labels)
		}
	}
}

// TestHistogramBuckets verifies bucket placement, including the
// on-a-bound-goes-to-the-next-bucket rule and the overflow slot.
func TestHistogramBuckets(t *testing.T) {
	tests := []struct {
		value    float64
		wantSlot int // index into counts, len(bounds)+1 is the overflow
	}{
		{value: 0, wantSlot: 0},
		{value: 0.05, wantSlot: 0},
		{value: 0.1, wantSlot: 1}, // on the first bound -> next bucket
		{value: 0.3, wantSlot: 1},
		{value: 0.5, wantSlot: 2}, // on the middle bound -> next bucket
		{value: 0.75, wantSlot: 2},
		{value: 1, wantSlot: 3}, // on the top bound -> overflow slot
		{value: 5, wantSlot: 3},
	}

	for i, tc := range tests {
		t.Run(fmt.Sprintf("case_%d_value_%v", i, tc.value), func(t *testing.T) {
			name := fmt.Sprintf("hist_case_%d", i)
			hm := RegisterHistogramMap(name, "help", []float64{0.1, 0.5, 1})
			hm.Add("Svc", "Op", tc.value)

			var s *metricSnapshot
			for _, cand := range snapshot() {
				if cand.Name == name {
					s = cand
					break
				}
			}
			if s == nil {
				t.Fatal("histogram snapshot not found")
			}

			if len(s.Counts) != 4 {
				t.Fatalf("counts len = %d, want 4: %v", len(s.Counts), s.Counts)
			}
			for slot, count := range s.Counts {
				if slot == tc.wantSlot && count != 1 {
					t.Errorf("counts[%d] = %d, want 1 (all: %v)", slot, count, s.Counts)
				}
				if slot != tc.wantSlot && count != 0 {
					t.Errorf("counts[%d] = %d, want 0 (all: %v)", slot, count, s.Counts)
				}
			}
			if s.Value != tc.value {
				t.Errorf("sum = %v, want %v", s.Value, tc.value)
			}
		})
	}
}

// TestPrometheusTextFormat verifies the exact exposition output for
// counter and histogram groups, including label sorting, cumulative
// buckets, the +Inf bucket, and sum/count lines.
func TestPrometheusTextFormat(t *testing.T) {
	cm := RegisterCounterMap("gtw_counter", "The Test Counter")
	cm.Add("Service", "Root", 3)
	cm.Add("Service", "Route", 1)
	cm.Set("Zone", "Z1", 7)

	hm := RegisterHistogramMap("gtw_latency", "The Test Latency", []float64{0.1, 0.5, 1})
	hm.Add("Service", "Root", 0.25)
	hm.Add("Service", "Root", 0.5)
	hm.Add("Service", "Root", 2)

	names := map[string]bool{"gtw_counter": true, "gtw_latency": true}
	var filtered []*metricSnapshot
	for _, s := range snapshot() {
		if names[s.Name] {
			filtered = append(filtered, s)
		}
	}

	var buf bytes.Buffer
	writePrometheusText(&buf, filtered)

	want := strings.Join([]string{
		`# HELP gtw_counter The Test Counter`,
		`# TYPE gtw_counter counter`,
		`gtw_counter{item="Root",name="Service"} 3`,
		`gtw_counter{item="Route",name="Service"} 1`,
		`gtw_counter{item="Z1",name="Zone"} 7`,
		``,
		`# HELP gtw_latency The Test Latency`,
		`# TYPE gtw_latency histogram`,
		`gtw_latency_bucket{item="Root",name="Service",le="0.1"} 0`,
		`gtw_latency_bucket{item="Root",name="Service",le="0.5"} 1`,
		`gtw_latency_bucket{item="Root",name="Service",le="1"} 2`,
		`gtw_latency_bucket{item="Root",name="Service",le="+Inf"} 3`,
		`gtw_latency_sum{item="Root",name="Service"} 2.75`,
		`gtw_latency_count{item="Root",name="Service"} 3`,
		``,
		``,
	}, "\n")

	if got := buf.String(); got != want {
		t.Fatalf("prometheus text mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestHttpHandler verifies the /metrics endpoint wrapper: content type
// and the presence of its own metric lines.
func TestHttpHandler(t *testing.T) {
	cm := RegisterCounterMap("http_counter", "The Http Counter")
	cm.Add("Service", "Root", 42)

	rec := httptest.NewRecorder()
	HttpHandler(rec, httptest.NewRequest(http.MethodGet, "/+/metrics", nil))

	if code := rec.Code; code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("content-type = %q, want prometheus text format", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `http_counter{item="Root",name="Service"} 42`) {
		t.Errorf("body missing metric line:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE http_counter counter") {
		t.Errorf("body missing TYPE header:\n%s", body)
	}
}

// TestConcurrentAccess hammers Add/Put from multiple goroutines while a
// scraper reads snapshots; run with -race to verify the lock-free paths.
func TestConcurrentAccess(t *testing.T) {
	const (
		goroutines = 8
		perG       = 500
	)
	total := goroutines * perG

	cm := RegisterCounterMap("conc_counter", "help")
	hm := RegisterHistogramMap("conc_latency", "help", []float64{0.1, 1})

	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range perG {
				cm.Add("Svc", "Op", 1)
				hm.Add("Svc", "Op", 0.5)
			}
		})
	}

	scrapeDone := make(chan struct{})
	go func() {
		for {
			select {
			case <-scrapeDone:
				return
			default:
				snapshot()
			}
		}
	}()

	wg.Wait()
	close(scrapeDone)

	var counterSum, histSum float64
	var histCount uint64
	for _, s := range snapshot() {
		switch s.Name {
		case "conc_counter":
			counterSum += s.Value
		case "conc_latency":
			histSum = s.Value
			for _, c := range s.Counts {
				histCount += c
			}
		}
	}
	if counterSum != float64(total) {
		t.Errorf("counter sum = %v, want %d", counterSum, total)
	}
	if histSum != float64(total)/2 {
		t.Errorf("histogram sum = %v, want %v", histSum, float64(total)/2)
	}
	if histCount != uint64(total) {
		t.Errorf("histogram count = %d, want %d", histCount, total)
	}
}

func BenchmarkCounterMapAdd(b *testing.B) {
	cm := RegisterCounterMap("bench_counter", "help")
	for b.Loop() {
		cm.Add("Svc", "Op", 1)
	}
}

func BenchmarkHistogramMapPut(b *testing.B) {
	hm := RegisterHistogramMap("bench_latency", "help", NewBuckets(0.0001, 1.5, 36))
	for b.Loop() {
		hm.Add("Svc", "Op", 0.01)
	}
}
