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

func Test_PrettyBytes(t *testing.T) {

	tests := []struct {
		name     string
		input    int64
		base     int64
		expected string
	}{
		// 用户要求的测试用例
		{
			name:     "16 GiB (整数)",
			input:    16 * GiByte,
			base:     1024,
			expected: "16 GiB",
		},
		{
			name:     "4.41 GiB → 4.4 GiB",
			input:    4735244007, // 4.41 * GiByte
			base:     1024,
			expected: "4.4 GiB",
		},
		{
			name:     "1 GiB (整数)",
			input:    1 * GiByte,
			base:     1024,
			expected: "1 GiB",
		},
		{
			name:     "460.43 GiB → 460 GiB",
			input:    494383273883, // 460.43 * GiByte
			base:     1024,
			expected: "460 GiB",
		},

		// 边界测试
		{
			name:     "0 bytes",
			input:    0,
			base:     1024,
			expected: "0 B",
		},
		{
			name:     "512 bytes",
			input:    512,
			base:     1024,
			expected: "512 B",
		},
		{
			name:     "1 KiB",
			input:    KiByte,
			base:     1024,
			expected: "1 KiB",
		},
		{
			name:     "1.5 KiB",
			input:    int64(1.5 * float64(KiByte)),
			base:     1024,
			expected: "1.5 KiB",
		},
		{
			name:     "1023 bytes",
			input:    1023,
			base:     1024,
			expected: "1023 B",
		},
		{
			name:     "1024 bytes = 1 KiB",
			input:    1024,
			base:     1024,
			expected: "1 KiB",
		},

		// 有效数字测试
		{
			name:     "9.9 GiB",
			input:    10628892211, // 9.9 * GiByte
			base:     1024,
			expected: "9.9 GiB",
		},
		{
			name:     "10.5 GiB → 10 GiB",
			input:    11274289152, // 10.5 * GiByte
			base:     1024,
			expected: "10 GiB",
		},
		{
			name:     "99.9 GiB (rounds to 100)",
			input:    107266932408, // 99.9 * GiByte
			base:     1024,
			expected: "100 GiB",
		},
		{
			name:     "100 GiB",
			input:    100 * GiByte,
			base:     1024,
			expected: "100 GiB",
		},

		// SI 单位测试 (base=1000)
		{
			name:     "16 GB (SI)",
			input:    16 * GByte,
			base:     1000,
			expected: "16 GB",
		},
		{
			name:     "1.5 GB (SI)",
			input:    int64(1.5 * float64(GByte)),
			base:     1000,
			expected: "1.5 GB",
		},
		{
			name:     "460 GB (SI)",
			input:    460 * GByte,
			base:     1000,
			expected: "460 GB",
		},

		// 大数值测试
		{
			name:     "1 TiB",
			input:    TiByte,
			base:     1024,
			expected: "1 TiB",
		},
		{
			name:     "1 PiB",
			input:    PiByte,
			base:     1024,
			expected: "1 PiB",
		},
		{
			name:     "1 EiB",
			input:    EiByte,
			base:     1024,
			expected: "1 EiB",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrettyBytes(tt.input, tt.base)
			if got != tt.expected {
				t.Errorf("PrettyBytes(%d, %d) = %q, want %q", tt.input, tt.base, got, tt.expected)
			}
		})
	}
}

