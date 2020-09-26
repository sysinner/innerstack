// Copyright 2015 Authors, All rights reserved.
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
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hooto/hlog4g/hlog"
	"github.com/hooto/httpsrv"

	incfg "github.com/sysinner/incore/config"
	indb "github.com/sysinner/incore/data"
	inhost "github.com/sysinner/incore/hostlet"
	"github.com/sysinner/incore/inapi"
	"github.com/sysinner/incore/injob"
	"github.com/sysinner/incore/inrpc"
	instatus "github.com/sysinner/incore/status"
	"github.com/sysinner/incore/websrv/o1"
	inzone "github.com/sysinner/incore/zonemaster"

	is_cfg "github.com/sysinner/innerstack/config"
)

var (
	version   = "git"
	release   = "1"
	released  = ""
	err       error
	jobDaemon *injob.Daemon
)

func main() {

	if err = incfg.BasicSetup(); err != nil {
		log.Fatalf("incore/config init error %s", err.Error())
	}

	{
		ohs := httpsrv.NewService()
		ohs.ModuleRegister("/in/o1", o1.NewModule())
		ohs.Config.HttpAddr = fmt.Sprintf("unix:%s/var/%s.sock", incfg.Prefix, "server")
		go ohs.Start()
	}

	// rpc init
	{
		inrpc.RegisterServer(func(s *inrpc.RpcServer) {
			inapi.RegisterApiHostMemberServer(s, new(inhost.ApiHostMember))
			inapi.RegisterApiZoneMasterServer(s, new(inzone.ApiZoneMaster))
		})
	}

	// configuration
	for i := 0; ; i++ {

		if i > 0 {
			time.Sleep(3e9)
		}

		//
		if len(incfg.Config.Zone.MainNodes) == 0 {
			hlog.Printf("warn", "waiting initialization")
			continue
		}

		// initialize status
		if err = instatus.Setup(); err != nil {
			hlog.Printf("warn", "incore/status setup err %s", err.Error())
			continue
		}

		//
		if err := indb.Setup(); err != nil {
			hlog.Printf("warn", "incore/data setup err %s", err.Error())
			continue
		}

		//
		{
			rpcPort := inapi.HostNodeAddress(incfg.Config.Host.LanAddr).Port()
			if err := inrpc.Start(rpcPort); err != nil {
				hlog.Printf("warn", "inrpc/server bind 0.0.0.0:%d err %s", rpcPort, err.Error())
				continue
			}
			hlog.Printf("info", "inrpc/server bind 0.0.0.0:%d ok", rpcPort)
		}

		if err = is_cfg.Setup(version, release, incfg.Config.Host.SecretKey, incfg.IsZoneMaster()); err != nil {
			hlog.Printf("warn", "innerstask/config/Setup error: %s", err.Error())
			continue
		}

		hlog.Printf("info", "Config Setup OK")
		break
	}

	{
		jobDaemon, _ = injob.NewDaemon(instatus.JobContextRefresh)

		jobDaemon.Commit(incfg.NewConfigJob())
		jobDaemon.Commit(indb.NewDataJob())

		//
		jobDaemon.Commit(inhost.NewHostletJob())

		//
		jobDaemon.Commit(inzone.NewZoneMainJob())

		go jobDaemon.Start()
	}

	if incfg.Config.Host.PprofHttpPort > 0 {
		go http.ListenAndServe(fmt.Sprintf(":%d", incfg.Config.Host.PprofHttpPort), nil)
		hlog.Printf("info", "pprof/server bind :%d", incfg.Config.Host.PprofHttpPort)
	}

	incfg.Config.Flush()

	quit := make(chan os.Signal, 2)

	//
	signal.Notify(quit,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGKILL)
	sg := <-quit

	indb.Close()

	hlog.Printf("warn", "Signal Quit: %s", sg.String())
	hlog.Flush()
}
