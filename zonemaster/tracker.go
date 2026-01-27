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

package zonemaster

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hooto/hlog4g/hlog"
	"github.com/lessos/lessgo/crypto/idhash"
	"github.com/lessos/lessgo/types"

	"github.com/sysinner/incore/config"
	"github.com/sysinner/incore/data"
	"github.com/sysinner/incore/inapi"
	"github.com/sysinner/incore/status"
	inapi2 "github.com/sysinner/incore/v2/inapi"

	hauth1 "github.com/hooto/hauth/go"
	hauth2 "github.com/hooto/hauth/go/v2"
	iamdata "github.com/hooto/iam/data"
	"github.com/lynkdb/lynkapi/go/lynkapi"
)

var (
	zmPodOperateHangTimeout uint32 = 86400 * 10
	zmReadyRefreshed        int64  = 0
	zmAccessKeySetup               = false
)

func zoneTracker() {

	// is zone-master
	if !status.IsZoneMaster() {
		return
	}

	// refresh host keys
	if len(status.ZoneHostSecretKeys) == 0 {

		offset := inapi.NsZoneSysHostSecretKey(status.ZoneId, "")
		if rs := data.DataZone.NewRanger(offset, offset).
			SetLimit(1000).Exec(); rs.OK() {

			for _, v := range rs.Items {
				status.ZoneHostSecretKeys.Set(
					inapi.NsKeyPathLastName(v.Key), v.StringValue())
			}

		} else {
			hlog.Printf("warn", "refresh host key list failed")
		}
	}

	/**
	// refresh zone-master leader ttl
	if rs := data.DataZone.NewWriter(
		inapi.NsKvZoneSysMasterLeader(status.ZoneId), status.Host.Meta.Id).
		SetTTL(12000).Exec(); !rs.OK() {
		hlog.Printf("warn", "zm/zone-master/leader ttl refresh failed "+rs.ErrorMessage())
		return
	}
	*/

	zmWorkerSysConfigRefresh()

	//
	zmWorkerPodListStatusRefresh()

	tn := time.Now().Unix()

	//
	if (zmReadyRefreshed + 60) > tn {
		hlog.Printf("debug", "zm/refresh SKIP")
		return
	}

	//
	if err := zmWorkerGlobalZoneListRefresh(); err != nil {
		hlog.Printf("warn", "zm/global-zone-list/refresh error %s", err.Error())
		return
	}

	//
	if err := zmWorkerZoneAccessKeySetup(); err != nil {
		hlog.Printf("warn", "zm/zone-access-key/setup error %s", err.Error())
		return
	}

	// refresh zone-master list
	if err := zmWorkerZoneMasterListRefresh(); err != nil {
		hlog.Printf("warn", "zm/master-list/refresh error %s", err.Error())
		return
	}

	// refresh host keys
	if err := zmWorkerZoneHostKeyListRefresh(); err != nil {
		hlog.Printf("warn", "zm/host-key-list/refresh error %s", err.Error())
		return
	}

	// refresh host list
	if err := zmWorkerZoneHostListRefresh(); err != nil {
		hlog.Printf("warn", "refresh host list err %s", err.Error())
		return
	}

	zmReadyRefreshed = tn
}

