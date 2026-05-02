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

package task

import (
	"os/exec"

	"github.com/robfig/cron/v3"
)

var (
	execRunning = "running"
	execExited  = "existed"
)

type executorStatus struct {
	Updated int64 `json:"updated,omitempty" toml:"updated,omitempty"`

	State string

	ExecWindow int64

	DoneUpdated int64

	FailUpdated      int64
	FailMessage      string
	OnFailedRetryNum int64

	Cmd *exec.Cmd `json:"-" toml:"-"`

	// Output stores the last captured stdout/stderr output from command execution
	Output    string `json:"output,omitempty" toml:"output,omitempty"`
	OutputBuf []byte `json:"-" toml:"-"`
}

// cronParse supports auto-detection of 5 or 6 field Cron expressions
func CronParse(spec string) (cron.Schedule, error) {
	// Define 6-field parser (with seconds)
	secondParser := cron.NewParser(
		cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)

	// Try 6-field parsing
	sched, err := secondParser.Parse(spec)
	if err == nil {
		return sched, nil
	}

	// If 6-field fails, try 5-field parsing (standard)
	standardParser := cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	return standardParser.Parse(spec)
}
