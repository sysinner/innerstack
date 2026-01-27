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

package data

import (
	"errors"

	"github.com/hooto/hauth/go"
	"github.com/hooto/hlog4g/hlog"
	"github.com/hooto/htoml4g/htoml"

	// "github.com/lynkdb/kvgo"
	kvclient "github.com/lynkdb/kvgo/v2/pkg/client"
	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
	"github.com/lynkdb/kvgo/v2/pkg/kvrep"
	"github.com/lynkdb/kvgo/v2/pkg/storage"
	"github.com/lynkdb/lynkapi/go/lynkapi"

	// kv2 "github.com/lynkdb/kvspec/v2/go/kvspec"

	"github.com/sysinner/incore/config"
	// "github.com/sysinner/incore/inapi"
)

var (
	// dbLocal kv2.Client
	// dbZone  kv2.Client

	// prevDataLocal kv2.ClientTable
	DataLocal kvapi.Client

	// prevDataZone kv2.ClientTable
	DataZone kvapi.Client

	// prevDataPack kv2.ClientTable
	DataPack kvapi.Client

	// prevDataGlobal kv2.ClientTable
	DataGlobal kvapi.Client

	err error
)

func Setup() error {

	if err := setupLocal(); err != nil {
		return err
	}

	return nil
}

func setupLocal() error {

	//// if prevDataLocal == nil {

	//// 	cfgLocal := &kvgo.Config{
	//// 		Storage: kvgo.ConfigStorage{
	//// 			DataDirectory: config.Prefix + "/var/db_local",
	//// 		},
	//// 	}

	//// 	cn, err := kvgo.Open(cfgLocal)
	//// 	if err != nil {
	//// 		return err
	//// 	}
	//// 	dbLocal, err = cn.NewClient()
	//// 	if err != nil {
	//// 		return err
	//// 	}
	//// 	prevDataLocal = dbLocal.OpenTable("main")
	//// }

	if DataLocal == nil {
		if c, err := kvrep.NewReplica(&storage.Options{
			DataDirectory: config.Prefix + "/var/inlocal",
		}); err != nil {
			return err
		} else {
			DataLocal = c
		}
	}

	return nil
}

