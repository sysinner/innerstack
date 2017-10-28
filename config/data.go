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
	"sort"

	"github.com/hooto/hlog4g/hlog"
	"github.com/hooto/iam/iamapi"
	"github.com/lessos/lessgo/encoding/json"
	"github.com/lessos/lessgo/types"

	incfg "github.com/sysinner/incore/config"
	"github.com/sysinner/incore/inapi"
)

var (
	init_zone_id  = "local"
	init_cell_id  = "general"
	init_sys_user = "sysadmin"
)

func InitHostletData() map[string]interface{} {

	items := map[string]interface{}{}

	items[inapi.NsLocalZoneMasterList()] = inapi.ResZoneMasterList{
		Leader:  incfg.Config.Host.Id,
		Updated: uint64(types.MetaTimeNow()),
		Items: []*inapi.ResZoneMasterNode{
			{
				Id:   incfg.Config.Host.Id,
				Addr: string(incfg.Config.Host.LanAddr),
			},
		},
	}

	return items
}

func InitIamAccessKeyData() []iamapi.AccessKey {
	return []iamapi.AccessKey{
		init_cache_akacc,
	}
}

func InitZoneMasterData() map[string]interface{} {

	var (
		items         = map[string]interface{}{}
		name          string
		host_id       = incfg.Config.Host.Id
		host_lan_addr = string(incfg.Config.Host.LanAddr)
		host_wan_addr = string(incfg.Config.Host.WanAddr)
	)

	// sys zone
	sys_zone := inapi.ResZone{
		Meta: &inapi.ObjectMeta{
			Id:      init_zone_id,
			Created: uint64(types.MetaTimeNow()),
			Updated: uint64(types.MetaTimeNow()),
		},
		Phase: 1,
		LanAddrs: []string{
			host_lan_addr,
		},
	}
	if incfg.Config.Host.WanAddr.Valid() {
		sys_zone.WanAddrs = []string{
			host_wan_addr,
		}
	}

	//
	sys_zone.OptionSet("iam/acc_charge/access_key", init_cache_akacc.AccessKey)
	sys_zone.OptionSet("iam/acc_charge/secret_key", init_cache_akacc.SecretKey)

	items[inapi.NsGlobalSysZone(init_zone_id)] = sys_zone
	items[inapi.NsZoneSysInfo(init_zone_id)] = sys_zone

	// sys cell
	sys_cell := inapi.ResCell{
		Meta: &inapi.ObjectMeta{
			Id:      init_cell_id,
			Created: uint64(types.MetaTimeNow()),
			Updated: uint64(types.MetaTimeNow()),
		},
		ZoneId:      init_zone_id,
		Phase:       1,
		Description: "",
	}
	items[inapi.NsGlobalSysCell(init_zone_id, init_cell_id)] = sys_cell
	items[inapi.NsZoneSysCell(init_zone_id, init_cell_id)] = sys_cell

	// sys host
	sys_host := inapi.ResHost{
		Meta: &inapi.ObjectMeta{
			Id:      host_id,
			Created: uint64(types.MetaTimeNow()),
			Updated: uint64(types.MetaTimeNow()),
		},
		Operate: &inapi.ResHostOperate{
			Action: 1,
			ZoneId: init_zone_id,
			CellId: init_cell_id,
		},
		Spec: &inapi.ResHostSpec{
			PeerLanAddr: host_lan_addr,
		},
	}
	items[inapi.NsZoneSysHost(init_zone_id, host_id)] = sys_host
	items[inapi.NsZoneSysHostSecretKey(init_zone_id, host_id)] = incfg.Config.Host.SecretKey

	//

	// zone-master node(s)/leader
	items[inapi.NsZoneSysMasterNode(init_zone_id, host_id)] = inapi.ResZoneMasterNode{
		Id:     host_id,
		Addr:   host_lan_addr,
		Action: 1,
	}
	items[inapi.NsZoneSysMasterLeader(init_zone_id)] = host_id

	//
	name = "g1"
	plan := inapi.PodSpecPlan{
		Meta: types.InnerObjectMeta{
			ID:      name,
			Name:    name,
			User:    "sysadmin",
			Version: "1",
			Created: types.MetaTimeNow(),
			Updated: types.MetaTimeNow(),
			Title:   "General g1",
		},
		Status: inapi.SpecStatusActive,
		Zones: []*inapi.PodSpecPlanZoneBound{
			{
				Name:  init_zone_id,
				Cells: types.ArrayString([]string{init_cell_id}),
			},
		},
	}
	plan.Labels.Set("pod/spec/plan/type", "g1")
	plan.Annotations.Set("meta/name", "General g1")
	plan.Annotations.Set("meta/homepage", "http://example.com")

	// Spec/Image
	name = "a1el7v1"

	image := inapi.PodSpecBoxImage{
		Meta: types.InnerObjectMeta{
			ID:      name,
			Name:    name,
			User:    "sysadmin",
			Version: "1",
			Created: types.MetaTimeNow(),
			Updated: types.MetaTimeNow(),
		},
		Status:    inapi.SpecStatusActive,
		Driver:    inapi.PodSpecBoxImageDocker,
		OsType:    "linux",
		OsDist:    "el7",
		OsVersion: "7",
		OsName:    "CentOS 7",
		Arch:      "x64",
	}
	image.Options.Set("docker/image/name", "sysinner:a1el7v1")
	items[inapi.NsGlobalPodSpec("box/image", image.Meta.ID)] = image

	image.Meta.User = ""
	image.Meta.Created = 0
	image.Meta.Updated = 0

	plan.Images = append(plan.Images, &inapi.PodSpecPlanBoxImageBound{
		RefId:   image.Meta.ID,
		Driver:  inapi.PodSpecBoxImageDocker,
		Options: image.Options,
		OsDist:  image.OsDist,
		Arch:    image.Arch,
	})
	plan.ImageDefault = image.Meta.ID

	for _, v := range [][]int64{
		{100, 128},
		{200, 256},
		{400, 512},
		{600, 1024},
		{800, 1024},
		{1000, 2048},
		{2000, 4096},
		{4000, 8192},
		{8000, 16384},
	} {

		name = fmt.Sprintf("c%dm%d", v[0], v[1])

		res := inapi.PodSpecResourceCompute{
			Meta: types.InnerObjectMeta{
				ID:      name,
				Name:    name,
				User:    "sysadmin",
				Created: types.MetaTimeNow(),
				Updated: types.MetaTimeNow(),
			},
			Status:   inapi.SpecStatusActive,
			CpuLimit: v[0],
			MemLimit: v[1] * inapi.ByteMB,
		}

		if v[0] == 100 {
			plan.ResourceComputeDefault = res.Meta.ID
		}

		items[inapi.NsGlobalPodSpec("res/compute", res.Meta.ID)] = res

		res.Meta.User = ""
		res.Meta.Created = 0
		res.Meta.Updated = 0

		plan.ResourceComputes = append(plan.ResourceComputes, &inapi.PodSpecPlanResComputeBound{
			RefId:    res.Meta.ID,
			CpuLimit: res.CpuLimit,
			MemLimit: res.MemLimit,
		})
	}
	sort.Sort(plan.ResourceComputes)

	//
	name = "lg1"
	vol := inapi.PodSpecResourceVolume{
		Meta: types.InnerObjectMeta{
			ID:      name,
			Name:    name,
			User:    "sysadmin",
			Version: "1",
			Created: types.MetaTimeNow(),
			Updated: types.MetaTimeNow(),
		},
		Status:  inapi.SpecStatusActive,
		Limit:   10 * inapi.ByteGB,
		Request: 100 * inapi.ByteMB,
		Step:    100 * inapi.ByteMB,
		Default: 100 * inapi.ByteMB,
	}
	vol.Labels.Set("pod/spec/res/volume/type", "system")
	items[inapi.NsGlobalPodSpec("res/volume", vol.Meta.ID)] = vol

	vol.Meta.User = ""
	vol.Meta.Created = 0
	vol.Meta.Updated = 0

	plan.ResourceVolumes = append(plan.ResourceVolumes, &inapi.PodSpecPlanResVolumeBound{
		RefId:   vol.Meta.ID,
		Limit:   vol.Limit,
		Request: vol.Request,
		Step:    vol.Step,
		Default: vol.Default,
	})
	plan.ResourceVolumeDefault = vol.Meta.ID

	//
	items[inapi.NsGlobalPodSpec("plan", plan.Meta.ID)] = plan

	specs := []string{
		"app_spec_hooto-press.json",
		"app_spec_sysinner-httplb.json",
		"app_spec_sysinner-mysql.json",
	}
	for _, v := range specs {
		var spec inapi.AppSpec
		if err := json.DecodeFile(incfg.Prefix+"/misc/app-spec/"+v, &spec); err != nil || spec.Meta.ID == "" {
			hlog.Printf("warn", "init app spec %s error", v)
			continue
		}
		spec.Meta.User = "sysadmin"
		items[inapi.NsGlobalAppSpec(spec.Meta.ID)] = spec
	}

	return items
}
