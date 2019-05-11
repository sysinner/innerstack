// Copyright 2019 Eryx <evorui аt gmail dοt com>, All rights reserved.
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
	"errors"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/hooto/hflag4g/hflag"
	"github.com/lessos/lessgo/encoding/json"

	"github.com/sysinner/incore/config"
	"github.com/sysinner/incore/inapi"
)

var (
	version = "0.9.0"
	release = ""
	Prefix  = "/opt/sysinner/innerstack"
)

func main() {

	action := "agent"
	if len(os.Args) > 1 {
		action = os.Args[1]
	}

	//
	help := false
	if _, ok := hflag.ValueOK("help"); ok {
		help = true
	}

	//
	if v, ok := hflag.ValueOK("prefix"); ok {
		Prefix = v.String()
	}

	switch action {

	case "info":
		if help {
			cmdInfoHelp()
		} else if err := cmdInfo(false); err != nil {
			fmt.Println(err)
		}

	case "config-init":

		if help {
			cmdConfigInitHelp()
		} else if err := cmdConfigInit(); err != nil {
			fmt.Println(err)
		}

	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`
Usage:	innerstack-cli Command [OPTIONS]

Commands:
  info         Display system information 
  config-init  Initial configuration
  help
`)
}

func cmdConfigInitHelp() {

	fmt.Println(`
Usage:	innerstack-cli config-init [OPTIONS]
  Initial configuration
Options:
  --zone_master_enable  Set to run as Zone Master
  --multi_host_enable  Allow Zone Master to manage multiple hosts
  --multi_cell_enable  Allow Zone Master to group the hosts
  --multi_replica_enable  Allow Zone Master to enable replication
`)
}

func cmdConfigInit() error {

	//
	if u, err := user.Current(); err != nil || u.Uid != "0" {
		return fmt.Errorf("Access Denied : must be run as root")
	}

	cfgFile := Prefix + "/etc/config.json"

	if _, err := os.Stat(cfgFile); err == nil {
		return fmt.Errorf("Command Denied : the config file (%s) already exists", cfgFile)
	}

	if _, ok := hflag.ValueOK("zone_master_enable"); ok {

		config.Config.ZoneMaster = &config.ZoneMaster{}

		if _, ok := hflag.ValueOK("multi_host_enable"); ok {
			config.Config.ZoneMaster.MultiHostEnable = true
		}

		if _, ok := hflag.ValueOK("multi_cell_enable"); ok {
			config.Config.ZoneMaster.MultiCellEnable = true
		}

		if _, ok := hflag.ValueOK("multi_replica_enable"); ok {
			config.Config.ZoneMaster.MultiReplicaEnable = true
		}
	}

	config.Config.SetupHost()

	if config.Config.ZoneMaster != nil {

		//
		if v, ok := hflag.ValueOK("masters"); ok {
			ls := strings.Split(v.String(), ",")
			for _, v2 := range ls {
				addr := inapi.HostNodeAddress(v2)
				if !addr.Valid() {
					return errors.New("invalid --masters sets")
				}
				config.Config.Masters = append(config.Config.Masters, addr)
			}
		}
		if len(config.Config.Masters) < 1 {
			config.Config.Masters = append(config.Config.Masters, config.Config.Host.LanAddr)
		}

		//
		if v, ok := hflag.ValueOK("zone_id"); ok {
			if !inapi.ResSysZoneIdReg.MatchString(v.String()) {
				return errors.New("invalid --zone_id set")
			}
			config.Config.Host.ZoneId = v.String()
		} else {
			config.Config.Host.ZoneId = config.InitZoneId
		}

		//
		if v, ok := hflag.ValueOK("cell_id"); ok {
			if !inapi.ResSysCellIdReg.MatchString(v.String()) {
				return errors.New("invalid --cell_id set")
			}
			config.Config.Host.CellId = v.String()
		} else {
			config.Config.Host.CellId = config.InitCellId
		}
	}

	config.Config.LxcFsEnable = true

	err := json.EncodeToFile(config.Config, cfgFile, "  ")
	if err != nil {
		return err
	}

	return cmdInfo(true)
}

func cmdInfoHelp() {

	fmt.Println(`
Usage:	innerstack-cli info [OPTIONS]
  Display system information
Options:
  --show-secretkey Print the full secret-key content
`)
}

func cmdInfo(showSecretKey bool) error {

	//
	if err := json.DecodeFile(Prefix+"/etc/config.json", &config.Config); err != nil {
		return err
	}

	if _, ok := hflag.ValueOK("show-secretkey"); ok {
		showSecretKey = true
	}

	secretKey := config.Config.Host.SecretKey
	if len(secretKey) > 20 && !showSecretKey {
		secretKey = secretKey[:20] + "..."
	}

	fmt.Println("Host")
	fmt.Printf("  ID         %s\n", config.Config.Host.Id)
	fmt.Printf("  Address    %s\n", config.Config.Host.LanAddr)
	fmt.Printf("  Secret Key %s\n", secretKey)
	fmt.Printf("  Http Port  %d\n", config.Config.Host.HttpPort)
	fmt.Printf("  Zone ID    %s\n", config.Config.Host.ZoneId)
	fmt.Printf("  Cell ID    %s\n", config.Config.Host.CellId)

	fmt.Printf("\nMasters %d\n", len(config.Config.Masters))
	for i, v := range config.Config.Masters {
		fmt.Printf(" Master #%d %s\n", i, v)
	}

	if config.Config.ZoneMaster != nil {
		fmt.Println("\nZone Master Enable")
		fmt.Printf("  MultiZoneEnable    %v\n", config.Config.ZoneMaster.MultiZoneEnable)
		fmt.Printf("  MultiCellEnable    %v\n", config.Config.ZoneMaster.MultiCellEnable)
		fmt.Printf("  MultiHostEnable    %v\n", config.Config.ZoneMaster.MultiHostEnable)
		fmt.Printf("  MultiReplicaEnable %v\n", config.Config.ZoneMaster.MultiReplicaEnable)
	}

	if len(config.Config.Masters) > 0 {

		fmt.Println("\nServices")
		fmt.Printf("  InpackServiceUrl %s\n", config.Config.InpackServiceUrl)
		fmt.Printf("  IamServiceUrlFrontend %s\n", config.Config.IamServiceUrlFrontend)
		fmt.Printf("  IamServiceUrlGlobal %s\n", config.Config.IamServiceUrlGlobal)

		fmt.Println("\nImage Services", len(config.Config.ImageServices))
		for _, v := range config.Config.ImageServices {
			fmt.Printf(" Driver %s, Url %s\n", v.Driver, v.Url)
		}
	}

	return nil
}
