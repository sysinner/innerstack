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

package network

import (
	"math/rand/v2"
	"slices"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// HostPortAlloc allocates a host port from the allocation range.
//
// If port > 0, it attempts to claim that specific port. Returns 0 if
// the port is already taken.
//
// If port == 0, it auto-selects a free port:
//  1. 10 random attempts within [inapi.HostPortMin, inapi.HostPortMax]
//  2. Sequential scan from the last allocated port + 1
//  3. Wrap-around scan from inapi.HostPortMin
//
// Returns the allocated port, or 0 if no port is available.
func HostPortAlloc(used []uint32, port uint32) uint32 {
	if port > 0 {
		if slices.Contains(used, port) {
			return 0
		}
		return port
	}

	if len(used) >= inapi.HostPortLimit {
		return 0
	}

	// Random attempt
	rn := inapi.HostPortMax - inapi.HostPortMin
	for i := 0; i < 10; i++ {
		p := inapi.HostPortMin + (rand.Uint32() % rn)
		if !slices.Contains(used, p) {
			return p
		}
	}

	// Sequential scan from last allocated port
	offset := inapi.HostPortMin
	if len(used) > 0 {
		last := used[len(used)-1]
		if last+1 > inapi.HostPortMin {
			offset = last + 1
		}
	}

	for p := offset; p <= inapi.HostPortMax; p++ {
		if !slices.Contains(used, p) {
			return p
		}
	}

	for p := inapi.HostPortMin; p < offset; p++ {
		if !slices.Contains(used, p) {
			return p
		}
	}

	return 0
}