func zmWorkerZoneAccessKeySetup() error {

	if zmAccessKeySetup {
		return nil
	}

	if status.Zone == nil {
		return fmt.Errorf("Zone Not Setup")
	}

	if config.Config.ZoneMain.IamAccessKey == nil {

		akId := "00" + idhash.HashToHexString(
			[]byte(fmt.Sprintf("sys/zone/iam_acc_charge/ak/%s", status.ZoneId)), 14)

		config.Config.ZoneMain.IamAccessKey = iamdata.KeyMgr.KeyGet(akId)

		if config.Config.ZoneMain.IamAccessKey == nil {

			config.Config.ZoneMain.IamAccessKey = &hauth1.AccessKey{
				Id: akId,
				Secret: idhash.HashToBase64String(
					idhash.AlgSha256, []byte(config.Config.Host.SecretKey), 40),
				User: "sysadmin",
				Type: "App",
				Scopes: []*hauth1.ScopeFilter{
					{
						Name:  "sys/zm",
						Value: status.ZoneId,
					},
				},
				Description: "ZoneMaster AccCharge",
			}

			hlog.Printf("warn", "zone #%s, init iam/acc_charge/key, access_key id %s, secret %s...",
				status.ZoneId, config.Config.ZoneMain.IamAccessKey.Id, config.Config.ZoneMain.IamAccessKey.Secret[:8])

			iamdata.AccessKeyInitData(config.Config.ZoneMain.IamAccessKey)
			config.Config.Flush()
		}
	}

	configFlush := false

	// config.Config.ZoneMain.SysAccessKeys = nil
	if len(config.Config.ZoneMain.SysAccessKeys) == 0 {

		offset := inapi2.NsZoneSysAccessKey(status.ZoneId, "")
		if rs := data.DataZone.NewRanger(offset, offset).
			SetLimit(1000).Exec(); rs.OK() {

			for _, v := range rs.Items {
				var ak hauth1.AccessKey
				if err := v.JsonDecode(&ak); err != nil {
					continue
				}
				if len(ak.Scopes) == 0 {
					ak.Scopes = append(ak.Scopes, inapi2.AuthPermSysAll)
					data.DataZone.NewWriter(inapi2.NsZoneSysAccessKey(status.ZoneId, ak.Id), ak).Exec()
				}
				if prev := lynkapi.SlicesSearchFunc(config.Config.ZoneMain.SysAccessKeys, func(a *hauth1.AccessKey) bool {
					return ak.Id == a.Id
				}); prev == nil {
					config.Config.ZoneMain.SysAccessKeys = append(config.Config.ZoneMain.SysAccessKeys, &ak)
					config.KeyMgr.KeySet(&ak)
					configFlush = true
				} else if len(prev.Scopes) == 0 {
					prev.Scopes = ak.Scopes
					configFlush = true
				}
				hlog.Printf("info", "zone sys-access-key %s", ak.Id)
			}
		}

		if len(config.Config.ZoneMain.SysAccessKeys) == 0 {
			ak := hauth2.NewAppAccessKey()
			ak.User = "sysadmin"
			ak.Scopes = append(ak.Scopes, inapi2.AuthPermSysAll)
			rs := data.DataZone.NewWriter(inapi2.NsZoneSysAccessKey(status.ZoneId, ak.Id), ak).Exec()
			if rs.OK() {
				config.Config.ZoneMain.SysAccessKeys = append(config.Config.ZoneMain.SysAccessKeys, ak)
				config.KeyMgr.KeySet(ak)
				configFlush = true
				hlog.Printf("info", "zone sys-access-key %s", ak.Id)
			}
		}
	}

	if configFlush {
		config.Config.Flush()
	}

	zmAccessKeySetup = true

	return nil
}

// refresh global zones
func zmWorkerGlobalZoneListRefresh() error {

	offset := inapi.NsGlobalSysZone("")
	rs := data.DataGlobal.NewRanger(offset, offset).
		SetLimit(50).Exec()
	if !rs.OK() {
		return errors.New("db/scan error")
	}

	for _, v := range rs.Items {

		var zone inapi.ResZone
		if err := v.JsonDecode(&zone); err != nil {
			continue
		}

		pZone := status.GlobalZone(zone.Meta.Id)
		chg := false
		if pZone == nil {
			pZone, chg = status.GlobalZoneSync(&zone)
		} else if zone.Meta.Updated > pZone.Meta.Updated {
			chg = true
		}

		//
		offset := inapi.NsGlobalSysCell(zone.Meta.Id, "")

		if rs := data.DataGlobal.NewRanger(offset, offset).
			SetLimit(100).Exec(); rs.OK() {

			for _, v2 := range rs.Items {
				var cell inapi.ResCell
				if err := v2.JsonDecode(&cell); err == nil {
					if _, chg2 := pZone.CellSync(&cell); chg2 {
						chg = true
					}
					// hlog.Printf("info", "cell %s : %s", cell.Meta.Id, cell.Meta.Name)
				}
			}
		}

		if pZone.Meta.Id != status.ZoneId {
			continue
		}

		if chg || status.Zone == nil {
			status.Zone = pZone
			data.DataZone.NewWriter(inapi.NsZoneSysZone(status.ZoneId), pZone).Exec()
			hlog.Printf("info", "zonemaster status/zone refreshed %s", status.ZoneId)
		}
	}

	return nil
}

