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

	"github.com/hooto/hlang4g/hlang"
	"github.com/hooto/hlog4g/hlog"
	"github.com/hooto/httpsrv"
	"github.com/lessos/lessgo/crypto/idhash"

	iam_cfg "github.com/hooto/iam/config"
	iam_cli "github.com/hooto/iam/iamclient"
	iam_db "github.com/hooto/iam/store"
	iam_web "github.com/hooto/iam/websrv/ctrl"
	iam_v1 "github.com/hooto/iam/websrv/v1"

	ip_cfg "github.com/sysinner/inpack/server/config"
	ip_db "github.com/sysinner/inpack/server/data"
	ip_p1 "github.com/sysinner/inpack/websrv/p1"
	ip_v1 "github.com/sysinner/inpack/websrv/v1"

	ic_ws_cp "github.com/sysinner/incore/websrv/cp"
	ic_ws_op "github.com/sysinner/incore/websrv/ops"
	ic_ws_p1 "github.com/sysinner/incore/websrv/p1"
	ic_ws_v1 "github.com/sysinner/incore/websrv/v1"
	ic_ws_ui "github.com/sysinner/inpanel"

	ic_cfg "github.com/sysinner/incore/config"
	ic_db "github.com/sysinner/incore/data"
	ic_host "github.com/sysinner/incore/hostlet"
	ic_api "github.com/sysinner/incore/inapi"
	ic_rpc "github.com/sysinner/incore/rpcsrv"
	ic_sts "github.com/sysinner/incore/status"
	ic_ver "github.com/sysinner/incore/version"
	ic_zm "github.com/sysinner/incore/zonemaster"

	is_cfg "github.com/sysinner/innerstack/config"
)

var (
	version  = "git"
	release  = "1"
	released = ""
	err      error
)

