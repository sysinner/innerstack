// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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

package metrics

import (
	"runtime"
	"time"

	"github.com/sysinner/innerstack/v2/pkg/signals"
)

// RuntimeRefreshLoop refreshes the Go runtime gauges on a 30-second beat
// until shutdown. ReadMemStats stops the world briefly, so it runs here on
// the background beat, never on the scrape path.
func RuntimeRefreshLoop() {

	runtimeRefresh()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-signals.Done():
			return
		case <-ticker.C:
			runtimeRefresh()
		}
	}
}

func runtimeRefresh() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	goGoroutines.Set("go", "goroutines", float64(runtime.NumGoroutine()))
	goHeapBytes.Set("go", "heap_alloc", float64(ms.HeapAlloc))
	goGCCycles.Set("go", "gc_cycles", float64(ms.NumGC))
}