func setupZone() error {

	if !config.IsZoneMaster() {
		return nil
	}

	if config.Config.ZoneMain == nil {
		return errors.New("no zone setup")
	}

	//// if config.Config.ZoneData == nil {

	//// 	config.Config.ZoneData = &kvgo.Config{
	//// 		Storage: kvgo.ConfigStorage{
	//// 			DataDirectory: config.Prefix + "/var/db_zone",
	//// 		},
	//// 	}

	//// 	if err = config.Config.Flush(); err != nil {
	//// 		return err
	//// 	}
	//// }

	//// cn, err := kvgo.Open(config.Config.ZoneData)
	//// if err != nil {
	//// 	return err
	//// }

	//// dbZone, err = cn.NewClient()
	//// if err != nil {
	//// 	return err
	//// }

	//// dbinit := func(name string) error {
	//// 	req := kv2.NewSysCmdRequest("TableSet", &kv2.TableSetRequest{
	//// 		Name: name,
	//// 		Desc: "innerstack " + name,
	//// 	})

	//// 	rs := dbZone.Connector().SysCmd(req)
	//// 	if !rs.OK() {
	//// 		return rs.Error()
	//// 	}
	//// 	hlog.Printf("info", "db init table setup %s ok", name)
	//// 	return nil
	//// }

	flush := false

	//
	//// if prevDataGlobal == nil {
	//// 	if config.Config.ZoneMain.DataTableGlobal == "" {
	//// 		if err := dbinit("global"); err != nil {
	//// 			return err
	//// 		}
	//// 		config.Config.ZoneMain.DataTableGlobal = "global"
	//// 		flush = true
	//// 	}
	//// 	prevDataGlobal = dbZone.OpenTable(config.Config.ZoneMain.DataTableGlobal)
	//// 	UpgradeGlobalData(prevDataGlobal)
	//// }

	//// //
	//// if prevDataZone == nil {
	//// 	if config.Config.ZoneMain.DataTableZone == "" {
	//// 		if err := dbinit("zone"); err != nil {
	//// 			return err
	//// 		}
	//// 		config.Config.ZoneMain.DataTableZone = "zone"
	//// 		flush = true
	//// 	}
	//// 	prevDataZone = dbZone.OpenTable(config.Config.ZoneMain.DataTableZone)
	//// }

	//// //
	//// if prevDataPack == nil {
	//// 	if config.Config.ZoneMain.DataTableInpack == "" {
	//// 		if err := dbinit("inpack"); err != nil {
	//// 			return err
	//// 		}
	//// 		config.Config.ZoneMain.DataTableInpack = "inpack"
	//// 		flush = true
	//// 	}
	//// 	prevDataPack = dbZone.OpenTable(config.Config.ZoneMain.DataTableInpack)
	//// }

	// auto init database setting
	if config.Config.GlobDatabase == nil ||
		config.Config.ZoneDatabase == nil ||
		config.Config.PackDatabase == nil {

		var kvConfig struct {
			Server struct {
				Bind      string           `toml:"bind"`
				AccessKey *hauth.AccessKey `toml:"access_key"`
			} `toml:"server"`
		}

		if err := htoml.DecodeFromFile("/opt/lynkdb/kvgo/etc/kvgo-server.toml", &kvConfig); err != nil {
			return err
		}

		if kvConfig.Server.AccessKey == nil {
			return errors.New("kv database config not setup")
		}

		cc := &kvclient.Config{
			Addr: kvConfig.Server.Bind,
			AccessKey: &hauth.AccessKey{
				Id:     kvConfig.Server.AccessKey.Id,
				Secret: kvConfig.Server.AccessKey.Secret,
			},
		}

		ac, err := cc.NewAdminClient()
		if err != nil {
			return err
		}

		if config.Config.GlobDatabase == nil {
			req := lynkapi.NewRequest("Admin", "DatabaseCreate", &kvapi.DatabaseCreateRequest{
				Name: "inglob",
			})
			if rs := ac.Exec(req); !rs.OK() && rs.StatusCode() != lynkapi.StatusCode_Conflict {
				return rs.Err()
			}
			config.Config.GlobDatabase = &kvclient.Config{
				Database:  "inglob",
				Addr:      kvConfig.Server.Bind,
				AccessKey: kvConfig.Server.AccessKey,
			}
		}

		if config.Config.ZoneDatabase == nil {
			req := lynkapi.NewRequest("Admin", "DatabaseCreate", &kvapi.DatabaseCreateRequest{
				Name: "inzone",
			})
			if rs := ac.Exec(req); !rs.OK() && rs.StatusCode() != lynkapi.StatusCode_Conflict {
				return rs.Err()
			}
			config.Config.ZoneDatabase = &kvclient.Config{
				Database:  "inzone",
				Addr:      kvConfig.Server.Bind,
				AccessKey: kvConfig.Server.AccessKey,
			}
		}

		if config.Config.PackDatabase == nil {
			req := lynkapi.NewRequest("Admin", "DatabaseCreate", &kvapi.DatabaseCreateRequest{
				Name: "inpack",
			})
			if rs := ac.Exec(req); !rs.OK() && rs.StatusCode() != lynkapi.StatusCode_Conflict {
				return rs.Err()
			}
			config.Config.PackDatabase = &kvclient.Config{
				Database:  "inpack",
				Addr:      kvConfig.Server.Bind,
				AccessKey: kvConfig.Server.AccessKey,
			}
		}

		flush = true
	}

	{
		// upgrade ...
		if DataGlobal, err = config.Config.GlobDatabase.NewClient(); err != nil {
			hlog.Printf("info", "glob database new client err %s", err.Error())
			return err
		} else {
			hlog.Printf("info", "glob database client setup ok")
		}

		if DataZone, err = config.Config.ZoneDatabase.NewClient(); err != nil {
			hlog.Printf("info", "zone database new client err %s", err.Error())
			return err
		} else {
			hlog.Printf("info", "zone database client setup ok")
		}

		if DataPack, err = config.Config.PackDatabase.NewClient(); err != nil {
			hlog.Printf("info", "inpack database new client err %s", err.Error())
			return err
		} else {
			hlog.Printf("info", "inpack database client setup ok")
		}
	}

	// upgrade ...
	dataUpgrade2()

	if true {
		// dataTransfer(DataGlobal, prevDataGlobal, "inglob")
	}

	if true {
		// dataTransfer(DataZone, prevDataZone, "inzone")
	}

	if true {
		// go dataTransfer(DataPack, prevDataPack, "inpack")
	}

	if flush {
		if err = config.Config.Flush(); err != nil {
			return err
		}
	}

	return nil
}

