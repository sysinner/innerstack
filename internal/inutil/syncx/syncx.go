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

package syncx

import (
	"sync"
	"sync/atomic"
)

type Map struct {
	sync.Map
	size atomic.Int64
}

func (c *Map) Store(key, value any) {
	_, loaded := c.Map.Swap(key, value)
	if !loaded {
		c.size.Add(1)
	}
}

func (c *Map) Delete(key any) {
	_, loaded := c.Map.LoadAndDelete(key)
	if loaded {
		c.size.Add(-1)
	}
}

func (c *Map) Len() int {
	return int(c.size.Load())
}

func (c *Map) Clear() {
	c.size.Store(0)
	c.Map.Clear()
}
