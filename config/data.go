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
	"sort"

	"github.com/lessos/lessgo/encoding/json"
	"github.com/lessos/lessgo/logger"
	"github.com/lessos/lessgo/types"

	loscfg "code.hooto.com/lessos/loscore/config"
	"code.hooto.com/lessos/loscore/losapi"
)

func InitHostletData() map[string]interface{} {

	items := map[string]interface{}{}

	items[losapi.NsLocalZoneMasterList()] = losapi.ResZoneMasterList{
		Leader:  loscfg.Config.Host.Id,
		Updated: uint64(types.MetaTimeNow()),
		Items: []*losapi.ResZoneMasterNode{
			{
				Id:   loscfg.Config.Host.Id,
				Addr: string(loscfg.Config.Host.LanAddr),
			},
		},
	}

	return items
}

func InitZoneMasterData() map[string]interface{} {

	var (
		items         = map[string]interface{}{}
		name          types.NameIdentifier
		zone_id       = "local"
		cell_id       = "general"
		host_id       = loscfg.Config.Host.Id
		host_lan_addr = string(loscfg.Config.Host.LanAddr)
		host_wan_addr = string(loscfg.Config.Host.WanAddr)
	)

	// sys zone
	sys_zone := losapi.ResZone{
		Meta: &losapi.ObjectMeta{
			Id:      zone_id,
			Created: uint64(types.MetaTimeNow()),
			Updated: uint64(types.MetaTimeNow()),
		},
		Phase: 1,
		LanAddrs: []string{
			host_lan_addr,
		},
	}
	if loscfg.Config.Host.WanAddr.Valid() {
		sys_zone.WanAddrs = []string{
			host_wan_addr,
		}
	}
	items[losapi.NsGlobalSysZone(zone_id)] = sys_zone
	items[losapi.NsZoneSysInfo(zone_id)] = sys_zone

	// sys cell
	sys_cell := losapi.ResCell{
		Meta: &losapi.ObjectMeta{
			Id:      cell_id,
			Created: uint64(types.MetaTimeNow()),
			Updated: uint64(types.MetaTimeNow()),
		},
		ZoneId:      zone_id,
		Phase:       1,
		Description: "",
	}
	items[losapi.NsGlobalSysCell(zone_id, cell_id)] = sys_cell
	items[losapi.NsZoneSysCell(zone_id, cell_id)] = sys_cell

	// sys host
	sys_host := losapi.ResHost{
		Meta: &losapi.ObjectMeta{
			Id:      host_id,
			Created: uint64(types.MetaTimeNow()),
			Updated: uint64(types.MetaTimeNow()),
		},
		Operate: &losapi.ResHostOperate{
			Action: 1,
			ZoneId: zone_id,
			CellId: cell_id,
		},
		Spec: &losapi.ResHostSpec{
			PeerLanAddr: host_lan_addr,
		},
	}
	items[losapi.NsZoneSysHost(zone_id, host_id)] = sys_host
	items[losapi.NsZoneSysHostSecretKey(zone_id, host_id)] = loscfg.Config.Host.SecretKey

	//

	// zone-master node(s)/leader
	items[losapi.NsZoneSysMasterNode(zone_id, host_id)] = losapi.ResZoneMasterNode{
		Id:     host_id,
		Addr:   host_lan_addr,
		Action: 1,
	}
	items[losapi.NsZoneSysMasterLeader(zone_id)] = host_id

	//
	name = types.NewNameIdentifier("pod/spec/plan/b1")
	plan := losapi.PodSpecPlan{
		Meta: types.InnerObjectMeta{
			ID:      name.HashToString(16),
			Name:    name.String(),
			User:    "sysadmin",
			Version: "1",
			Created: types.MetaTimeNow(),
			Updated: types.MetaTimeNow(),
			Title:   "Basic B1",
		},
		Status: losapi.SpecStatusActive,
		Zones: []losapi.PodSpecPlanZone{
			{
				Name:  zone_id,
				Cells: []string{cell_id},
			},
		},
	}
	plan.Labels.Set("pod/spec/plan/type", "b1")
	plan.Annotations.Set("meta/name", "Basic B1")
	plan.Annotations.Set("meta/homepage", "http://example.com")

	// Spec/Image
	name = types.NewNameIdentifier("pod/spec/box/image/d1el7b1")

	image := losapi.PodSpecBoxImage{
		Meta: types.InnerObjectMeta{
			ID:      name.HashToString(16),
			Name:    name.String(),
			User:    "sysadmin",
			Version: "1",
			Created: types.MetaTimeNow(),
			Updated: types.MetaTimeNow(),
		},
		Status:    losapi.SpecStatusActive,
		Driver:    losapi.PodSpecBoxImageDocker,
		OsType:    "linux",
		OsDist:    "el7",
		OsVersion: "7",
		OsName:    "CentOS 7",
		Arch:      "x64",
	}
	image.Options.Set("docker/image/name", "lessos:d1el7b1")
	items[losapi.NsGlobalPodSpec("box/image", image.Meta.ID)] = image

	image.Meta.User = ""
	image.Meta.Created = 0
	image.Meta.Updated = 0

	plan.Images = append(plan.Images, image)
	plan.ImageDefault = image.Meta.ID

	for _, v := range [][]int64{
		{100, 128},
		{200, 256},
		{300, 512},
		{500, 1024},
		{1000, 2048},
		{2000, 4096},
		{4000, 8192},
		{8000, 16384},
	} {

		name = types.NewNameIdentifier(fmt.Sprintf("pod/spec/res/compute/c%06dm%06d", v[0], v[1]))

		res := losapi.PodSpecResourceCompute{
			Meta: types.InnerObjectMeta{
				ID:      name.HashToString(16),
				Name:    name.String(),
				User:    "sysadmin",
				Version: "1",
				Created: types.MetaTimeNow(),
				Updated: types.MetaTimeNow(),
			},
			Status:      losapi.SpecStatusActive,
			CpuLimit:    v[0],
			MemoryLimit: v[1] * losapi.ByteMB,
		}

		if v[0] == 100 {
			plan.ResourceComputeDefault = res.Meta.ID
		}

		items[losapi.NsGlobalPodSpec("res/compute", res.Meta.ID)] = res

		res.Meta.User = ""
		res.Meta.Created = 0
		res.Meta.Updated = 0

		plan.ResourceComputes = append(plan.ResourceComputes, &res)
	}
	sort.Sort(plan.ResourceComputes)

	//
	name = types.NewNameIdentifier("pod/spec/res/volume/local.g01.h01")
	vol := losapi.PodSpecResourceVolume{
		Meta: types.InnerObjectMeta{
			ID:      name.HashToString(16),
			Name:    name.String(),
			User:    "sysadmin",
			Version: "1",
			Created: types.MetaTimeNow(),
			Updated: types.MetaTimeNow(),
		},
		Status:  losapi.SpecStatusActive,
		Limit:   10 * losapi.ByteGB,
		Request: 100 * losapi.ByteMB,
		Step:    100 * losapi.ByteMB,
		Default: 100 * losapi.ByteMB,
	}
	vol.Labels.Set("pod/spec/res/volume/type", "system")
	items[losapi.NsGlobalPodSpec("res/volume", vol.Meta.ID)] = vol

	vol.Meta.User = ""
	vol.Meta.Created = 0
	vol.Meta.Updated = 0

	plan.ResourceVolumes = append(plan.ResourceVolumes, vol)
	plan.ResourceVolumeDefault = vol.Meta.ID

	//
	items[losapi.NsGlobalPodSpec("plan", plan.Meta.ID)] = plan

	specs := []string{
		"los_app_spec_hooto-press.json",
		"los_app_spec_los-httplb.json",
		"los_app_spec_los-mysql.json",
	}
	for _, v := range specs {
		var spec losapi.AppSpec
		if err := json.DecodeFile(loscfg.Prefix+"/misc/app-spec/"+v, &spec); err != nil || spec.Meta.ID == "" {
			logger.Printf("warn", "init app spec %s error", v)
			continue
		}
		spec.Meta.User = "sysadmin"
		items[losapi.NsGlobalAppSpec(spec.Meta.ID)] = spec
	}

	return items
}