func zmWorkerZoneHostListRefresh() error {

	if err := status.ZoneNetworkManager.ZoneSetup(
		status.Zone.NetworkVpcBridge,
		status.Zone.NetworkVpcInstance); err != nil {
		return err
	}

	rs := data.DataZone.NewRanger(
		inapi.NsZoneSysHost(status.ZoneId, ""),
		inapi.NsZoneSysHost(status.ZoneId, "")).
		SetLimit(1000).Exec()
	if !rs.OK() {
		hlog.Printf("warn", "refresh host list failed")
		return errors.New("db/scan error")
	}

	cellCount := map[string]int32{}

	for _, v := range rs.Items {

		var o inapi.ResHost
		if err := v.JsonDecode(&o); err != nil {
			hlog.Printf("error", "refresh host list %s", err.Error())
			continue
		}

		if o.Operate == nil {
			o.Operate = &inapi.ResHostOperate{}
		} else {
			// o.Operate.PortUsed.Clean()
		}
		cellCount[o.Operate.CellId]++

		//
		{

		}

		status.ZoneHostList.Sync(o)

		//
		if gn := status.GlobalHostList.Item(o.Meta.Id); gn == nil {
			if rs := data.DataGlobal.NewReader(inapi.NsGlobalSysHost(o.Operate.ZoneId, o.Meta.Id)).Exec(); rs.NotFound() {
				data.DataGlobal.NewWriter(inapi.NsGlobalSysHost(o.Operate.ZoneId, o.Meta.Id), o).Exec()
			}
		}
		status.GlobalHostList.Sync(o)

		// hlog.Printf("info", "refresh host refresh %d", len(rss))
	}

	if len(cellCount) > 0 {

		for cellId, num := range cellCount {

			pCell := status.GlobalZoneCell(status.ZoneId, cellId)
			if pCell == nil || pCell.NodeNum == num {
				continue
			}
			pCell.Meta.Updated = uint64(types.MetaTimeNow())
			pCell.NodeNum = num

			if rs := data.DataGlobal.NewWriter(
				inapi.NsGlobalSysCell(status.ZoneId, cellId), pCell).Exec(); rs.OK() {
				data.DataZone.NewWriter(
					inapi.NsZoneSysCell(status.ZoneId, cellId), pCell).Exec()

				// hlog.Printf("info", "cell %s : %s", cellId, pCell.Meta.Name)
			}
		}
	}

	if !status.ZoneHostListImported {

		offset := inapi.NsZonePodInstance(status.ZoneId, "")
		for {
			rs := data.DataZone.NewRanger(offset, offset).
				SetLimit(1000).Exec()
			if !rs.OK() {
				break
			}

			for _, v := range rs.Items {

				offset = v.Key

				var pod inapi.Pod
				if err := v.JsonDecode(&pod); err != nil {
					continue
				}

				status.ZonePodList.Items.Set(&pod)

				if inapi.OpActionAllow(pod.Operate.Action, inapi.OpActionDestroy) {
					continue
				}

				for _, opRep := range pod.Operate.Replicas {

					host := status.ZoneHostList.Item(opRep.Node)
					if host == nil {
						continue
					}

					for _, v := range opRep.Ports {

						if v.HostPort == 0 {
							continue
						}

						if host.OpPortAlloc(v.HostPort) == 0 {
							continue
						}

						hlog.Printf("info", "zm/host:%s.operate.ports refreshed", host.Meta.Id)

						data.SysHostUpdate(status.ZoneId, host)
					}
				}
			}

			if !rs.NextResultSet {
				break
			}
		}
	}

	if len(status.ZoneHostList.Items) > 0 {
		status.ZoneHostListImported = true
	}

	for _, host := range status.ZoneHostList.Items {

		if host.Operate == nil ||
			host.Spec == nil || host.Spec.Capacity == nil {
			continue
		}

		peerIp := host.Spec.PeerLanAddr
		if n := strings.IndexByte(peerIp, ':'); n > 0 {
			peerIp = peerIp[:n]
		}

		if err := status.ZoneNetworkManager.HostSetup(host.Meta.Id, peerIp,
			host.Operate.NetworkVpcBridge, host.Operate.NetworkVpcInstance); err != nil {
			hlog.Printf("warn", "host %s network vpc refresh error %s",
				host.Meta.Id, err.Error())
			host.Operate.NetworkVpcBridge = ""
			host.Operate.NetworkVpcInstance = ""
		}
	}
	status.ZoneNetworkManager.Ready(true)

	// hlog.Printf("info", "zm/host-list %d refreshed", len(status.ZoneHostList.Items))
	return nil
}

