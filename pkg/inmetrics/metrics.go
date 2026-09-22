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

// Package inmetrics implements a minimal local metrics registry with
// counter, gauge and histogram maps keyed by a (name, item) label pair,
// exported in the Prometheus text exposition format.
//
// It is an in-tree port of github.com/hooto/hmetrics. The metrics engine
// (registry, lock-free accounting, histogram buckets, snapshot) is a
// simplified re-implementation of the retired ServiceWeaver
// runtime/metrics package (Apache 2.0, Google LLC), carrying no external
// dependencies. The public API and the exported text format are unchanged.
package inmetrics

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Label is the fixed two-key label schema of every metric map, exported
// to Prometheus as {name="...", item="..."}.
type Label struct {
	Name string
	Item string
}

// MetricCounterMap is a counter collection keyed by (name, item).
type MetricCounterMap interface {
	Add(string, string, float64)
	Set(string, string, float64)
}

// MetricGaugeMap is a gauge collection keyed by (name, item).
type MetricGaugeMap interface {
	Add(string, string, float64)
	Set(string, string, float64)
}

// MetricHistogramMap is a histogram collection keyed by (name, item).
type MetricHistogramMap interface {
	Add(string, string, float64)
}

// MetricComplexMap feeds a counter, a gauge and a histogram sharing the
// same base name (<name>_counter, <name>_gauge, <name>_histogram).
type MetricComplexMap interface {
	Add(name, item string, c float64, g float64, t time.Duration)
}

// metricType enumerates the supported Prometheus metric types.
type metricType int

const (
	metricTypeInvalid metricType = iota
	metricTypeCounter
	metricTypeGauge
	metricTypeHistogram
)

// regMu guards regNames, regMetrics and regNextID. Every metric created
// through a map Get is appended here, so a scrape concurrent with the
// first Get on a fresh label pair is safe.
var (
	regMu      sync.RWMutex
	regNames   = map[string]bool{}
	regMetrics []*metric
	regNextID  uint64
)

// metric is a thread-safe readable and writeable metric. Values are kept
// lock-free via atomics; the hot path (Add/Put) never touches a mutex.
type metric struct {
	typ    metricType
	name   string
	help   string
	id     uint64            // registration order, used as a stable sort key
	labels map[string]string // fixed at construction: {"name":.., "item":..}

	fvalue atomicFloat64 // value for counter/gauge, sum for histogram

	// histogram only
	bounds []float64
	counts []atomic.Uint64 // len(bounds)+1, last one is the +Inf overflow
}

// metricSnapshot is a point-in-time copy of a metric.
type metricSnapshot struct {
	Id     uint64
	Type   metricType
	Name   string
	Labels map[string]string
	Help   string

	Value  float64
	Bounds []float64
	Counts []uint64
}

// register validates the arguments, claims the metric name globally and
// returns an empty map. Panics on duplicate or invalid registration,
// mirroring the original hmetrics behavior (an init-time programming
// error, not a runtime condition).
func register(typ metricType, name, help string, bounds []float64) *metricMap {
	if name == "" {
		panic("inmetrics: empty metric name")
	}
	if typ == metricTypeInvalid {
		panic(fmt.Sprintf("inmetrics: metric %q: invalid metric type", name))
	}
	for _, x := range bounds {
		if math.IsNaN(x) {
			panic(fmt.Sprintf("inmetrics: metric %q: NaN histogram bound", name))
		}
	}
	for i := 0; i < len(bounds)-1; i++ {
		if bounds[i] >= bounds[i+1] {
			panic(
				fmt.Sprintf(
					"inmetrics: metric %q: non-ascending histogram bounds %v",
					name,
					bounds,
				),
			)
		}
	}

	regMu.Lock()
	defer regMu.Unlock()
	if regNames[name] {
		panic(fmt.Sprintf("inmetrics: metric %q already exists", name))
	}
	regNames[name] = true
	return &metricMap{
		config:  metricMapConfig{typ: typ, name: name, help: help, bounds: bounds},
		metrics: map[Label]*metric{},
	}
}

