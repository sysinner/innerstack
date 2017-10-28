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
	_ "expvar"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"

	"github.com/hooto/hlog4g/hlog"
	"github.com/hooto/httpsrv"
	"github.com/lessos/lessgo/crypto/idhash"
	"github.com/lynkdb/kvgo"

	iam_cfg "github.com/hooto/iam/config"
	iam_cli "github.com/hooto/iam/iamclient"
	iam_sto "github.com/hooto/iam/store"
	iam_web "github.com/hooto/iam/websrv/ctrl"
	iam_api "github.com/hooto/iam/websrv/v1"

	ips_cf "github.com/sysinner/inpack/server/config"
	ips_db "github.com/sysinner/inpack/server/data"
	ips_v1 "github.com/sysinner/inpack/websrv/v1"

	in_ws_cp "github.com/sysinner/incore/websrv/cp"
	in_ws_op "github.com/sysinner/incore/websrv/ops"
	in_ws_v1 "github.com/sysinner/incore/websrv/v1"
	in_ws_ui "github.com/sysinner/inpanel"

	in_cf "github.com/sysinner/incore/config"
	in_db "github.com/sysinner/incore/data"
	in_host "github.com/sysinner/incore/hostlet"
	in_api "github.com/sysinner/incore/inapi"
	in_rpc "github.com/sysinner/incore/rpcsrv"
	in_sched "github.com/sysinner/incore/scheduler"
	in_sts "github.com/sysinner/incore/status"
	in_zm "github.com/sysinner/incore/zonemaster"

	"github.com/sysinner/insoho/config"
)

var (
	Version  = "0.1.2.dev"
	Released = ""
	err      error
)

