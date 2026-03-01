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
	"sync"
	"time"
)

const (
	Window1Min  int64 = 60
	Window5Min  int64 = 300
	Window15Min int64 = 900
)

// SlidingCounter implements a ring buffer for time-series delta calculations.
type SlidingCounter struct {
	mu          sync.RWMutex
	samples     []int64 // ring buffer
	lastSeconds int64
	lastValue   int64
	size        int64
}

// GroupSlidingCounter manages multiple named SlidingCounters.
type GroupSlidingCounter struct {
	mu      sync.RWMutex
	metrics map[string]*SlidingCounter
	size    int64
}

func NewGroupSlidingCounter(maxSeconds int64) *GroupSlidingCounter {
	return &GroupSlidingCounter{
		metrics: map[string]*SlidingCounter{},
		size:    max(60, maxSeconds),
	}
}

// NewSlidingCounter creates a counter with the specified window size.
func NewSlidingCounter(maxSeconds int64) *SlidingCounter {
	return &SlidingCounter{
		samples: make([]int64, int(maxSeconds)),
		size:    max(60, maxSeconds),
	}
}

func (c *GroupSlidingCounter) Counter(name string) *SlidingCounter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.metrics[name]; ok {
		return m
	}
	m := NewSlidingCounter(c.size)
	c.metrics[name] = m
	return m
}

// Record stores a cumulative value and returns the counter for chaining.
func (c *SlidingCounter) Record(totalValue int64) *SlidingCounter {
	t := time.Now().Unix()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Initialize or reset on counter decrease
	if c.lastSeconds == 0 || totalValue < c.lastValue {
		c.lastSeconds = t
		c.lastValue = totalValue
		for i := 0; i < int(c.size); i++ {
			c.samples[i] = totalValue
		}
	}

	if t == c.lastSeconds {
		c.samples[int(t%c.size)] = totalValue
	} else {
		for i := c.lastSeconds + 1; i < t; i++ {
			c.samples[int(i%c.size)] = c.lastValue
		}
		c.samples[int(t%c.size)] = totalValue
		c.lastSeconds = t
		c.lastValue = totalValue
	}

	return c
}

// Delta returns the increment over the past N seconds.
func (c *SlidingCounter) Delta(seconds int64) int64 {
	t := time.Now().Unix()

	c.mu.RLock()
	defer c.mu.RUnlock()

	if seconds > c.size {
		seconds = c.size
	}

	return max(0, c.samples[int(t%c.size)]-c.samples[int((t-seconds)%c.size)])
}
