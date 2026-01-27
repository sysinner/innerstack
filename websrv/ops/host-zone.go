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

package ops

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/hooto/hauth/go"
	"github.com/hooto/hlog4g/hlog"
	"github.com/lessos/lessgo/crypto/idhash"
	"github.com/lessos/lessgo/types"

	iam_db "github.com/hooto/iam/data"
	"github.com/sysinner/incore/data"
	"github.com/sysinner/incore/inapi"
	"github.com/sysinner/incore/status"
)

func (c Host) ZoneListAction() {

	ls := inapi.GeneralObjectList{}
	defer c.RenderJson(&ls)

	for _, v := range status.GlobalZones {

		zone := inapi.ResZone{
			Meta:               v.Meta,
			Phase:              v.Phase,
			WanAddrs:           v.WanAddrs,
			LanAddrs:           v.LanAddrs,
			Options:            v.Options,
			WanApi:             v.WanApi,
			ImageServices:      v.ImageServices,
			NetworkDomainName:  v.NetworkDomainName,
			NetworkVpcInstance: v.NetworkVpcInstance,
			NetworkVpcBridge:   v.NetworkVpcBridge,
		}

		if v.Driver != nil {
			zone.Driver = &inapi.ConfigInstance{
				Name: v.Driver.Name,
			}
		} else {
			zone.Driver = &inapi.ConfigInstance{
				Name: "private_cloud",
			}
		}

		if c.Params.Value("fields") == "cells" {
			zone.Cells = v.Cells
		}

		ls.Items = append(ls.Items, zone)
	}

	ls.Kind = "HostZoneList"
}

func (c Host) ZoneDriverSpecListAction() {

	var set inapi.GeneralObjectList
	defer c.RenderJson(&set)

	set.Kind = "ZoneDriverSpecList"
	for _, v := range status.ZoneDrivers {
		if v != nil {
			set.Items = append(set.Items, v.ConfigSpec())
		}
	}
}

func (c Host) ZoneEntryAction() {

	var set struct {
		inapi.GeneralObject
		inapi.ResZone
	}

	defer c.RenderJson(&set)

	var (
		zoneId = c.Params.Value("id")
		fields = c.Params.Value("fields")
	)

	rs := data.DataGlobal.NewReader(
		inapi.NsGlobalSysZone(zoneId)).Exec()
	if rs.OK() {
		rs.Item().JsonDecode(&set.ResZone)
	} else if rs.NotFound() {
		rs = data.DataGlobal.NewReader(
			inapi.NsGlobalSysZone(zoneId)).Exec()
		if rs.OK() {
			rs.Item().JsonDecode(&set.ResZone)
		}
	}

	if set.Meta.Id == zoneId && fields == "cells" {

		var (
			offset = inapi.NsGlobalSysCell(set.Meta.Id, "")
			rs2    = data.DataGlobal.NewRanger(offset, offset).
				SetLimit(100).Exec()
		)

		for _, v2 := range rs2.Items {
			var cell inapi.ResCell
			if err := v2.JsonDecode(&cell); err == nil {
				set.Cells = append(set.Cells, &cell)
			}
		}
	}

	if set.Meta.Id != "" {
		set.Kind = "HostZone"
	} else {
		set.Error = &types.ErrorMeta{"404", "Item Not Found"}
	}
}