func main() {

	hs := httpsrv.NewService()

	// initialize configuration
	{
		if err = in_cf.Init(); err != nil {
			log.Fatalf("conf.Initialize error: %s", err.Error())
		}

		if err = config.Init(Version, in_cf.Config.Host.SecretKey); err != nil {
			log.Fatalf("conf.Initialize error: %s", err.Error())
		}
	}

	// initialize status
	{
		if err = in_sts.Init(); err != nil {
			log.Fatalf("status.Init error: %s", err.Error())
		}
	}

	{
		hlog.Printf("info", "inCore  version %s", in_cf.Version)
		hlog.Printf("info", "inPanel version %s", in_ws_ui.Version)
		hlog.Printf("info", "inPack  version %s", ips_cf.Version)
		hlog.Printf("info", "inSoho  version %s", Version)
		in_ws_ui.VersionHash = idhash.HashToHexString([]byte(
			(in_ws_ui.Version + Released)), 16)
	}

	// initialize data/io connection
	{
		// init local cache database
		opts := in_cf.Config.IoConnectors.Options("in_local_cache")
		if opts == nil {
			log.Fatalf("conf.Data No IoConnector (%s) Found", "in_local_cache")
		}

		if in_db.LocalDB, err = kvgo.Open(*opts); err != nil {
			log.Fatalf("Can Not Connect To %s, Error: %s", opts.Name, err.Error())
		}

		// init zone master database
		opts = in_cf.Config.IoConnectors.Options("in_zone_master")
		if opts == nil {
			log.Fatalf("conf.Data No IoConnector (%s) Found", "in_zone_master")
		}

		if in_db.ZoneMaster, err = kvgo.Open(*opts); err != nil {
			log.Fatalf("Can Not Connect To %s, Error: %s", opts.Name, err.Error())
		}

		in_db.HiMaster = in_db.ZoneMaster
	}

	// module/IAM
	{
		//
		iam_cfg.Prefix = in_cf.Prefix + "/vendor/github.com/hooto/iam_static"
		iam_cfg.Config.InstanceID = idhash.HashToHexString([]byte("insoho/iam"), 16)

		// init database
		iam_sto.Data = in_db.ZoneMaster
		if err := iam_sto.Init(); err != nil {
			log.Fatalf("iam.Store.Init error: %s", err.Error())
		}
		if err := iam_sto.InitData(); err != nil {
			log.Fatalf("iam.Store.InitData error: %s", err.Error())
		}

		//
		iam_cli.ServiceUrl = fmt.Sprintf(
			"http://%s:%d/iam",
			in_cf.Config.Host.LanAddr.IP(),
			in_cf.Config.Host.HttpPort,
		)
		if in_cf.Config.IamServiceUrlFrontend == "" {
			if in_cf.Config.Host.WanAddr.IP() != "" {
				iam_cli.ServiceUrlFrontend = fmt.Sprintf(
					"http://%s:%d/iam",
					in_cf.Config.Host.WanAddr.IP(),
					in_cf.Config.Host.HttpPort,
				)
			} else {
				iam_cli.ServiceUrlFrontend = iam_cli.ServiceUrl
			}
		} else {
			iam_cli.ServiceUrlFrontend = in_cf.Config.IamServiceUrlFrontend
		}
		hlog.Printf("info", "IAM ServiceUrl %s", iam_cli.ServiceUrl)
		hlog.Printf("info", "IAM ServiceUrlFrontend %s", iam_cli.ServiceUrlFrontend)

		//
		if err := iam_cfg.InitConfig(); err != nil {
			log.Fatalf("iam_cfg.InitConfig error: %s", err.Error())
		}
		iam_sto.SysConfigRefresh()

		//
		hs.ModuleRegister("/iam/v1", iam_api.NewModule())
		hs.ModuleRegister("/iam", iam_web.NewModule())

		//
		aks := config.InitIamAccessKeyData()
		for _, v := range aks {
			iam_sto.AccessKeyInitData(v)
		}
	}

	// module/LPS: init ips database and webserver
	{
		if err = ips_cf.Init(in_cf.Prefix); err != nil {
			log.Fatalf("ips.Config.Init error: %s", err.Error())
		}

		if err = ips_db.Init(ips_cf.Config.IoConnectors); err != nil {
			log.Fatalf("ips.Data.Init error: %s", err.Error())
		}

		if err := iam_sto.AppInstanceRegister(ips_cf.IamAppInstance()); err != nil {
			log.Fatalf("ips.Data.Init error: %s", err.Error())
		}

		hs.ModuleRegister("/ips/v1", ips_v1.NewModule())
		hs.ModuleRegister("/in/cp/ips/~", httpsrv.NewStaticModule("ips_ui", in_cf.Prefix+"/webui/ips"))

		// TODO
		in_cf.Config.InpackServiceUrl = fmt.Sprintf(
			"http://%s:%d/",
			in_cf.Config.Host.LanAddr.IP(),
			in_cf.Config.Host.HttpPort,
		)
	}

	// module/hchart
	{
		hs.ModuleRegister("/in/cp/hchart/~", httpsrv.NewStaticModule("hchart_ui", in_cf.Prefix+"/webui/hchart/webui"))
	}

	// incore
	{
		in_inst := config.IamAppInstance()
		if err := iam_sto.AppInstanceRegister(in_inst); err != nil {
			log.Fatalf("in.Data.Init error: %s", err.Error())
		}
		if in_cf.Config.InstanceId == "" {
			in_cf.Config.InstanceId = in_inst.Meta.ID
			in_cf.Config.Sync()
		}

		hs.HandlerFuncRegister("/in/v1/pb/termws", in_ws_v1.PodBoundTerminalWsHandlerFunc)

		// Frontend APIs/UI for Users
		hs.ModuleRegister("/in/v1", in_ws_v1.NewModule())
		hs.ModuleRegister("/in/cp", in_ws_cp.NewModule())

		// Backend Operating APIs/UI for System Operators
		hs.ModuleRegister("/in/ops", in_ws_op.NewModule())

		// Frontend UI Index
		hs.ModuleRegister("/in", in_ws_cp.NewIndexModule())

		// i18n
		// hs.Config.I18n(in_cf.Prefix + "/i18n/en.json")
		// hs.Config.I18n(in_cf.Prefix + "/i18n/zh_CN.json")
	}

	// init zonemaster
	{
		if err := in_zm.InitData(config.InitZoneMasterData()); err != nil {
			log.Fatal(err.Error())
		}

		in_zm.Scheduler = in_sched.NewScheduler()
		in_api.RegisterApiZoneMasterServer(in_rpc.Server, new(in_zm.ApiZoneMaster))

		if err := in_zm.Start(); err != nil {
			log.Fatal(err.Error())
		}
	}

	//
	{
		if err := in_host.InitData(config.InitHostletData()); err != nil {
			log.Fatal(err.Error())
		}

		if err := in_host.Start(); err != nil {
			log.Fatal(err.Error())
		}
	}

	//
	if err := in_rpc.Start(in_cf.Config.Host.LanAddr.Port()); err != nil {
		log.Fatalf("rpc.server.Start error: %v", err)
	}

	// http service
	hs.Config.HttpPort = in_cf.Config.Host.HttpPort
	go hs.Start()

	// job/task
	// go nodelet.Start()
	// go scheduler.Start()

	if in_cf.Config.PprofHttpPort > 0 {
		go http.ListenAndServe(fmt.Sprintf(":%d", in_cf.Config.PprofHttpPort), nil)
	}

	in_cf.Config.Sync()

	select {}
}
