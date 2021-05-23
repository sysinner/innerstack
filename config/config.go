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

	"github.com/hooto/hauth/go/hauth/v1"
	"github.com/hooto/iam/iamapi"
	"github.com/lessos/lessgo/crypto/idhash"
	"github.com/lessos/lessgo/types"

	incfg "github.com/sysinner/incore/config"
)

var (
	Version    = "0.10"
	Release    = "1"
	InstanceId = "00" + idhash.HashToHexString([]byte("innerstack"), 14)
	akAccInit  *hauth.AccessKey
)

func Setup(ver, rel, seed string, isZoneMaster bool) error {

	Version = ver
	Release = rel

	if len(incfg.Config.Zone.MainNodes) < 1 {
		return fmt.Errorf("no zone/main_nodes setup")
	}

	if incfg.Config.Zone.ZoneId == "" {
		return fmt.Errorf("no zone_id setup")
	}

	/**
	if incfg.Config.Host.CellId == "" {
		return fmt.Errorf("no cell_id setup")
	}
	*/

	if isZoneMaster &&
		incfg.Config.ZoneMain.IamAccessKey == nil {

		incfg.Config.ZoneMain.IamAccessKey = &hauth.AccessKey{
			User: init_sys_user,
			Id: "00" + idhash.HashToHexString(
				[]byte(fmt.Sprintf("sys/zone/iam_acc_charge/ak/%s", incfg.Config.Zone.ZoneId)), 14),
			Secret: idhash.HashToBase64String(idhash.AlgSha256, []byte(seed), 40),
			Scopes: []*hauth.ScopeFilter{{
				Name:  "sys/zm",
				Value: incfg.Config.Zone.ZoneId,
			}},
			Description: "ZoneMaster AccCharge",
		}

		incfg.Config.Flush()
	}

	return nil
}

func IamAppInstance() iamapi.AppInstance {

	return iamapi.AppInstance{
		Meta: types.InnerObjectMeta{
			ID:   InstanceId,
			User: "sysadmin",
		},
		Version:  Version,
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
