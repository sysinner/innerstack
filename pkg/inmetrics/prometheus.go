// Copyright 2022 Google LLC
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

// Prometheus text exposition derived from ServiceWeaver/weaver and
// github.com/hooto/hmetrics (both Apache 2.0), simplified for local use.

package inmetrics

import (
	"bytes"
	"cmp"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// escaper escapes the Prometheus label-value special characters. Label
// values may hold any UTF-8, but backslash, double-quote and line feed
// must appear as \\, \" and \n.
//
// See https://prometheus.io/docs/instrumenting/exposition_formats/#text-format-details
var escaper = strings.NewReplacer("\\", `\\`, "\n", `\n`, "\"", `\"`)

// writePrometheusText renders the snapshots in the Prometheus text
// exposition format: metric names in ascending order, entries of one name
// in registration order, HELP/TYPE headers emitted once per name.
func writePrometheusText(w *bytes.Buffer, ms []*metricSnapshot) {
	slices.SortStableFunc(ms, func(a, b *metricSnapshot) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return cmp.Compare(a.Id, b.Id)
	})

	for i := 0; i < len(ms); {
		j := i
		for j < len(ms) && ms[j].Name == ms[i].Name {
			j++
		}
		translateMetricGroup(w, ms[i:j])
		i = j
	}
}

// translateMetricGroup renders all entries of a single metric name:
//
//	counter/gauge: name{label="value",...} value
//	histogram:     name_bucket{...,le="<bound>"} for every bound plus
//	               the mandatory +Inf bucket, then name_sum and name_count
func translateMetricGroup(w *bytes.Buffer, group []*metricSnapshot) {
	head := group[0]

	if head.Help != "" {
		w.WriteString("# HELP ")
		w.WriteString(head.Name)
		w.WriteByte(' ')
		w.WriteString(head.Help)
		w.WriteByte('\n')
	}

	w.WriteString("# TYPE ")
	w.WriteString(head.Name)
	isHistogram := head.Type == metricTypeHistogram
	if isHistogram {
		w.WriteString(" histogram\n")
	} else if head.Type == metricTypeCounter {
		w.WriteString(" counter\n")
	} else {
		w.WriteString(" gauge\n")
	}

	for i, m := range group {
		if isHistogram {
			hasInf := false

			var count uint64
			for idx, bound := range m.Bounds {
				count += m.Counts[idx]
				writeEntry(w, m.Name, float64(count), "_bucket", m.Labels, "le", bound)
				if math.IsInf(bound, +1) {
					hasInf = true
				}
			}

			// Account for the +Inf bucket.
			count += m.Counts[len(m.Bounds)]
			if !hasInf {
				writeEntry(w, m.Name, float64(count), "_bucket", m.Labels, "le", math.Inf(+1))
			}
			writeEntry(w, m.Name, m.Value, "_sum", m.Labels, "", 0)
			writeEntry(w, m.Name, float64(count), "_count", m.Labels, "", 0)
		} else { // counter or gauge
			writeEntry(w, m.Name, m.Value, "", m.Labels, "", 0)
		}
		// Blank line between histogram entries, matching the original
		// hmetrics layout.
		if isHistogram && i != len(group)-1 {
			w.WriteByte('\n')
		}
	}
	w.WriteByte('\n')
}

// writeEntry renders one sample line: name[suffix]{labels} value.
func writeEntry(w *bytes.Buffer, metricName string, value float64, suffix string,
	labels map[string]string, extraLabelName string, extraLabelItem float64) {
	w.WriteString(metricName)
	if suffix != "" {
		w.WriteString(suffix)
	}
	writeLabels(w, labels, extraLabelName, extraLabelItem)
	w.WriteByte(' ')
	w.WriteString(strconv.FormatFloat(value, 'f', -1, 64))
	w.WriteByte('\n')
}

// writeLabels renders the label set, sorted by label name, optionally
// appending the histogram-only extra label (le).
func writeLabels(w *bytes.Buffer, labels map[string]string,
	extraLabelName string, extraLabelItem float64) {
	if len(labels) == 0 && extraLabelName == "" {
		return
	}

	sortedLabels := slices.Sorted(maps.Keys(labels))

	separator := "{"
	for _, l := range sortedLabels {
		w.WriteString(separator)
		w.WriteString(l)
		w.WriteString(`="`)
		escaper.WriteString(w, labels[l]) // bytes.Buffer writes never fail
		w.WriteByte('"')
		separator = ","
	}
	if extraLabelName != "" {
		// Set for a histogram metric only.
		w.WriteString(separator)
		w.WriteString(extraLabelName)
		w.WriteString(`="`)
		w.WriteString(strconv.FormatFloat(extraLabelItem, 'f', -1, 64))
		w.WriteByte('"')
	}
	w.WriteString("}")
}
