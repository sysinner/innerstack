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

package daemon

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/hostlet/hostapi"
	"github.com/sysinner/incore/v2/internal/inagent/executor"
	"github.com/sysinner/incore/v2/pkg/inlog"
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

	inlog.Setup()

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

	if false {
		//
		if _, err := user.Lookup(config.User.Username); err != nil {
			if _, err = exec.Command(
				"/usr/sbin/useradd",
				"-d", "/home/action",
				"-s", "/bin/bash",
				"-u", config.User.Uid, config.User.Username,
			).Output(); err != nil {
				return err
			}
		}

		//
		syscall.Setgid(config.DefaultGroupID)
		syscall.Setuid(config.DefaultUserID)
	}
	syscall.Chdir("/home/action")

	slog.Info("inagent/daemon started")

	worker()
	return nil
}

func worker() {

	for {
		workerEntry()
		time.Sleep(10 * time.Second)
	}
}

func workerEntry() {

	var app hostapi.AppReplicaInstance

	f, err := os.Open(hostapi.AppInstanceFile)
	if err != nil {
		slog.Error("failed to open app instance file", "error", err)
		return
	}
	defer f.Close()

	if err = json.NewDecoder(f).Decode(&app); err != nil {
		slog.Error("failed to decode app instance", "error", err)
		return
	}

	if app.App == nil || app.App.Spec == nil ||
		app.App.Spec.Resources == nil ||
		app.App.Operate == nil {
		slog.Error("app or spec/operate is nil in app instance config")
		return
	}

	for _, v := range app.App.Operate.Replicas {
		if v.HostId != hostId || v.Id != repId {
			continue
		}
		app.Replica = v
		break
	}

	if app.Replica == nil {
		slog.Error("replica not found in app instance config")
		return
	}

	if err = executor.Runner(&app, hostapi.HomeDir); err != nil {
		slog.Error("executor runner failed", "error", err)
		return
	}

}
