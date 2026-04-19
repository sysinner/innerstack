// Copyright 2015 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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

func PrettyCPUs(value int64) string {

	if value >= 1000 {
		if value%1000 == 0 {
			return fmt.Sprintf("%d", value/1000)
		}
		return fmt.Sprintf("%.1f", float64(value)/1000)
	}

	return fmt.Sprintf("%dm", value)
}

func ParseCPUs(s string) (int64, error) {

	var (
		value      = float64(0)
		decimalDiv = float64(0)
	)

	s = strings.TrimSpace(s)

	for i, v := range []byte(s) {
		if v >= '0' && v <= '9' {
			if decimalDiv > 0 {
				if decimalDiv <= 100 {
					value += (float64(v-'0') / decimalDiv)
					decimalDiv *= 10
				}
			} else {
				value *= 10
				value += float64(v - '0')
			}
		} else if v == ',' {
			continue
		} else if v == '.' {
			if decimalDiv > 0 {
				return 0, errors.New("invalid human-cpus value")
			}
			decimalDiv = 10
		} else {
			if s[i] == 'm' {
				return int64(value), nil
			}
			return 0, errors.New("invalid unit")
		}
	}

	return int64(value * 1000), nil
}
