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

package scheduler

import (
	"errors"
	"sort"
)

// prioritizer is a priority function that favors hosts with fewer requested resources.
func prioritizer(hosts []*hostFit) ([]hostPriority, error) {

	var ls []hostPriority

	if len(hosts) < 1 {
		return ls, errors.New("No Host Found")
	}

	for _, host := range hosts {
		ls = append(ls, calculateOccupancy(host))
	}

	sort.Slice(ls, func(i, j int) bool {
		return ls[i].score < ls[j].score
	})

	return ls, nil
}

// Calculate the occupancy on a host
func calculateOccupancy(host *hostFit) hostPriority {

	var (
		cpuUsedP = calculatePercentage(max(host.cpuAlloc, host.cpuUsed), host.cpuTotal)
		memUsedP = calculatePercentage(max(host.memAlloc, host.memUsed), host.memTotal)
	)

	return hostPriority{
		id:     host.id,
		score:  (cpuUsedP + memUsedP) / 2,
		volume: host.volume,
	}
}

func calculatePercentage(numerator, denominator int64) int64 {
	if denominator <= 0 {
		return 0
	}
	return (numerator * 100) / denominator
}
