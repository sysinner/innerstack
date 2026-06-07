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
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/pkg/inconf"
)

const minRetryIntervalSeconds int64 = 10

var (
	appReplicaInstance *inapi.AppReplicaInstance
	execStatuses       = map[string]*executorStatus{}
)

func dependAllow(task *inapi.AppSpecTask) bool {
	for _, name := range task.DependsOn {
		if es, ok := execStatuses[name]; !ok || (es.DoneUpdated < es.ExecWindow) {
			return false
		}
	}
	return true
}

func Kill() error {
	if appReplicaInstance == nil || len(appReplicaInstance.App.Spec.Tasks) == 0 {
		return nil
	}

	params := inconf.VarParams(appReplicaInstance)

	for _, task := range appReplicaInstance.App.Spec.Tasks {

		if es, ok := execStatuses[task.Name]; ok {
			if es.Cmd != nil && es.Cmd.Process != nil {
				es.Cmd.Process.Kill()
				es.Cmd = nil
				slog.Info(fmt.Sprintf("task [%s] kill", task.Name))
				time.Sleep(10e6)
			}
		}
	}

	for _, task := range appReplicaInstance.App.Spec.Tasks {

		if !task.GetOnShutdown() {
			continue
		}

		if err := taskSyncAction(task, params); err != nil {
			slog.Info(fmt.Sprintf("task [%s] kill failed, err %s", task.Name, err.Error()))
		} else {
			slog.Info(fmt.Sprintf("task [%s] kill ok", task.Name))
		}
	}

	return nil
}

func Run(app *inapi.AppReplicaInstance) error {

	if len(app.App.Spec.Tasks) == 0 {
		return nil
	}

	appReplicaInstance = app

	var (
		params map[string]string

		tn = time.Now().Unix()
	)

	for _, task := range app.App.Spec.Tasks {

		if task.GetOnShutdown() {
			continue
		}

		es, ok := execStatuses[task.Name]
		if !ok {
			es = &executorStatus{
				Updated: time.Now().Unix(),
			}
			if task.GetOnStartup() ||
				task.GetIntervalSeconds() > 0 {
				es.ExecWindow = tn
			}
			execStatuses[task.Name] = es
		}

		switch {
		case task.GetOnStartup():

			if es.ExecWindow > 0 &&
				es.DoneUpdated == 0 &&
				es.FailUpdated >= es.ExecWindow {
				// fail and retry
				es.ExecWindow = max(es.ExecWindow+minRetryIntervalSeconds, tn)
			}

		case task.GetIntervalSeconds() > 0:
			lastUpdated := max(es.DoneUpdated, es.FailUpdated)
			if lastUpdated+task.GetIntervalSeconds() <= tn {
				es.ExecWindow = max(es.ExecWindow+minRetryIntervalSeconds, tn)
			}

		case task.Cron != "":
			lastUpdated := max(es.DoneUpdated, es.FailUpdated)
			if es.ExecWindow == 0 || es.ExecWindow <= lastUpdated {
				if sched, err := CronParse(task.Cron); err == nil {
					t := sched.Next(time.Now())
					es.ExecWindow = max(t.Unix(), lastUpdated)
				} else {
					slog.Warn(fmt.Sprintf("task [%s] parse faild, err %s", task.Name, err.Error()))
				}
			}

		default:
			continue
		}

		if es.ExecWindow == 0 || es.ExecWindow > tn {
			continue
		}

		if es.DoneUpdated >= es.ExecWindow {
			continue
		}

		if !dependAllow(task) {
			continue
		}

		if params == nil {
			params = inconf.VarParams(app)
		}

		if err := taskAsyncAction(task, es, params); err != nil {
			es.DoneUpdated = 0
			es.FailUpdated = max(time.Now().Unix(), es.ExecWindow)
			slog.Info(fmt.Sprintf("task [%s] stats, msg %s", task.Name, err.Error()))
		}

		time.Sleep(10e6)
	}

	return nil
}

func taskSyncAction(
	task *inapi.AppSpecTask,
	dms map[string]string,
) error {

	script := strings.TrimSpace(task.Script)
	if script == "" {
		return nil
	}

	//
	script = inconf.RenderWithExpand(script, dms)

	cmd := exec.Command("sh")

	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	// setup stdout/stderr capture using outputBuf
	outputBuf := make([]byte, 0, 4096)
	outputWriter := &outputBuffer{buf: &outputBuf}
	cmd.Stdout = outputWriter
	cmd.Stderr = outputWriter

	// Set working directory, default to HomeDir(/home/action)
	if task.WorkingDir == "" {
		task.WorkingDir = hostapi.HomeDir
	}
	cmd.Dir = task.WorkingDir

	// Set user credentials: only support "root" and "action" (default)
	// - root: uid=0, gid=0
	// - action (default): uid=2048, gid=2048 (/home/action)
	uid := config.DefaultUserID
	gid := config.DefaultGroupID
	if task.RunUser == "root" {
		uid = 0
		gid = 0
	} else if task.RunUser != "" && task.RunUser != "action" {
		slog.Warn(fmt.Sprintf("task user invalid (%s), using default 'action'", task.RunUser))
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}

	in.Write([]byte("set -e\n" + script + "\nexit\n"))
	in.Close()

	if err := cmd.Start(); err != nil {
		return err
	}

	go cmd.Wait()

	ttl := time.Now().UnixMilli() + 5000

	for {
		time.Sleep(10e6)

		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {

			if cmd.ProcessState.Success() {
				slog.Info(fmt.Sprintf("task [%s] success", task.Name))
			} else {
				slog.Error(fmt.Sprintf("task [%s] failed, err %s", task.Name, string(outputBuf)))
			}

			break
		}

		if time.Now().UnixMilli() > ttl {
			break
		}
	}

	return nil
}

