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

package inutil

import (
	"testing"
)

func Test_ParseCPUs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"integer cores", "1", 1000, false},
		{"integer cores large", "4", 4000, false},
		{"decimal one digit", "1.5", 1500, false},
		{"decimal two digits", "1.25", 1250, false},
		{"decimal less than 1", "0.5", 500, false},
		{"decimal less than 1 two digits", "0.25", 250, false},
		{"millicores", "500m", 500, false},
		{"millicores small", "100m", 100, false},
		{"millicores large", "999m", 999, false},
		{"with comma", "1,000", 1000000, false},
		{"with comma and decimal", "1,500.5", 1500500, false},
		{"whitespace handling", " 2 ", 2000, false},
		{"zero value", "0", 0, false},
		{"zero millicores", "0m", 0, false},
		{"error multiple decimals", "1.2.3", 0, true},
		{"error invalid unit", "2x", 0, true},
		{"error invalid chars", "abc", 0, true},
		{"decimal followed by m", "1.m", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCPUs(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseCPUs(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseCPUs(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func Test_PrettyCPUs(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  string
	}{
		{"zero", 0, "0m"},
		{"less than 1000 single digit", 5, "5m"},
		{"less than 1000 hundreds", 500, "500m"},
		{"less than 1000 boundary", 999, "999m"},
		{"equals 1000", 1000, "1"},
		{"greater than 1000 divisible", 2000, "2"},
		{"greater than 1000 divisible large", 4000, "4"},
		{"greater than 1000 one decimal", 1500, "1.5"},
		{"greater than 1000 two decimals", 1250, "1.2"},
		{"greater than 1000 three decimals", 1234, "1.2"},
		{"greater than 1000 boundary", 1001, "1.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrettyCPUs(tt.input)
			if got != tt.want {
				t.Errorf("PrettyCPUs(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func Test_ParsePrettyCPUs_RoundTrip(t *testing.T) {
	tests := []string{
		"1",
		"2",
		"4",
		"1.5",
		"0.5",
		"500m",
		"100m",
	}

	for _, input := range tests {
		t.Run("roundtrip_"+input, func(t *testing.T) {
			parsed, err := ParseCPUs(input)
			if err != nil {
				t.Errorf("ParseCPUs(%q) error = %v", input, err)
				return
			}
			pretty := PrettyCPUs(parsed)
			parsed2, err := ParseCPUs(pretty)
			if err != nil {
				t.Errorf("ParseCPUs(%q) error = %v", pretty, err)
				return
			}
			if parsed != parsed2 {
				t.Errorf("Round trip failed: %q -> %d -> %q -> %d", input, parsed, pretty, parsed2)
			}
		})
	}
}
