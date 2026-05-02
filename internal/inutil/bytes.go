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
	"errors"
	"fmt"
	"strings"
)

const (
	Byte int64 = 1

	// IEC units (base 1024)
	KiByte = 1024 * Byte
	MiByte = 1024 * KiByte
	GiByte = 1024 * MiByte
	TiByte = 1024 * GiByte
	PiByte = 1024 * TiByte
	EiByte = 1024 * PiByte

	// SI units (base 1000)
	KByte = 1000 * Byte
	MByte = 1000 * KByte
	GByte = 1000 * MByte
	TByte = 1000 * GByte
	PByte = 1000 * TByte
	EByte = 1000 * PByte
)

var byteSizeMap = map[string]int64{
	"":  Byte,
	"b": Byte,

	// IEC Sizes
	"kib": KiByte,
	"mib": MiByte,
	"gib": GiByte,
	"tib": TiByte,
	"pib": PiByte,
	"eib": EiByte,

	"ki": KiByte,
	"mi": MiByte,
	"gi": GiByte,
	"ti": TiByte,
	"pi": PiByte,
	"ei": EiByte,

	// SI Sizes
	"kb": KByte,
	"mb": MByte,
	"gb": GByte,
	"tb": TByte,
	"pb": PByte,
	"eb": EByte,

	"k": KByte,
	"m": MByte,
	"g": GByte,
	"t": TByte,
	"p": PByte,
	"e": EByte,
}

var iecUnits = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
var siUnits = []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}

// PrettyBytes formats bytes into human-readable string.
func PrettyBytes(v, base int64) string {
	if base != 1024 {
		base = 1000
	}

	value := float64(v)
	i := 0
	for value >= float64(base) && i < len(iecUnits)-1 {
		value /= float64(base)
		i++
	}

	unit := iecUnits[i]
	if base != 1024 {
		unit = siUnits[i]
	}

	var s string
	if value >= 100 {
		s = fmt.Sprintf("%.0f", value)
	} else if value >= 10 {
		s = fmt.Sprintf("%.0f", value)
	} else {
		s = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", value), "0"), ".")
	}

	return fmt.Sprintf("%s %s", s, unit)
}

// ParseBytes parses human-readable byte string to int64.
func ParseBytes(s string) (int64, error) {
	var value, value2, unitRate float64 = 0, 0, 1

	s = strings.TrimSpace(s)

	for i, v := range []byte(s) {
		if v >= '0' && v <= '9' {
			if value2 > 0 {
				if value2 <= 100 {
					value += float64(v-'0') / value2
					value2 *= 10
				}
			} else {
				value = value*10 + float64(v-'0')
			}
		} else if v == ',' {
			continue
		} else if v == '.' {
			if value2 > 0 {
				return 0, errors.New("invalid human-bytes value")
			}
			value2 = 10
		} else {
			unit := strings.TrimSpace(strings.ToLower(s[i:]))
			if u, ok := byteSizeMap[unit]; ok {
				unitRate = float64(u)
			} else if len(unit) > 0 {
				return 0, errors.New("invalid unit")
			}
			break
		}
	}

	return int64(value * unitRate), nil
}