//// func UpgradeGlobalData(data kv2.ClientTable) error {
////
//// 	if data == nil {
//// 		return errors.New("Upgrade Global Data kv2.ClientConnector Not Found")
//// 	}
////
//// 	rs := data.NewReader(nil).KeyRangeSet(
//// 		inapi.NsGlobalAppSpec(""), inapi.NsGlobalAppSpec("zzzz")).
//// 		LimitNumSet(200).Query()
////
//// 	for _, v := range rs.Items {
////
//// 		var spec inapi.AppSpec
//// 		if err := v.Decode(&spec); err != nil {
//// 			continue
//// 		}
//// 		var specPrev inapi.AppSpecPrev
//// 		if err := v.Decode(&specPrev); err != nil {
//// 			continue
//// 		}
////
//// 		prekey := inapi.NsKvGlobalAppSpecVersion(spec.Meta.ID, "")
////
//// 		rs2 := data.NewReader(nil).KeyRangeSet(
//// 			prekey, append(prekey, 0xff)).LimitNumSet(200).Query()
//// 		if rs2.OK() {
////
//// 			for _, v2 := range rs2.Items {
////
//// 				k := strings.TrimPrefix(string(v2.Meta.Key), string(prekey))
//// 				if len(k) > 24 {
//// 					continue
//// 				}
////
//// 				var pspec inapi.AppSpec
//// 				if err := v2.Decode(&pspec); err != nil {
//// 					continue
//// 				}
////
//// 				if rs3 := data.NewWriter(inapi.NsKvGlobalAppSpecVersion(spec.Meta.ID, pspec.Meta.Version), pspec).Commit(); rs3.OK() {
////
//// 					data.NewWriter(v2.Meta.Key, nil, nil).ModeDeleteSet(true).Commit()
//// 				}
//// 			}
//// 		}
////
//// 		if specPrev.Meta.Version != spec.Meta.Version {
//// 			data.NewWriter(inapi.NsGlobalAppSpec(spec.Meta.ID), spec).Commit()
//// 			hlog.Printf("info", "AppSpec %s version %s -> %s",
//// 				spec.Meta.ID, specPrev.Meta.Version, spec.Meta.Version)
//// 		}
//// 	}
////
//// 	return nil
//// }

func Close() error {

	// for _, db := range []kv2.Client{
	// 	dbLocal,
	// 	dbZone,
	// } {
	// 	if db != nil {
	// 		db.Close()
	// 	}
	// }

	return nil
}

//// func dataTransfer(dst kvapi.Client, src kv2.ClientTable, desc string) error {
////
//// 	var (
//// 		offset = []byte{0x00}
//// 		cutset = []byte{0xff}
////
//// 		tn = time.Now().UnixNano() / 1e6
////
//// 		statsReqN  int
//// 		statsTtlN  int
//// 		statsIncN  int
//// 		statsHitN  int
//// 		statsHitOK int
//// 		statsHitER int
////
//// 		statsSize int64
//// 	)
////
//// 	for {
//// 		rs := src.NewReader(nil).KeyRangeSet(offset, cutset).
//// 			LimitNumSet(200).Query()
//// 		statsReqN += 1
//// 		for _, v := range rs.Items {
////
//// 			offset = v.Meta.Key
//// 			statsHitN += 1
////
//// 			if len(v.Data.Value) <= 1 || (v.Data.Value[0] != 0 && v.Data.Value[0] != 2) {
//// 				statsHitER += 1
//// 				continue
//// 			}
////
//// 			statsSize += int64(len(v.Data.Value))
////
//// 			wr := dst.NewWriter(v.Meta.Key, v.Data.Value[1:]).SetCreateOnly(true)
////
//// 			if v.Meta.Expired > 0 {
////
//// 				statsTtlN += 1
////
//// 				ttl := int64(v.Meta.Expired) - tn
////
//// 				if ttl > 0 {
//// 					wr.SetTTL(ttl)
//// 				}
////
//// 				hlog.Printf("info", "transfer %s, key %s, val %d, TTL %d sec",
//// 					desc, string(v.Meta.Key), len(v.Data.Value), ttl/1e3)
//// 			}
////
//// 			if v.Meta.IncrId > 0 {
//// 				wr.SetIncr(v.Meta.IncrId, "")
//// 				statsIncN += 1
////
//// 				hlog.Printf("info", "transfer %s, key %s, val %d, Incr %d",
//// 					desc, string(v.Meta.Key), len(v.Data.Value), v.Meta.IncrId)
//// 			}
////
//// 			if rs2 := wr.Exec(); rs2.OK() {
//// 				statsHitOK += 1
//// 				hlog.Printf("info", "transfer %s, key %s, val %d, version %d",
//// 					desc, string(v.Meta.Key), len(v.Data.Value), rs2.Meta().Version)
//// 			} else {
//// 				statsHitER += 1
//// 				hlog.Printf("info", "transfer %s, key %s, err %s", desc, string(v.Meta.Key), rs2.ErrorMessage())
//// 			}
//// 		}
////
//// 		if len(rs.Items) < 1 {
//// 			break
//// 		}
//// 	}
////
//// 	hlog.Printf("info", "transfer %s, reqs %d, keyd %d/%d/%d, ttl %d, incr %d, size %d",
//// 		desc, statsReqN, statsHitOK, statsHitER, statsHitN, statsTtlN, statsIncN, statsSize)
////
//// 	return nil
//// }