func zmWorkerZoneHostKeyListRefresh() error {

	offset := inapi.NsZoneSysHostSecretKey(status.ZoneId, "")
	rs := data.DataZone.NewRanger(offset, offset).
		SetLimit(1000).Exec()
	if !rs.OK() {
		return errors.New("db/scan error")
	}

	for _, v := range rs.Items {
		status.ZoneHostSecretKeys.Set(
			inapi.NsKeyPathLastName(v.Key), v.StringValue())
	}

	return nil
}

func zmWorkerZoneMasterListRefresh() error {

	offset := inapi.NsZoneSysMasterNode(status.ZoneId, "")
	rs := data.DataZone.NewRanger(offset, offset).SetLimit(100).Exec()
	if !rs.OK() {
		return errors.New("db/scan error")
	}

	zms := inapi.ResZoneMasterList{
		Leader: status.Host.Meta.Id,
	}

	for _, v := range rs.Items {

		var o inapi.ResZoneMasterNode
		if err := v.JsonDecode(&o); err == nil {
			zms.Sync(o)
		}
	}

	if len(zms.Items) > 0 {
		status.ZoneMasterList.SyncList(&zms)
		return nil
	}

	return errors.New("No ZoneMasters Found")
}

var zmWorkerSysConfigRefreshed = uint32(0)

func zmWorkerSysConfigRefresh() {

	tn := uint32(time.Now().Unix())

	if (zmWorkerSysConfigRefreshed + 60) > tn {
		return
	}

	for _, v := range config.SysConfigurators {

		//
		if rs := data.DataGlobal.NewReader(inapi.NsGlobalSysConfig(v.Name)).Exec(); rs.OK() {
			var item inapi.SysConfigGroup
			if err := rs.Item().JsonDecode(&item); err == nil {
				status.ZoneSysConfigGroupList.Sync(&item)
			}
		}
	}

	zmWorkerSysConfigRefreshed = tn
	// hlog.Printf("info", "zm/sys-config/refresh %d", len(config.SysConfigurators))
}