func (c Host) ZoneSetAction() {

	var set struct {
		inapi.GeneralObject
		inapi.ResZone
	}

	defer c.RenderJson(&set)

	if err := c.Request.JsonDecode(&set.ResZone); err != nil {
		set.Error = &types.ErrorMeta{"400", err.Error()}
		return
	}

	set.Meta.Id = strings.ToLower(set.Meta.Id)

	if mat := inapi.ResSysZoneIdReg.MatchString(set.Meta.Id); !mat {
		set.Error = &types.ErrorMeta{
			Code:    "400",
			Message: "Zone Id must consist of letters or numbers, and begin with a letter",
		}
		return
	}

	if set.Driver == nil {
		set.Error = &types.ErrorMeta{
			Code:    "400",
			Message: "driver not setup",
		}
		return
	}

	zoneDriver := status.ZoneDriver(set.Driver.Name)
	if zoneDriver == nil {
		set.Error = &types.ErrorMeta{
			Code:    "400",
			Message: "driver not found",
		}
		return
	}

	if err := inapi.ConfigInstanceApply(set.Driver, zoneDriver.ConfigSpec()); err != nil {
		set.Error = &types.ErrorMeta{
			Code:    "400",
			Message: err.Error(),
		}
		return
	}

	if err := zoneDriver.ConfigValid(set.Driver); err != nil {
		set.Error = &types.ErrorMeta{
			Code:    "400",
			Message: err.Error(),
		}
		return
	}

	set.WanApi = strings.Trim(strings.TrimSpace(set.WanApi), "/")
	if len(set.WanApi) > 0 {
		if !strings.HasPrefix(set.WanApi, "http://") &&
			!strings.HasPrefix(set.WanApi, "https://") {
			set.Error = types.NewErrorMeta("400", fmt.Sprintf("Invalid Address (%s)", set.WanApi))
			return
		}
	}

	for _, addr := range set.WanAddrs {

		if !inapi.HostNodeAddress(addr).Valid() {
			set.Error = &types.ErrorMeta{
				Code:    "400",
				Message: fmt.Sprintf("Invalid Address (%s)", addr),
			}
			return
		}
	}

	for _, addr := range set.LanAddrs {

		if !inapi.HostNodeAddress(addr).Valid() {
			set.Error = &types.ErrorMeta{
				Code:    "400",
				Message: fmt.Sprintf("Invalid Address (%s)", addr),
			}
			return
		}
	}

	if len(set.ImageServices) > 5 {
		set.Error = types.NewErrorMeta("400",
			fmt.Sprintf("the number of Image Services cannot be greater than %d", 5))
		return
	}

	for _, v := range set.ImageServices {

		if v.Driver != inapi.PodSpecBoxImageDocker &&
			v.Driver != inapi.PodSpecBoxImagePouch {
			set.Error = types.NewErrorMeta("400", fmt.Sprintf("Invalid ImageService Driver (%s)", v.Driver))
			return
		}

		if _, err := url.ParseRequestURI(v.Url); err != nil {
			set.Error = types.NewErrorMeta("400", fmt.Sprintf("Invalid ImageService URL (%s)", v.Url))
			return
		}
	}

	vpcCheck := func(name, ipnet string, min, max int) error {
		ipv4Addr, ipv4Net, err := net.ParseCIDR(ipnet)
		if err != nil {
			return fmt.Errorf("Invalid Network VPC %s IP (%s)", name, err.Error())
		}
		siz, _ := ipv4Net.Mask.Size()
		if siz < min || siz > max {
			return fmt.Errorf("Invalid Network VPC %s IP-Net mask size (%d)", name, siz)
		}
		if err := inapi.PrivateIPValid(ipv4Addr.String()); err != nil {
			return fmt.Errorf("Invalid Network VPC %s Private IP (%s)", name, err.Error())
		}
		return nil
	}

	if set.NetworkVpcBridge != "" {
		if err := vpcCheck("Bridge", set.NetworkVpcBridge, 16, 24); err != nil {
			set.Error = types.NewErrorMeta("400", err.Error())
			return
		}
	}

	if set.NetworkVpcInstance != "" {
		if err := vpcCheck("Instance", set.NetworkVpcInstance, 8, 16); err != nil {
			set.Error = types.NewErrorMeta("400", err.Error())
			return
		}
	}

	set.NetworkDomainName = strings.TrimSpace(strings.ToLower(set.NetworkDomainName))
	if set.NetworkDomainName != "" && !inapi.ResNetworkDomainNameRE.MatchString(set.NetworkDomainName) {
		set.Error = types.NewErrorMeta("400", fmt.Sprintf("Invalid Network Domain Name (%s)", set.NetworkDomainName))
		return
	}

	//
	groups, groupms := []*inapi.ZoneGroupSpec{}, map[string]bool{}
	for _, g := range set.Groups {
		g.Id = strings.ToLower(g.Id)
		if !inapi.ZoneGroupIdRX.MatchString(g.Id) {
			set.Error = types.NewErrorMeta("400", fmt.Sprintf("Invalid Group ID (%s)", g.Id))
			return
		}
		if groupms[g.Id] == true {
			continue
		}
		if g.Action != inapi.ZoneGroupSetupIn {
			g.Action = inapi.ZoneGroupSetupOut
		}
		groups = append(groups, g)
		groupms[g.Id] = true
	}
	set.Groups = groups

	if rs := data.DataGlobal.NewReader(inapi.NsGlobalSysZone(set.Meta.Id)).Exec(); rs.OK() {
		var prev inapi.ResZone
		if err := rs.Item().JsonDecode(&prev); err == nil {
			if prev.Meta.Created > 0 {
				set.Meta.Created = prev.Meta.Created
			}
			for _, g := range prev.Groups {
				if groupms[g.Id] == true {
					continue
				}
				g.Action = inapi.ZoneGroupSetupOut
				set.Groups = append(set.Groups, g)
				groupms[g.Id] = true
			}
		}
	}

	if set.Meta.Created == 0 {
		set.Meta.Created = uint64(types.MetaTimeNow())
	}

	set.Meta.Updated = uint64(types.MetaTimeNow())

	data.DataGlobal.NewWriter(inapi.NsGlobalSysZone(set.Meta.Id), set.ResZone).Exec()

	if inapi.OpActionAllow(set.Phase, inapi.OpActionDestroy) &&
		inapi.OpActionAllow(set.Phase, inapi.OpActionForce) {
		if err := data.SysZoneDelete(set.Meta.Id, &set.ResZone); err != nil {
			set.Error = types.NewErrorMeta("500", "Server Error : "+err.Error())
		} else {
			set.Kind = "HostZone"
		}
		status.GlobalZoneDel(set.Meta.Id)
		return
	}

	set.Kind = "HostZone"
}