func Test_ParseBytes(t *testing.T) {

	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		// basic tests
		{
			name:  "empty string",
			input: "",
			want:  0,
		},
		{
			name:  "zero",
			input: "0",
			want:  0,
		},
		{
			name:  "simple bytes",
			input: "100",
			want:  100,
		},
		{
			name:  "bytes with b unit",
			input: "100b",
			want:  100,
		},

		// IEC unit tests
		{
			name:  "1 KiB",
			input: "1kib",
			want:  KiByte,
		},
		{
			name:  "1 MiB",
			input: "1mib",
			want:  MiByte,
		},
		{
			name:  "1 GiB",
			input: "1gib",
			want:  GiByte,
		},
		{
			name:  "1 TiB",
			input: "1tib",
			want:  TiByte,
		},
		{
			name:  "1 PiB",
			input: "1pib",
			want:  PiByte,
		},
		{
			name:  "1 EiB",
			input: "1eib",
			want:  EiByte,
		},
		{
			name:  "512 MiB",
			input: "512mib",
			want:  512 * MiByte,
		},
		{
			name:  "2 GiB",
			input: "2gib",
			want:  2 * GiByte,
		},

		// IEC short form tests
		{
			name:  "1 Ki",
			input: "1ki",
			want:  KiByte,
		},
		{
			name:  "1 Mi",
			input: "1mi",
			want:  MiByte,
		},
		{
			name:  "1 Gi",
			input: "1gi",
			want:  GiByte,
		},
		{
			name:  "1 Ti",
			input: "1ti",
			want:  TiByte,
		},
		{
			name:  "1 Pi",
			input: "1pi",
			want:  PiByte,
		},
		{
			name:  "1 Ei",
			input: "1ei",
			want:  EiByte,
		},
		{
			name:  "1024 Mi",
			input: "1024mi",
			want:  1024 * MiByte,
		},

		// SI unit tests
		{
			name:  "1 KB",
			input: "1kb",
			want:  KByte,
		},
		{
			name:  "1 MB",
			input: "1mb",
			want:  MByte,
		},
		{
			name:  "1 GB",
			input: "1gb",
			want:  GByte,
		},
		{
			name:  "1 TB",
			input: "1tb",
			want:  TByte,
		},
		{
			name:  "1 PB",
			input: "1pb",
			want:  PByte,
		},
		{
			name:  "1 EB",
			input: "1eb",
			want:  EByte,
		},
		{
			name:  "500 MB",
			input: "500mb",
			want:  500 * MByte,
		},
		{
			name:  "2 GB",
			input: "2gb",
			want:  2 * GByte,
		},

		// SI short form tests
		{
			name:  "1 K",
			input: "1k",
			want:  KByte,
		},
		{
			name:  "1 M",
			input: "1m",
			want:  MByte,
		},
		{
			name:  "1 G",
			input: "1g",
			want:  GByte,
		},
		{
			name:  "1 T",
			input: "1t",
			want:  TByte,
		},
		{
			name:  "1 P",
			input: "1p",
			want:  PByte,
		},
		{
			name:  "1 E",
			input: "1e",
			want:  EByte,
		},
		{
			name:  "1000 M",
			input: "1000m",
			want:  1000 * MByte,
		},

		// decimal tests
		{
			name:  "1.5 GB",
			input: "1.5gb",
			want:  int64(1.5 * float64(GByte)),
		},
		{
			name:  "0.5 GB",
			input: "0.5gb",
			want:  int64(0.5 * float64(GByte)),
		},
		{
			name:  "2.25 GB",
			input: "2.25gb",
			want:  int64(2.25 * float64(GByte)),
		},
		{
			name:  "1.5 GiB",
			input: "1.5gib",
			want:  int64(1.5 * float64(GiByte)),
		},
		{
			name:  "0.25 MiB",
			input: "0.25mib",
			want:  int64(0.25 * float64(MiByte)),
		},

		// case insensitive tests
		{
			name:  "1GB uppercase",
			input: "1GB",
			want:  GByte,
		},
		{
			name:  "1Gb mixed case",
			input: "1Gb",
			want:  GByte,
		},
		{
			name:  "1gB mixed case",
			input: "1gB",
			want:  GByte,
		},
		{
			name:  "1GiB uppercase",
			input: "1GiB",
			want:  GiByte,
		},
		{
			name:  "1KIB uppercase",
			input: "1KIB",
			want:  KiByte,
		},

		// whitespace tests
		{
			name:  "1 GB with space",
			input: "1 gb",
			want:  GByte,
		},
		{
			name:  "1 GiB with space",
			input: "1 gib",
			want:  GiByte,
		},
		{
			name:  "1.5 GB with space",
			input: "1.5 gb",
			want:  int64(1.5 * float64(GByte)),
		},

		// comma separator tests
		{
			name:  "1,000 B with comma",
			input: "1,000",
			want:  1000,
		},
		{
			name:  "1,000,000 MB with comma",
			input: "1,000,000mb",
			want:  1000000 * MByte,
		},
		{
			name:  "1,234.56 MB with comma",
			input: "1,234.56mb",
			want:  int64(1234.56 * float64(MByte)),
		},

		// edge cases
		{
			name:  "large value",
			input: "1eib",
			want:  EiByte,
		},
		{
			name:  "small fraction",
			input: "0.1gb",
			want:  int64(0.1 * float64(GByte)),
		},

		// invalid input tests
		{
			name:    "invalid unit",
			input:   "1xb",
			want:    0,
			wantErr: true,
		},
		{
			name:    "multiple decimal points",
			input:   "1.5.5gb",
			want:    0,
			wantErr: true,
		},
		{
			name:  "only unit",
			input: "gb",
			want:  0,
		},
		{
			name:    "only numbers with invalid chars",
			input:   "1a2b",
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseBytes(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseBytes(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseBytes(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