func taskAsyncAction(
	task *inapi.AppSpecTask,
	es *executorStatus,
	dms map[string]string,
) error {

	if es.DoneUpdated >= es.ExecWindow {
		return nil
	}

	tn := time.Now().Unix()

	//
	if es.Cmd != nil {

		if es.Updated+60 < tn {
			duration := time.Duration(tn-es.ExecWindow) * time.Second
			slog.Info(fmt.Sprintf("task [%s] running, duration %s", task.Name, duration))
			es.Updated = tn
		}

		return nil
	}

	// TODO: Interval-based and cron-based scheduling with retry logic
	// This section is reserved for future implementation of:
	// - interval_seconds trigger
	// - cron trigger
	// - max_attempts and retry_backoff_seconds

	//
	script := strings.TrimSpace(task.Script)
	if len(script) == 0 {
		return nil
	}

	//
	es.State = execRunning
	es.Updated = tn

	//
	script = inconf.RenderWithExpand(script, dms)

	slog.Info("inagent/exec run",
		"name", task.Name,
		"user", task.RunUser,
		"script", script)

	//
	if err := taskCmd(task, es, script); err != nil {
		slog.Error(fmt.Sprintf("task [%s] CMD, err %s", task.Name, err.Error()))
		return err
	}

	return nil
}

func taskCmd(task *inapi.AppSpecTask, es *executorStatus, script string) error {

	if es.Cmd != nil && es.Cmd.Process != nil {
		es.Cmd.Process.Kill()
		time.Sleep(10e6)
	}

	es.Cmd = exec.Command("sh")

	in, err := es.Cmd.StdinPipe()
	if err != nil {
		es.Cmd = nil
		return err
	}

	// setup stdout/stderr capture using OutputBuf
	es.OutputBuf = make([]byte, 0, 4096)
	outputWriter := &outputBuffer{buf: &es.OutputBuf}
	es.Cmd.Stdout = outputWriter
	es.Cmd.Stderr = outputWriter

	// Set working directory, default to HomeDir(/home/action)
	if task.WorkingDir == "" {
		task.WorkingDir = hostapi.HomeDir
	}
	es.Cmd.Dir = task.WorkingDir

	// Set user credentials: only support "root" and "action" (default)
	// - root: uid=0, gid=0
	// - action (default): uid=2048, gid=2048 (/home/action)
	uid := config.DefaultUserID
	gid := config.DefaultGroupID
	if task.RunUser == "root" {
		uid = 0
		gid = 0
	} else if task.RunUser != "" && task.RunUser != "action" {
		slog.Warn(fmt.Sprintf("task [%s] user invalid (%s), using default 'action'", task.Name, task.RunUser))
	}
	es.Cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}

	in.Write([]byte("set -e\nset -o pipefail\n" + script + "\nexit\n"))
	in.Close()

	execStarted := time.Now()

	if err := es.Cmd.Start(); err != nil {
		es.Cmd = nil
		return err
	}

	go func() {

		es.Cmd.Wait()

		if es.Cmd.ProcessState != nil && es.Cmd.ProcessState.Exited() {

			es.State = execExited

			if es.Cmd.ProcessState.Success() {
				es.DoneUpdated = max(time.Now().Unix(), es.ExecWindow)
				es.FailUpdated = 0
				slog.Info(fmt.Sprintf("task [%s] success, duration %v",
					task.Name, time.Since(execStarted)))
			} else {
				// capture output from buffer
				es.Output = strings.TrimSpace(string(es.OutputBuf))
				if es.Output != "" {
					es.FailMessage = fmt.Sprintf("process error %s, output: %s",
						es.Cmd.ProcessState.String(), es.Output)
				} else {
					es.FailMessage = "process error " + es.Cmd.ProcessState.String()
				}
				es.DoneUpdated = 0
				es.FailUpdated = max(time.Now().Unix(), es.ExecWindow)
				slog.Error(fmt.Sprintf("task [%s] failed, duration %v, err %s, script %s",
					task.Name, time.Since(execStarted), es.FailMessage, script))
			}

			es.Cmd = nil
			es.Updated = time.Now().Unix()
		}

		// slog.Warn(fmt.Sprintf("task [%s] exited in %v", task.Name, time.Since(execStarted)))
	}()

	return nil
}

// outputBuffer is a thread-safe writer that appends to a byte slice
type outputBuffer struct {
	buf *[]byte
}

func (ob *outputBuffer) Write(p []byte) (n int, err error) {
	*ob.buf = append(*ob.buf, p...)
	return len(p), nil
}
