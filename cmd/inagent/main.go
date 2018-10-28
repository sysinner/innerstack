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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strings"
	"syscall"

	"github.com/hooto/hlog4g/hlog"
	"github.com/hooto/httpsrv"
	"github.com/hooto/httpsrv/deps/go.net/websocket"

	"github.com/sysinner/incore/config"
	"github.com/sysinner/incore/inagent/cmd/confrender"
	"github.com/sysinner/incore/inagent/executor"
	"github.com/sysinner/incore/inagent/v1"
	"github.com/sysinner/incore/inapi"
)

const (
	addr_sock = "/home/action/.sysinner/inagent.sock"
)

var (
	version = "0.3.0.alpha"
	release = ""
	pod_id  = ""
)

func main() {

	runtime.GOMAXPROCS(1)

	action := "agent"
	if len(os.Args) > 1 {
		action = os.Args[1]
	}

	switch action {

	case "confrender":
		if err := confrender.ActionConfig(); err != nil {
			fmt.Println("cmd error :", err)
			os.Exit(1)
		}

	case "agent":
		action_agent()

	default:
		fmt.Println("invalid command")
		os.Exit(1)
	}
}

func action_agent() {

	//
	pod_id = strings.TrimSpace(os.Getenv("POD_ID"))
	if !inapi.PodIdReg.MatchString(pod_id) {
		os.Exit(1)
	}

	//
	init_dirs := []string{
		"/home/action/local/bin",
		"/home/action/local/share",
		"/home/action/local/profile.d",
		"/home/action/var/tmp",
		"/home/action/var/log",
		"/home/action/.ssh",
	}
	for _, v := range init_dirs {
		os.MkdirAll(v, 0755)
	}

	//
	if _, err := user.Lookup(config.User.Username); err != nil {

		nologin, err := exec.LookPath("nologin")
		if err != nil {
			nologin = "/sbin/nologin"
		}

		if _, err = exec.Command(
			"/usr/sbin/useradd",
			"-d", "/home/action",
			"-s", nologin,
			"-u", config.User.Uid, config.User.Username,
		).Output(); err != nil {
			os.Exit(1)
		}
	}

	//
	syscall.Setgid(2048)
	syscall.Setuid(2048)
	syscall.Chdir("/home/action")

	//
	hlog.LogDirSet("/home/action/var/log")
	hlog.Printf("info", "started")

	//
	go executor.Runner("/home/action")

	//
	httpsrv.GlobalService.Config.HttpAddr = "unix:" + addr_sock

	httpsrv.GlobalService.HandlerRegister(
		"/in/v1/pb/termws",
		websocket.Handler(v1.TerminalWsOpenAction))

	httpsrv.GlobalService.ModuleRegister("/in/v1/", v1.NewModule())

	httpsrv.GlobalService.Start()

	select {}
}
