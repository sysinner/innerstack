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

	in_cfg "github.com/sysinner/incore/config"
	"github.com/sysinner/incore/inapi"
)

var (
	version          = "0.9.0"
	release          = "1"
	InstanceId       = "00" + idhash.HashToHexString([]byte("innerstack"), 14)
	init_cache_akacc iamapi.AccessKey
)

func Setup(ver, rel, seed string, isZoneMaster bool) error {

	version = ver
	release = rel

	if len(in_cfg.Config.Masters) < 1 &&
		in_cfg.Config.ZoneMaster != nil {

		in_cfg.Config.Masters = []inapi.HostNodeAddress{
			in_cfg.Config.Host.LanAddr,
		}

		if in_cfg.Config.Host.ZoneId == "" {
			in_cfg.Config.Host.ZoneId = in_cfg.InitZoneId
		}

		if in_cfg.Config.Host.CellId == "" {
			in_cfg.Config.Host.CellId = in_cfg.InitCellId
		}
	}

	if isZoneMaster {

		if in_cfg.Config.ZoneIamAccessKey != nil {
			init_cache_akacc = *in_cfg.Config.ZoneIamAccessKey
		} else {

			init_cache_akacc = iamapi.AccessKey{
				User: init_sys_user,
				AccessKey: "00" + idhash.HashToHexString(
					[]byte(fmt.Sprintf("sys/zone/iam_acc_charge/ak/%s", in_cfg.Config.Host.ZoneId)), 14),
				SecretKey: idhash.HashToBase64String(idhash.AlgSha256, []byte(seed), 40),
				Bounds: []iamapi.AccessKeyBound{{
					Name: "sys/zm/" + in_cfg.Config.Host.ZoneId,
				}},
				Description: "ZoneMaster AccCharge",
			}

			in_cfg.Config.ZoneIamAccessKey = &init_cache_akacc
			in_cfg.Config.Sync()
		}
	}

	return nil
}

func IamAppInstance() iamapi.AppInstance {

	return iamapi.AppInstance{
		Meta: types.InnerObjectMeta{
			ID:   InstanceId,
			User: "sysadmin",
		},
		Version:  version,
		AppID:    "innerstack",
		AppTitle: "InnerStack Enterprise Paas",
		Status:   1,
		Url:      "",
		Privileges: []iamapi.AppPrivilege{
			{
				Privilege: "sysinner.admin",
				Roles:     []uint32{1},
				Desc:      "System Management",
			},
		},
	}
}