func main() {

	hs := httpsrv.NewService()
	hs.Config.TemplateFuncRegister("T", hlang.StdLangFeed.Translate)

	// initialize configuration
	for {

		if err = ic_cfg.Setup(); err != nil {
			hlog.Printf("warn", "incore/config/Setup error: %s", err.Error())
			time.Sleep(3e9)
			continue
		}

		if err = is_cfg.Setup(version, release, ic_cfg.Config.Host.SecretKey, ic_cfg.IsZoneMaster()); err != nil {
			hlog.Printf("warn", "innerstask/config/Setup error: %s", err.Error())
			time.Sleep(3e9)
			continue
		}

		hlog.Printf("info", "Config Setup OK")
		break
	}

	// initialize status
	{
		if err = ic_sts.Init(); err != nil {
			log.Fatalf("status.Init error: %s", err.Error())
		}
	}

	{
		hlog.Printf("info", "InnerStack %s-%s", version, release)
		hlog.Printf("info", "inCore %s", ic_ver.Version)

		if ic_cfg.IsZoneMaster() {

			hlog.Printf("info", "inPack %s", ip_cfg.Version)
			hlog.Printf("info", "inPanel %s", ic_ws_ui.Version)

			ic_ws_ui.VersionHash = idhash.HashToHexString([]byte(
				(version + release + released)), 16)
			ic_ws_ui.ZoneId = ic_cfg.Config.Host.ZoneId

			if ic_cfg.Config.ZoneMaster != nil &&
				ic_cfg.Config.ZoneMaster.MultiHostEnable {
				ic_ws_ui.OpsClusterHost = true
				if ic_cfg.Config.ZoneMaster.MultiCellEnable {
					ic_ws_ui.OpsClusterCell = true
					if ic_cfg.Config.ZoneMaster.MultiZoneEnable {
						ic_ws_ui.OpsClusterZone = true
					}
				}
			}
		}
	}

	// initialize data/io connection
	{
		if err := ic_db.Setup(); err != nil {
			time.Sleep(1e9)
			log.Fatalf("ic_db setup err %s", err.Error())
		}
	}

	// module/IAM
	if ic_cfg.IsZoneMaster() {

		if ic_cfg.Config.IamService == nil {
			ic_cfg.Config.IamService = &iam_cfg.ConfigCommon{}
		}

		//
		if err := iam_cfg.SetupConfig(
			ic_cfg.Prefix+"/vendor/github.com/hooto/iam",
			ic_cfg.Config.IamService,
		); err != nil {
			log.Fatalf("iam_cfg.InitConfig error: %s", err.Error())
		}

		iam_cfg.Config.InstanceID = "00" + idhash.HashToHexString([]byte("innerstack/iam"), 14)
		iam_cfg.VersionHash = idhash.HashToHexString([]byte(
			(iam_cfg.Config.InstanceID + iam_cfg.Version + released)), 16)

		// init database
		iam_db.Data = ic_db.DataGlobal
		if err := iam_db.Init(); err != nil {
			log.Fatalf("iam.Store.Init error: %s", err.Error())
		}
		if err := iam_db.InitData(); err != nil {
			log.Fatalf("iam.Store.InitData error: %s", err.Error())
		}
		iam_db.SysConfigRefresh()

		//
		hs.ModuleRegister("/iam/v1", iam_v1.NewModule())
		hs.ModuleRegister("/iam", iam_web.NewModule())

		//
		if aks := is_cfg.InitIamAccessKeyData(); len(aks) > 0 {
			for _, v := range aks {
				iam_db.AccessKeyInitData(v)
			}
		}
	}
	{
		//
		iam_cli.ServiceUrl = fmt.Sprintf(
			"http://%s:%d/iam",
			ic_cfg.Config.Host.LanAddr.IP(),
			ic_cfg.Config.Host.HttpPort,
		)
		if ic_cfg.Config.IamServiceUrlFrontend == "" {
			if ic_cfg.Config.Host.WanAddr.IP() != "" {
				iam_cli.ServiceUrlFrontend = fmt.Sprintf(
					"http://%s:%d/iam",
					ic_cfg.Config.Host.WanAddr.IP(),
					ic_cfg.Config.Host.HttpPort,
				)
			} else {
				iam_cli.ServiceUrlFrontend = iam_cli.ServiceUrl
			}
		} else {
			iam_cli.ServiceUrlFrontend = ic_cfg.Config.IamServiceUrlFrontend
		}

		if ic_cfg.Config.IamServiceUrlGlobal != "" {
			iam_cli.ServiceUrlGlobal = ic_cfg.Config.IamServiceUrlGlobal
		}

		hlog.Printf("info", "IAM ServiceUrl %s", iam_cli.ServiceUrl)
		hlog.Printf("info", "IAM ServiceUrlFrontend %s", iam_cli.ServiceUrlFrontend)

		if ic_cfg.Config.IamServiceUrlGlobal != "" {
			iam_cli.ServiceUrlGlobal = ic_cfg.Config.IamServiceUrlGlobal
			hlog.Printf("info", "IAM ServiceUrlGlobal %s", iam_cli.ServiceUrlGlobal)
		}
	}

	// module/IPS: init ips database and webserver
	if ic_cfg.IsZoneMaster() {

		if err = ip_cfg.Setup(ic_cfg.Prefix); err != nil {
			log.Fatalf("ips.Config.Init error: %s", err.Error())
		}

		// init inpack database
		if err = ip_db.Setup(); err != nil {
			log.Fatalf("ip_db setup failed:%s", err.Error())
		}
		ic_db.DataInpack = ip_db.Data

		// TODEL
		ip_cfg.Config.Sync()

		if err := iam_db.AppInstanceRegister(ip_cfg.IamAppInstance()); err != nil {
			log.Fatalf("ips.Data.Init error: %s", err.Error())
		}

		hs.ModuleRegister("/ips/v1", ip_v1.NewModule())
		hs.ModuleRegister("/ips/p1", ip_p1.NewModule())
		hs.ModuleRegister("/in/cp/ips/~", httpsrv.NewStaticModule("ip_ui", ic_cfg.Prefix+"/webui/ips"))

		// TODO
		ic_cfg.Config.InpackServiceUrl = fmt.Sprintf(
			"http://%s:%d/",
			ic_cfg.Config.Host.LanAddr.IP(),
			ic_cfg.Config.Host.HttpPort,
		)

		//
		if aks := ip_cfg.InitIamAccessKeyData(); len(aks) > 0 {
			for _, v := range aks {
				iam_db.AccessKeyInitData(v)
			}
		}
	}

	// module/hchart
	if ic_cfg.IsZoneMaster() {
		hs.ModuleRegister("/in/cp/hchart/~", httpsrv.NewStaticModule("hchart_ui", ic_cfg.Prefix+"/webui/hchart/webui"))
		hs.ModuleRegister("/in/ops/hchart/~", httpsrv.NewStaticModule("hchart_ui_ops", ic_cfg.Prefix+"/webui/hchart/webui"))
	}

	// incore
	if ic_cfg.IsZoneMaster() {

		ic_inst := is_cfg.IamAppInstance()
		if err := iam_db.AppInstanceRegister(ic_inst); err != nil {
			log.Fatalf("in.Data.Init error: %s", err.Error())
		}
		ic_cfg.Config.InstanceId = ic_inst.Meta.ID
		ic_cfg.Config.Sync()

		hs.HandlerFuncRegister("/in/v1/pb/termws", ic_ws_v1.PodBoundTerminalWsHandlerFunc)

		// Frontend APIs for Users
		hs.ModuleRegister("/in/v1", ic_ws_v1.NewModule())

		// Frontend UI for Users
		hlang.StdLangFeed.LoadMessages(ic_cfg.Prefix+"/i18n/en.json", true)
		hlang.StdLangFeed.LoadMessages(ic_cfg.Prefix+"/i18n/zh-CN.json", true)

		hs.Config.TemplateFuncRegister("T", hlang.StdLangFeed.Translate)

		ic_ws_m := ic_ws_cp.NewModule()
		ic_ws_m.ControllerRegister(new(hlang.Langsrv))

		hs.ModuleRegister("/in/cp", ic_ws_m)

		// Backend Operating APIs/UI for System Operators
		hs.ModuleRegister("/in/ops", ic_ws_op.NewModule())

		// Frontend UI Index
		hs.ModuleRegister("/in/p1", ic_ws_p1.NewModule())
		hs.ModuleRegister("/in", ic_ws_cp.NewIndexModule())
	}

	// init zonemaster
	if ic_cfg.IsZoneMaster() {

		if err := ic_zm.InitData(is_cfg.InitZoneMasterData()); err != nil {
			log.Fatalf("ic_zm.InitData err %s", err.Error())
		}

		if err := ic_zm.SetupScheduler(); err != nil {
			log.Fatalf("ic_zm.SetupScheduler err %s", err.Error())
		}

		ic_api.RegisterApiZoneMasterServer(ic_rpc.Server, new(ic_zm.ApiZoneMaster))

		if err := ic_zm.Start(); err != nil {
			log.Fatalf("ic_zm.Start err %s", err.Error())
		}
	}

	//
	{
		if err := ic_host.Start(); err != nil {
			log.Fatalf("ic_host.Start err %s", err.Error())
		}
	}

	// hostlet
	{
		ic_api.RegisterApiHostMemberServer(ic_rpc.Server, new(ic_host.ApiHostMember))
	}

	//
	if err := ic_rpc.Start(ic_cfg.Config.Host.LanAddr.Port()); err != nil {
		log.Fatalf("rpc.server.Start error: %v", err)
	} else {
		hlog.Printf("info", "rpc/server bind :%d", ic_cfg.Config.Host.LanAddr.Port())
	}

	// http service
	if ic_cfg.IsZoneMaster() {
		hs.Config.HttpPort = ic_cfg.Config.Host.HttpPort
		go hs.Start()
	}

	if ic_cfg.Config.PprofHttpPort > 0 {
		go http.ListenAndServe(fmt.Sprintf(":%d", ic_cfg.Config.PprofHttpPort), nil)
		hlog.Printf("info", "pprof/server bind :%d", ic_cfg.Config.PprofHttpPort)
	}

	ic_cfg.Config.Sync()

	quit := make(chan os.Signal, 2)

	//
	signal.Notify(quit,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGKILL)
	sg := <-quit

	hlog.Printf("warn", "Signal Quit: %s", sg.String())
	hlog.Flush()
}
