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

package injob

import (
	"sort"
	"sync"
)

const (
	StatusOK      uint64 = 1 << 1
	StatusER      uint64 = 1 << 2
	jobExecLogMax        = 10
)

type DaemonStatus struct {
	Jobs []*JobStatus `json:"jobs" toml:"jobs"`
}

type JobExecLog struct {
	Created int64  `json:"created" toml:"created"`
	Updated int64  `json:"updated" toml:"updated"`
	Status  uint64 `json:"status" toml:"status"`
	Message string `json:"message,omitempty" toml:"message,omitempty"`
}

type JobStatus struct {
	mu           sync.RWMutex
	ExecNum      int64         `json:"exec_num" toml:"exec_num"`
	NextExecTime int64         `json:"next_exec_time" toml:"next_exec_time"`
	ExecLogs     []*JobExecLog `json:"exec_logs" toml:"exec_logs"`
}

func newJobExecLog() *JobExecLog {
	return &JobExecLog{
		Created: timenow(),
	}
}

func (it *JobExecLog) OK(args ...string) *JobExecLog {
	it.Status = StatusOK
	if len(args) == 1 {
		it.Message = args[0]
	}
	return it
}

func (it *JobExecLog) ER(msg string) *JobExecLog {
	it.Status = StatusER
	it.Message = msg
	return it
}

func (it *JobStatus) LogSync(log *JobExecLog) {
	it.mu.Lock()
	defer it.mu.Unlock()
	it.ExecNum++
	log.Updated = timenow()
	it.ExecLogs = append(it.ExecLogs, log)
	sort.Slice(it.ExecLogs, func(i, j int) bool {
		return it.ExecLogs[i].Created < it.ExecLogs[j].Created
	})
	if len(it.ExecLogs) > jobExecLogMax {
		it.ExecLogs = it.ExecLogs[len(it.ExecLogs)-jobExecLogMax:]
	}
}

func (it *JobStatus) LastLog() *JobExecLog {
	it.mu.RLock()
	defer it.mu.RUnlock()
	if len(it.ExecLogs) > 0 {
		return it.ExecLogs[len(it.ExecLogs)-1]
	}
	return nil
}
