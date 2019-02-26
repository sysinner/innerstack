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

	"github.com/hooto/hlog4g/hlog"
	"github.com/hooto/httpsrv"
	"github.com/lessos/lessgo/crypto/idhash"

	iam_cfg "github.com/hooto/iam/config"
	iam_cli "github.com/hooto/iam/iamclient"
	iam_db "github.com/hooto/iam/store"
	iam_web "github.com/hooto/iam/websrv/ctrl"
	iam_v1 "github.com/hooto/iam/websrv/v1"

	ips_cf "github.com/sysinner/inpack/server/config"
	ips_db "github.com/sysinner/inpack/server/data"
	ips_p1 "github.com/sysinner/inpack/websrv/p1"
	ips_v1 "github.com/sysinner/inpack/websrv/v1"

	in_ws_cp "github.com/sysinner/incore/websrv/cp"
	in_ws_op "github.com/sysinner/incore/websrv/ops"
	in_ws_p1 "github.com/sysinner/incore/websrv/p1"
	in_ws_v1 "github.com/sysinner/incore/websrv/v1"
	in_ws_ui "github.com/sysinner/inpanel"

	in_cfg "github.com/sysinner/incore/config"
	in_db "github.com/sysinner/incore/data"
	in_host "github.com/sysinner/incore/hostlet"
	in_api "github.com/sysinner/incore/inapi"
	in_rpc "github.com/sysinner/incore/rpcsrv"
	in_sts "github.com/sysinner/incore/status"
	in_ver "github.com/sysinner/incore/version"
	in_zm "github.com/sysinner/incore/zonemaster"

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

	// initialize configuration
	{
		if err = in_cfg.Setup(); err != nil {
			log.Fatalf("incore/config/Setup error: %s", err.Error())
		}

		if err = is_cfg.Setup(version, release, in_cfg.Config.Host.SecretKey, in_cfg.IsZoneMaster()); err != nil {
			log.Fatalf("innerstack/config/Setup error: %s", err.Error())
		}
	}

	// initialize status
	{
		if err = in_sts.Init(); err != nil {
			log.Fatalf("status.Init error: %s", err.Error())
		}
	}

	{
		hlog.Printf("info", "InnerStack %s-%s", version, release)
		hlog.Printf("info", "inCore %s", in_ver.Version)

		if in_cfg.IsZoneMaster() {

			hlog.Printf("info", "inPack %s", ips_cf.Version)
			hlog.Printf("info", "inPanel %s", in_ws_ui.Version)

			in_ws_ui.VersionHash = idhash.HashToHexString([]byte(
				(version + release + released)), 16)
			in_ws_ui.ZoneId = in_cfg.Config.Host.ZoneId

			if in_cfg.Config.ZoneMaster.MultiHostEnable {
				in_ws_ui.OpsClusterHost = true
				if in_cfg.Config.ZoneMaster.MultiCellEnable {
					in_ws_ui.OpsClusterCell = true
					if in_cfg.Config.ZoneMaster.MultiZoneEnable {
						in_ws_ui.OpsClusterZone = true
					}
				}
			}
		}
	}

	// initialize data/io connection
	{
		if err := in_db.Setup(); err != nil {
			log.Fatalf("in_db setup err %s", err.Error())
		}
	}

	// module/IAM
	if in_cfg.IsZoneMaster() {
		//
		iam_cfg.Prefix = in_cfg.Prefix + "/vendor/github.com/hooto/iam"
		iam_cfg.Config.InstanceID = "00" + idhash.HashToHexString([]byte("innerstack/iam"), 14)
		iam_cfg.VersionHash = idhash.HashToHexString([]byte(
			(iam_cfg.Config.InstanceID + iam_cfg.Version + released)), 16)

		// init database
		iam_db.Data = in_db.GlobalMaster
		if err := iam_db.Init(); err != nil {
			log.Fatalf("iam.Store.Init error: %s", err.Error())
		}
		if err := iam_db.InitData(); err != nil {
			log.Fatalf("iam.Store.InitData error: %s", err.Error())
		}

		//
		if err := iam_cfg.InitConfig(); err != nil {
			log.Fatalf("iam_cfg.InitConfig error: %s", err.Error())
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
			in_cfg.Config.Host.LanAddr.IP(),
			in_cfg.Config.Host.HttpPort,
		)
		if in_cfg.Config.IamServiceUrlFrontend == "" {
			if in_cfg.Config.Host.WanAddr.IP() != "" {
				iam_cli.ServiceUrlFrontend = fmt.Sprintf(
					"http://%s:%d/iam",
					in_cfg.Config.Host.WanAddr.IP(),
					in_cfg.Config.Host.HttpPort,
				)
			} else {
				iam_cli.ServiceUrlFrontend = iam_cli.ServiceUrl
			}
		} else {
			iam_cli.ServiceUrlFrontend = in_cfg.Config.IamServiceUrlFrontend
		}

		if in_cfg.Config.IamServiceUrlGlobal != "" {
			iam_cli.ServiceUrlGlobal = in_cfg.Config.IamServiceUrlGlobal
		}

		hlog.Printf("info", "IAM ServiceUrl %s", iam_cli.ServiceUrl)
		hlog.Printf("info", "IAM ServiceUrlFrontend %s", iam_cli.ServiceUrlFrontend)

		if in_cfg.Config.IamServiceUrlGlobal != "" {
			iam_cli.ServiceUrlGlobal = in_cfg.Config.IamServiceUrlGlobal
			hlog.Printf("info", "IAM ServiceUrlGlobal %s", iam_cli.ServiceUrlGlobal)
		}
	}

	// module/IPS: init ips database and webserver
	if in_cfg.IsZoneMaster() {

		if err = ips_cf.Setup(in_cfg.Prefix); err != nil {
			log.Fatalf("ips.Config.Init error: %s", err.Error())
		}

		// init database
		if err = ips_db.Setup(); err != nil {
			log.Fatalf("ips_db setup err %s", err.Error())
		}
		in_db.InpackData = ips_db.Data

		if err := iam_db.AppInstanceRegister(ips_cf.IamAppInstance()); err != nil {
			log.Fatalf("ips.Data.Init error: %s", err.Error())
		}

		hs.ModuleRegister("/ips/v1", ips_v1.NewModule())
		hs.ModuleRegister("/ips/p1", ips_p1.NewModule())
		hs.ModuleRegister("/in/cp/ips/~", httpsrv.NewStaticModule("ips_ui", in_cfg.Prefix+"/webui/ips"))

		// TODO
		in_cfg.Config.InpackServiceUrl = fmt.Sprintf(
			"http://%s:%d/",
			in_cfg.Config.Host.LanAddr.IP(),
			in_cfg.Config.Host.HttpPort,
		)

		//
		if aks := ips_cf.InitIamAccessKeyData(); len(aks) > 0 {
			for _, v := range aks {
				iam_db.AccessKeyInitData(v)
			}
		}
	}

	// module/hchart
	if in_cfg.IsZoneMaster() {
		hs.ModuleRegister("/in/cp/hchart/~", httpsrv.NewStaticModule("hchart_ui", in_cfg.Prefix+"/webui/hchart/webui"))
		hs.ModuleRegister("/in/ops/hchart/~", httpsrv.NewStaticModule("hchart_ui_ops", in_cfg.Prefix+"/webui/hchart/webui"))
	}

	// incore
	if in_cfg.IsZoneMaster() {

		in_inst := is_cfg.IamAppInstance()
		if err := iam_db.AppInstanceRegister(in_inst); err != nil {
			log.Fatalf("in.Data.Init error: %s", err.Error())
		}
		in_cfg.Config.InstanceId = in_inst.Meta.ID
		in_cfg.Config.Sync()

		hs.HandlerFuncRegister("/in/v1/pb/termws", in_ws_v1.PodBoundTerminalWsHandlerFunc)

		// Frontend APIs/UI for Users
		hs.ModuleRegister("/in/v1", in_ws_v1.NewModule())
		hs.ModuleRegister("/in/cp", in_ws_cp.NewModule())

		// Backend Operating APIs/UI for System Operators
		hs.ModuleRegister("/in/ops", in_ws_op.NewModule())

		// Frontend UI Index
		hs.ModuleRegister("/in/p1", in_ws_p1.NewModule())
		hs.ModuleRegister("/in", in_ws_cp.NewIndexModule())

		// i18n
		// hs.Config.I18n(in_cfg.Prefix + "/i18n/en.json")
		// hs.Config.I18n(in_cfg.Prefix + "/i18n/zh_CN.json")
	}

	// init zonemaster
	if in_cfg.IsZoneMaster() {

		if err := in_zm.InitData(is_cfg.InitZoneMasterData()); err != nil {
			log.Fatalf("in_zm.InitData err %s", err.Error())
		}

		if err := in_zm.SetupScheduler(); err != nil {
			log.Fatalf("in_zm.SetupScheduler err %s", err.Error())
		}

		in_api.RegisterApiZoneMasterServer(in_rpc.Server, new(in_zm.ApiZoneMaster))

		if err := in_zm.Start(); err != nil {
			log.Fatalf("in_zm.Start err %s", err.Error())
		}
	}

	//
	{
		if err := in_host.Start(); err != nil {
			log.Fatalf("in_host.Start err %s", err.Error())
		}
	}

	// hostlet
	{
		in_api.RegisterApiHostMemberServer(in_rpc.Server, new(in_host.ApiHostMember))
	}

	//
	if err := in_rpc.Start(in_cfg.Config.Host.LanAddr.Port()); err != nil {
		log.Fatalf("rpc.server.Start error: %v", err)
	} else {
		hlog.Printf("info", "rpc/server bind :%d", in_cfg.Config.Host.LanAddr.Port())
	}

	// http service
	if in_cfg.IsZoneMaster() {
		hs.Config.HttpPort = in_cfg.Config.Host.HttpPort
		go hs.Start()
	}

	/**
	if in_cfg.Config.PprofHttpPort > 0 {
		go http.ListenAndServe(fmt.Sprintf(":%d", in_cfg.Config.PprofHttpPort), nil)
		fmt.Println("PprofHttpPort", in_cfg.Config.PprofHttpPort)
	}
	*/

	in_cfg.Config.Sync()

	select {}
}
