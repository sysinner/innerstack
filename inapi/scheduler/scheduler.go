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

// Scheduler is an interface implemented by things that know how to schedule pods
// onto hosts.
type Scheduler interface {

	//
	ScheduleHost(
		rep *SchedulePodReplica,
		hostls *ScheduleHostList,
		opts *ScheduleOptions,
	) (
		hit *ScheduleHitItem,
		err error,
	)

	//
	ScheduleHostValid(
		host *ScheduleHostItem,
		entry *SchedulePodReplica,
	) (
		err error,
	)
}

type SchedulePodReplica struct {
	RepId uint64
	Cpu   int64 // mCores (1 core = 1000m)
	Mem   int64 // Bytes
	Vol   int64 // Bytes
}

type ScheduleHostList struct {
	Hosts []*ScheduleHostItem `json:"hosts,omitempty" toml:"hosts,omitempty"`
}

type ScheduleHostVolume struct {
	Name string `json:"name" toml:"name"`

	Total int64 `json:"total" toml:"total"` // Bytes
	Used  int64 `json:"used" toml:"used"`   // Bytes
	Alloc int64 `json:"alloc" toml:"alloc"` // Bytes

	// Attrs uint64 `json:"attrs" toml:"attrs"`
}

type ScheduleHostItem struct {
	Id string `json:"id" toml:"id"`

	OpAction []string `json:"op_action,omitempty" toml:"op_action,omitempty"`

	CpuTotal int64 `json:"cpu_total,omitempty" toml:"cpu_total,omitempty"` // mCores (1 core = 1000m)
	CpuUsed  int64 `json:"cpu_used,omitempty" toml:"cpu_used,omitempty"`   // mCores (1 core = 1000m)
	CpuAlloc int64 `json:"cpu_alloc,omitempty" toml:"cpu_alloc,omitempty"` // mCores (1 core = 1000m)

	MemTotal int64 `json:"mem_total,omitempty" toml:"mem_total,omitempty"` // Bytes
	MemUsed  int64 `json:"mem_used,omitempty" toml:"mem_used,omitempty"`   // Bytes
	MemAlloc int64 `json:"mem_alloc,omitempty" toml:"mem_alloc,omitempty"` // Bytes

	Volumes []*ScheduleHostVolume `json:"volumes" toml:"volumes"`
}

type ScheduleOptions struct {
	HostExcludes []string
}

type ScheduleHitVolume struct {
	Name string `json:"name" toml:"name"`
	Size int64  `json:"size" toml:"size"` // Bytes
}

type ScheduleHitItem struct {
	HostId  string
	Volumes []*ScheduleHitVolume
	Host    *ScheduleHostItem
}
