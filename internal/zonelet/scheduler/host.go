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

package scheduler

type hostFit struct {
	id       string
	cpuAlloc int64 // mCores (1 core = 1000m)
	cpuTotal int64 // mCores (1 core = 1000m)
	cpuUsed  int64 // mCores (1 core = 1000m)
	memAlloc int64 // Bytes
	memTotal int64 // Bytes
	memUsed  int64 // Bytes
	volume   string
}

// hostPriority represents the priority of scheduling to a particular host, lower priority is better.
type hostPriority struct {
	id     string
	score  int64
	volume string
}
