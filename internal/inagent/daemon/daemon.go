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

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
	"github.com/sysinner/incore/v2/internal/inagent/task"
	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/pkg/inlog"
	"github.com/sysinner/incore/v2/pkg/signals"
)

var (
	hostId        = ""
	appId         = ""
	repId  uint32 = 0
)

type agentDaemonCommand struct {
	cmd  *cobra.Command
	args struct {
		Action string
	}
}

func NewAgentDaemonCommand() *cobra.Command {

	c := &agentDaemonCommand{
		cmd: &cobra.Command{
			Use:   "daemon",
			Short: "run inagent in daemon mode",
		},
	}

	c.cmd.RunE = c.run

	return c.cmd
}

func (it *agentDaemonCommand) run(cmd *cobra.Command, args []string) error {

	{
		fp, err := os.OpenFile("/home/action/inagent.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			panic(err)
		}
		defer fp.Close()

		inlog.Setup(fp)
	}

	hostId = strings.TrimSpace(os.Getenv("APP_HOST_ID"))
	if !inapi.ObjectIdValid.MatchString(hostId) {
		return errors.New("ENV APP_HOST_ID Not Match")
	}

	//
	appId = strings.TrimSpace(os.Getenv("APP_ID"))
	if !inapi.ObjectIdValid.MatchString(appId) {
		return errors.New("ENV APP_ID Not Match")
	}

	if os.Getenv("APP_REP_ID") == "" {
		return errors.New("ENV APP_REP_ID Not Set")
	}
	if v, err := strconv.ParseInt(os.Getenv("APP_REP_ID"), 10, 32); err != nil ||
		v < 0 || v >= 256 {
		return errors.New("ENV APP_REP_ID Not Valid")
	} else {
		repId = uint32(v)
	}

	// Create init directories using hostapi.InitDirs
	for _, v := range hostapi.InitDirs {
		if err := os.MkdirAll(v, 0755); err != nil {
			return err
		}
	}

	// if false {
	// 	//
	// 	if _, err := user.Lookup(config.User.Username); err != nil {
	// 		if _, err = exec.Command(
	// 			"/usr/sbin/useradd",
	// 			"-d", "/home/action",
	// 			"-s", "/bin/bash",
	// 			"-u", config.User.Uid, config.User.Username,
	// 		).Output(); err != nil {
	// 			return err
	// 		}
	// 	}

	// 	//
	// 	syscall.Setgid(config.DefaultGroupID)
	// 	syscall.Setuid(config.DefaultUserID)
	// }
	syscall.Chdir("/home/action")

	slog.Info("inagent daemon started")

	signals.Go(func() {
		tr := time.NewTimer(10e6)
		// Periodically reap orphaned zombie processes (PID 1 responsibility)
		orphanReaper := time.NewTicker(1e9)
		defer tr.Stop()
		defer orphanReaper.Stop()
		for {
			select {
			case <-signals.Done():
				if err := task.Kill(); err != nil {
					slog.Warn(fmt.Sprintf("task [*] kill failed, err %s", err.Error()))
				}
				task.ReapOrphans()

			case <-orphanReaper.C:
				task.ReapOrphans()

			case <-tr.C:
				if app, dur, ok := specRefresh(); ok {
					if err := task.Run(app); err != nil {
						slog.Error(fmt.Sprintf("task [*] run failed, err %s", err.Error()))
					}
					tr.Reset(dur)
				} else {
					tr.Reset(10e9)
				}
			}
		}
	}, nil)

	signals.Wait()

	return nil
}

var (
	app inapi.AppReplicaInstance
)

func specRefresh() (*inapi.AppReplicaInstance, time.Duration, bool) {

	dur := time.Duration(10e9)

	fp, err := os.Open(hostapi.AppReplicaFile)
	if err != nil {
		slog.Error(fmt.Sprintf("failed to open app instance file, err %s", err.Error()))
		return nil, dur, false
	}
	defer fp.Close()

	if err = json.NewDecoder(fp).Decode(&app); err != nil {
		slog.Error(fmt.Sprintf("failed to decode app instance, err %s", err.Error()))
		return nil, dur, false
	}

	if app.App == nil || app.App.Spec == nil ||
		app.App.Spec.Resources == nil ||
		app.App.Deploy == nil {
		slog.Error("app or spec/operate is nil in app instance config")
		return nil, dur, false
	}

	for _, v := range app.App.Deploy.Replicas {
		if v.HostId != hostId || v.Id != repId {
			continue
		}
		app.Replica = v
		break
	}

	if app.Replica == nil {
		slog.Error("replica not found in app instance config")
		return nil, dur, false
	}

	for _, t := range app.App.Spec.Tasks {
		if t.Cron != "" {
			if sched, err := task.CronParse(t.Cron); err == nil {
				dur = max(1e9, time.Since(sched.Next(time.Now())).Abs()/2)
			}
		}
	}

	return &app, dur, true
}
