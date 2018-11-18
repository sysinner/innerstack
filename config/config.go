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

package config

import (
	"fmt"

	"github.com/hooto/iam/iamapi"
	"github.com/lessos/lessgo/crypto/idhash"
	"github.com/lessos/lessgo/types"
	"github.com/lynkdb/iomix/connect"

	incfg "github.com/sysinner/incore/config"
	"github.com/sysinner/incore/inapi"
)

var (
	version          = "0.4.0"
	release          = "1"
	InstanceId       = "00" + idhash.HashToHexString([]byte("insoho"), 14)
	init_cache_akacc iamapi.AccessKey
)

func Init(ver, rel, seed string) error {

	version = ver
	release = rel

	incfg.Config.Masters = []inapi.HostNodeAddress{
		incfg.Config.Host.LanAddr,
	}

	incfg.Config.Host.ZoneId = "local"

	if err := init_data(); err != nil {
		return err
	}

	init_cache_akacc = iamapi.AccessKey{
		User: init_sys_user,
		AccessKey: "00" + idhash.HashToHexString(
			[]byte(fmt.Sprintf("sys/zone/iam_acc_charge/ak/%s", init_zone_id)), 14),
		SecretKey: idhash.HashToBase64String(idhash.AlgSha256, []byte(seed), 40),
		Bounds: []iamapi.AccessKeyBound{{
			Name: "sys/zm/" + init_zone_id,
		}},
		Description: "ZoneMaster AccCharge",
	}

	return nil
}

//
func init_data() error {

	io_name := types.NewNameIdentifier("in_zone_master")
	opts := incfg.Config.IoConnectors.Options(io_name)

	if opts == nil {

		opts = &connect.ConnOptions{
			Name:      io_name,
			Connector: "iomix/skv/connector",
			Driver:    types.NewNameIdentifier("lynkdb/kvgo"),
		}
	}

	if opts.Value("data_dir") == "" {
		opts.SetValue("data_dir", incfg.Prefix+"/var/"+string(io_name))
	}

	incfg.Config.IoConnectors.SetOptions(*opts)

	return nil
}

func IamAppInstance() iamapi.AppInstance {

	return iamapi.AppInstance{
		Meta: types.InnerObjectMeta{
			ID:   InstanceId,
			User: "sysadmin",
		},
		Version:  version,
		AppID:    "insoho",
		AppTitle: "SysInner for SOHO",
		Status:   1,
		Url:      "",
		Privileges: []iamapi.AppPrivilege{
			{
				Privilege: "sysinner.admin",
				Roles:     []uint32{1},
				Desc:      "SysInner Management",
			},
		},
	}
}