// newMetric appends a metric for the given label pair to the registry.
// The name is already claimed by the owning map; no duplicate check here.
func newMetric(cfg metricMapConfig, l Label) *metric {
	regMu.Lock()
	defer regMu.Unlock()
	regNextID++
	m := &metric{
		typ:    cfg.typ,
		name:   cfg.name,
		help:   cfg.help,
		id:     regNextID,
		labels: map[string]string{"name": l.Name, "item": l.Item},
		bounds: cfg.bounds,
	}
	if cfg.typ == metricTypeHistogram {
		m.counts = make([]atomic.Uint64, len(cfg.bounds)+1)
	}
	regMetrics = append(regMetrics, m)
	return m
}

// snapshotOf returns a copy of the metric's current state.
func (m *metric) snapshotOf() *metricSnapshot {
	var counts []uint64
	if n := len(m.counts); n > 0 {
		counts = make([]uint64, n)
		for i := range m.counts {
			counts[i] = m.counts[i].Load()
		}
	}
	return &metricSnapshot{
		Id:     m.id,
		Name:   m.name,
		Type:   m.typ,
		Help:   m.help,
		Labels: maps.Clone(m.labels),
		Value:  m.fvalue.get(),
		Bounds: slices.Clone(m.bounds),
		Counts: counts,
	}
}

// snapshot returns a snapshot of every registered metric. The result is
// not atomic across metrics: concurrent Add/Put may be partially applied.
func snapshot() []*metricSnapshot {
	regMu.RLock()
	defer regMu.RUnlock()
	snapshots := make([]*metricSnapshot, 0, len(regMetrics))
	for _, m := range regMetrics {
		snapshots = append(snapshots, m.snapshotOf())
	}
	return snapshots
}

// metricMapConfig configures the metrics returned by a metricMap.
type metricMapConfig struct {
	typ    metricType
	name   string
	help   string
	bounds []float64
}

// metricMap is a collection of metrics sharing one registered name and
// bounds but distinct label values. Metrics are created lazily on the
// first Get of a label pair.
type metricMap struct {
	config  metricMapConfig
	mu      sync.Mutex // guards metrics
	metrics map[Label]*metric
}

// get returns the metric for the label pair, constructing and registering
// it if it does not exist yet.
func (mm *metricMap) get(l Label) *metric {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if m, ok := mm.metrics[l]; ok {
		return m
	}
	m := newMetric(mm.config, l)
	mm.metrics[l] = m
	return m
}

// atomicFloat64 provides atomic storage for float64 via its bit pattern.
type atomicFloat64 struct {
	v atomic.Uint64
}

func (f *atomicFloat64) get() float64 { return math.Float64frombits(f.v.Load()) }

func (f *atomicFloat64) set(v float64) { f.v.Store(math.Float64bits(v)) }

// add atomically adds v, retrying the CAS loop on contention.
func (f *atomicFloat64) add(v float64) {
	for {
		cur := f.v.Load()
		next := math.Float64bits(math.Float64frombits(cur) + v)
		if f.v.CompareAndSwap(cur, next) {
			return
		}
	}
}

// Add adds the delta to the metric value (or the sum, for histograms).
func (m *metric) Add(v float64) { m.fvalue.add(v) }

// Set overwrites the metric value.
func (m *metric) Set(v float64) { m.fvalue.set(v) }

// Put records v in the histogram: the covering bucket is incremented and
// v joins the running sum. A value exactly on a bound counts toward the
// next bucket; the last counts slot holds everything above the top bound.
func (m *metric) Put(v float64) {
	var idx int
	if len(m.bounds) == 0 || v < m.bounds[0] {
		// Values in the first bucket skip the binary search, which is
		// the common case for fast operations.
	} else {
		idx = sort.SearchFloat64s(m.bounds, v)
		if idx < len(m.bounds) && v == m.bounds[idx] {
			idx++
		}
	}
	m.counts[idx].Add(1)
	// Zero values would not move the sum but still count as observations.
	if v != 0 {
		m.fvalue.add(v)
	}
}

