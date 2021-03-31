// Copyright 2020 Eryx <evorui аt gmail dοt com>, All rights reserved.
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

package tsd

import (
	"strconv"
)

func arrayStringHas(ar []string, s string) bool {
	for _, v := range ar {
		if v == s {
			return true
		}
	}
	return false
}

func u64Allow(opbase, op uint64) bool {
	return (op & opbase) == op
}

func u64Remove(opbase, op uint64) uint64 {
	return (opbase | op) - (op)
}

func u64Append(opbase, op uint64) uint64 {
	return (opbase | op)
}

func parstInt(str string, def int64) int64 {
	if v, err := strconv.ParseInt(str, 10, 32); err == nil {
		return v
	}
	return def
}

func varUnixSecondFilter(v int64, min, max int64) int64 {
	if v < min {
		v = min
	} else if v > max {
		v = max
	}
	return v
}