// refresh pod's status
func zmWorkerPodListStatusRefresh() {

	tn := uint32(time.Now().Unix())

	for _, pod := range status.ZonePodList.Items {

		if pod.Spec == nil || pod.Spec.Zone != status.ZoneId {
			continue
		}

		if pod.Operate.ReplicaCap < 1 {
			continue
		}

		var (
			podSync       = false
			podStatusKey  = inapi.NsKvZonePodStatus(status.ZoneId, pod.Meta.ID)
			podStatusKeyG = inapi.NsKvGlobalPodStatus(status.ZoneId, pod.Meta.ID)
			podStatus     = status.ZonePodStatusList.Get(pod.Meta.ID)
		)

		if podStatus == nil {
			if rs := data.DataZone.NewReader(podStatusKey).Exec(); rs.OK() {
				var item inapi.PodStatus
				if err := rs.Item().JsonDecode(&item); err == nil {

					podStatus = &item
				}
			} else if rs.NotFound() {
				podStatus = &inapi.PodStatus{
					PodId: pod.Meta.ID,
				}
			}
			if podStatus == nil {
				continue
			}
			status.ZonePodStatusList.Set(podStatus)
		}

		if podStatus.PodId == "" {
			podStatus.PodId = pod.Meta.ID
		}

		for _, repId := range pod.Operate.ExpMigrates {

			if repId >= uint32(pod.Operate.ReplicaCap) {
				continue
			}

			ctrRep := pod.Operate.Replicas.Get(repId)
			if ctrRep == nil {
				continue
			}

			if ctrRep.Next == nil {
				continue
			}

			repStatus := podStatus.RepGet(repId)
			if repStatus == nil {
				continue
			}

			// logicfix
			if inapi.OpActionAllow(ctrRep.Action, inapi.OpActionMigrate) {

				host := status.ZoneHostList.Item(ctrRep.Node)
				if host != nil {

					var (
						hostPeer = inapi.HostNodeAddress(host.Spec.PeerLanAddr)
						addr     = fmt.Sprintf("%s:%d", hostPeer.IP(), hostPeer.Port()+5)
					)

					if opt, ok := ctrRep.Options.Get("rsync/host"); ok && addr != opt.String() {

						hlog.Printf("warn", "zm/tracker rep %s:%d, set addr from %s to %s",
							pod.Meta.ID, repId,
							opt.String(), addr,
						)

						ctrRep.Options.Set("rsync/host", addr)
						if ctrRep.Next != nil {
							ctrRep.Next.Options.Set("rsync/host", addr)
						}
					}
				}
			}
		}

		// inapi.ObjPrint(pod.Meta.ID, v)

		if len(pod.Operate.OpLog) > 0 {
			podStatus.OpLog = pod.Operate.OpLog
		}

		podStatus.Updated = uint32(time.Now().Unix())
		podStatus.ActionRunning = 0

		repStatusOuts := []uint32{}

		for _, repStatus := range podStatus.Replicas {

			ctrlRep := pod.Operate.Replicas.Get(repStatus.RepId)

			if repStatus.RepId >= uint32(pod.Operate.ReplicaCap) {
				if ctrlRep == nil {
					repStatusOuts = append(repStatusOuts, repStatus.RepId)
				}
				continue
			}

			if inapi.OpActionAllow(repStatus.Action, inapi.OpActionRunning) {
				podStatus.ActionRunning += 1
			}

			if ctrlRep == nil {
				continue
			}

			if opv := inapi.OpActionDesire(ctrlRep.Action, repStatus.Action); opv > 0 {

				if !inapi.OpActionAllow(ctrlRep.Action, opv) {

					hlog.Printf("info", "zm/tracker rep %s:%d, action %s, status %s",
						pod.Meta.ID, repStatus.RepId,
						strings.Join(inapi.OpActionStrings(ctrlRep.Action), ","),
						strings.Join(inapi.OpActionStrings(repStatus.Action), ","),
					)

					// merge rep status's action to control's action
					ctrlRep.Action = ctrlRep.Action | opv

					if inapi.OpActionAllow(ctrlRep.Action, inapi.OpActionDestroy) {
						//
					} else if inapi.OpActionAllow(ctrlRep.Action, inapi.OpActionStart) {
						ctrlRep.Action = inapi.OpActionRemove(ctrlRep.Action, inapi.OpActionStop)
						ctrlRep.Action = inapi.OpActionRemove(ctrlRep.Action, inapi.OpActionStopped)
					} else if inapi.OpActionAllow(ctrlRep.Action, inapi.OpActionStop) {
						ctrlRep.Action = inapi.OpActionRemove(ctrlRep.Action, inapi.OpActionStart)
						ctrlRep.Action = inapi.OpActionRemove(ctrlRep.Action, inapi.OpActionRunning)
					}

					ctrlRep.Updated = tn
					podSync = true
				}
			}
		}

		for _, repId := range repStatusOuts {
			podStatus.RepDel(repId)
			hlog.Printf("info", "zm/rep %s:%d, status out", pod.Meta.ID, repId)
		}

		if rs := data.DataZone.NewWriter(podStatusKey, podStatus).Exec(); !rs.OK() {
			continue
		}

		if rs := data.DataGlobal.NewWriter(podStatusKeyG, podStatus).Exec(); !rs.OK() {
			continue
		}

		if podSync {
			data.DataZone.NewWriter(inapi.NsZonePodInstance(status.ZoneId, pod.Meta.ID), pod).Exec()
			// hlog.Printf("info", "pod %s operate db-sync", pod.Meta.ID)
		}

		// inapi.ObjPrint(pod.Meta.ID, podStatus)
		// inapi.ObjPrint(pod.Meta.ID, v)
	}

}