func (c Host) ZoneAccChargeKeyRefreshAction() {

	var set inapi.GeneralObject
	defer c.RenderJson(&set)

	var (
		zone_id = c.Params.Value("zone_id")
		zone    inapi.ResZone
	)

	if zone_id != status.Zone.Meta.Id {
		set.Error = types.NewErrorMeta("400", "Zone Not Found")
		return
	}

	if rs := data.DataGlobal.NewReader(inapi.NsGlobalSysZone(zone_id)).Exec(); rs.OK() {
		rs.Item().JsonDecode(&zone)
	}

	if zone.Meta.Id != zone_id {
		set.Error = types.NewErrorMeta("400", "Zone Not Found")
		return
	}

	init_akacc := hauth.AccessKey{
		User: "sysadmin",
		Id: "00" + idhash.HashToHexString(
			[]byte(fmt.Sprintf("sys/zone/iam_acc_charge/ak/%s", zone_id)), 14),
		Secret: idhash.RandBase64String(40),
		Scopes: []*hauth.ScopeFilter{
			{
				Name:  "sys/zm",
				Value: zone_id,
			},
		},
		Description: "ZoneMaster AccCharge",
	}
	if err := iam_db.AccessKeyReset(&init_akacc); err != nil {
		set.Error = types.NewErrorMeta("500", "database/iam error "+err.Error())
		return
	}

	zone.OptionSet("iam/acc_charge/access_key", init_akacc.Id)
	zone.OptionSet("iam/acc_charge/secret_key", init_akacc.Secret)

	if rs := data.DataGlobal.NewWriter(inapi.NsGlobalSysZone(zone_id), zone).Exec(); !rs.OK() {
		set.Error = types.NewErrorMeta("500", "database/global error "+rs.ErrorMessage())
		return
	}

	if zone_id == status.ZoneId {
		if rs := data.DataZone.NewWriter(inapi.NsZoneSysZone(zone_id), zone).Exec(); !rs.OK() {
			set.Error = types.NewErrorMeta("500", "database/zone error "+rs.ErrorMessage())
			return
		}
	}

	hlog.Printf("warn", "ops/zone/acc-charge/key/reset %s, %s...",
		init_akacc.Id, init_akacc.Secret[:20])

	status.Zone = &zone

	set.Kind = "Zone"
}