// RegisterCounterMap registers and returns a counter map. Panics if a
// metric with the same name is already registered.
func RegisterCounterMap(name, help string) MetricCounterMap {
	return &counterMap{
		metric: register(metricTypeCounter, name, help, nil),
	}
}

// RegisterGaugeMap registers and returns a gauge map. Panics if a metric
// with the same name is already registered.
func RegisterGaugeMap(name, help string) MetricGaugeMap {
	return &gaugeMap{
		metric: register(metricTypeGauge, name, help, nil),
	}
}

// RegisterHistogramMap registers and returns a histogram map with the
// given upper bounds. Panics if a metric with the same name is already
// registered.
func RegisterHistogramMap(name, help string, buckets []float64) MetricHistogramMap {
	return &histogramMap{
		metric: register(metricTypeHistogram, name, help, buckets),
	}
}

// NewBuckets returns count exponentially growing bounds starting at start
// (start, start*factor, start*factor^2, ...). Panics on non-positive
// start, a factor not greater than 1, or a count below 1.
func NewBuckets(start, factor float64, count int) []float64 {
	if count < 1 {
		panic("NewBuckets needs a positive count")
	}
	if start <= 0 {
		panic("NewBuckets needs a positive start value")
	}
	if factor <= 1 {
		panic("NewBuckets needs a factor greater than 1")
	}
	buckets := make([]float64, count)
	for i := range buckets {
		buckets[i] = start
		start *= factor
	}
	return buckets
}

// HttpHandler serves every registered metric in the Prometheus text
// exposition format.
func HttpHandler(w http.ResponseWriter, _ *http.Request) {
	var buf bytes.Buffer
	writePrometheusText(&buf, snapshot())
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write(buf.Bytes())
}

type counterMap struct {
	metric *metricMap
}

func (it *counterMap) Add(name, item string, v float64) {
	it.metric.get(Label{name, item}).Add(v)
}

func (it *counterMap) Set(name, item string, v float64) {
	it.metric.get(Label{name, item}).Set(v)
}

type gaugeMap struct {
	metric *metricMap
}

func (it *gaugeMap) Add(name, item string, v float64) {
	it.metric.get(Label{name, item}).Add(v)
}

func (it *gaugeMap) Set(name, item string, v float64) {
	it.metric.get(Label{name, item}).Set(v)
}

type histogramMap struct {
	metric *metricMap
}

func (it *histogramMap) Add(name, item string, v float64) {
	it.metric.get(Label{name, item}).Put(v)
}

type complexMap struct {
	counter   *metricMap
	gauge     *metricMap
	histogram *metricMap
}

var (
	complexMu      sync.Mutex
	complexMetrics = map[string]*complexMap{}
)

// RegisterComplexMap registers and returns the counter/gauge/histogram
// trio derived from name. Repeated calls with the same name return the
// cached instance instead of panicking on duplicate registration.
func RegisterComplexMap(name, help string, buckets []float64) MetricComplexMap {
	complexMu.Lock()
	defer complexMu.Unlock()
	m, ok := complexMetrics[name]
	if !ok {
		m = &complexMap{
			counter: register(metricTypeCounter, name+"_counter", help, nil),
			gauge:   register(metricTypeGauge, name+"_gauge", help, nil),
			histogram: register(
				metricTypeHistogram, name+"_histogram", help, buckets),
		}
		complexMetrics[name] = m
	}
	return m
}

func (it *complexMap) Add(name, item string, c, g float64, h time.Duration) {
	l := Label{name, item}
	if c > 0 {
		it.counter.get(l).Add(c)
	}
	if g != 0 {
		it.gauge.get(l).Add(g)
	}
	if h >= 0 {
		it.histogram.get(l).Put(h.Seconds())
	}
}
