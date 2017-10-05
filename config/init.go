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
	init_cache_akacc iamapi.AccessKey
)

func Init(ver, seed string) error {

	version = ver

	incfg.Config.Masters = []inapi.HostNodeAddress{
		incfg.Config.Host.LanAddr,
	}

	incfg.Config.Host.ZoneId = "local"

	if err := init_data(); err != nil {
		return err
	}

	init_cache_akacc = iamapi.AccessKey{
		User: init_sys_user,
		AccessKey: idhash.HashToHexString(
			[]byte(fmt.Sprintf("sys/zone/iam_acc_charge/ak/%s", init_zone_id)), 16),
		SecretKey: idhash.HashToBase64String(idhash.AlgMd5, []byte(seed), 40),
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
			Connector: "iomix/skv/Connector",
			Driver:    types.NewNameIdentifier("lynkdb/kvgo"),
		}
	}

	if opts.Value("data_dir") == "" {
		opts.SetValue("data_dir", incfg.Prefix+"/var/"+string(io_name))
	}

	incfg.Config.IoConnectors.SetOptions(*opts)

	return nil
}
