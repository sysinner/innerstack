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

package kvgo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hooto/hlog4g/hlog"
	ps_disk "github.com/shirou/gopsutil/disk"
	ps_mem "github.com/shirou/gopsutil/mem"
	"github.com/valuedig/apis/go/tsd/v1"

	kv2 "github.com/lynkdb/kvspec/go/kvspec/v2"
)

func (cn *Conn) workerLocal() {
	cn.workmu.Lock()
	if cn.workerLocalRunning {
		cn.workmu.Unlock()
		return
	}
	cn.workerLocalRunning = true
	cn.workmu.Unlock()

	go cn.workerLocalReplicaOfRefresh()

	for !cn.close {

		if err := cn.workerLocalExpiredRefresh(); err != nil {
			hlog.Printf("warn", "local ttl clean err %s", err.Error())
		}

		if err := cn.workerLocalTableRefresh(); err != nil {
			hlog.Printf("warn", "local table refresh err %s", err.Error())
		}

		if err := cn.workerLocalSysStatusRefresh(); err != nil {
			hlog.Printf("warn", "local sys-status refresh err %s", err.Error())
		}

		time.Sleep(workerLocalExpireSleep)
	}
}

func (cn *Conn) workerLocalReplicaOfRefresh() {

	hlog.Printf("info", "replica-of servers %d", len(cn.opts.Cluster.ReplicaOfNodes))

	for !cn.close {
		time.Sleep(workerReplicaLogAsyncSleep)

		if err := cn.workerLocalReplicaOfLogAsync(); err != nil {
			hlog.Printf("warn", "replica-of log async err %s", err.Error())
		}
	}
}

func (cn *Conn) workerLocalExpiredRefresh() error {

	for _, t := range cn.tables {
		if err := cn.workerLocalExpiredRefreshTable(t); err != nil {
			hlog.Printf("warn", "cluster ttl refresh error %s", err.Error())
		}
	}

	return nil
}

func (cn *Conn) workerLocalExpiredRefreshTable(dt *dbTable) error {

	tn := timems()

	if tn < dt.expiredSync(0) {
		return nil
	}

	var (
		offset = keyEncode(nsKeyTtl, uint64ToBytes(0))
		cutset = keyEncode(nsKeyTtl, uint64ToBytes(uint64(tn)))
		iter   = dt.db.NewIterator(&kv2.StorageIteratorRange{
			Start: offset,
			Limit: cutset,
		})
		ok bool
	)
	defer iter.Release()

	for !cn.close {

		batch := dt.db.NewBatch()

		for ok = iter.First(); ok && !cn.close; ok = iter.Next() {

			if bytes.Compare(iter.Key(), offset) <= 0 {
				continue
			}

			if bytes.Compare(iter.Key(), cutset) > 0 {
				break
			}

			meta, err := kv2.ObjectMetaDecode(bytesClone(iter.Value()))
			if err != nil {
				hlog.Printf("warn", "db err %s", err.Error())
				break
			}

			ss := dt.db.Get(keyEncode(nsKeyMeta, meta.Key), nil)
			if ss.OK() {

				cmeta, err := kv2.ObjectMetaDecode(ss.Bytes())
				if err == nil && cmeta.Version == meta.Version {
					batch.Delete(keyEncode(nsKeyMeta, meta.Key))
					batch.Delete(keyEncode(nsKeyData, meta.Key))
					batch.Delete(keyEncode(nsKeyLog, uint64ToBytes(meta.Version)))
				}

			} else if !ss.NotFound() {
				hlog.Printf("warn", "db err %s", ss.ErrorMessage())
				break
			}

			batch.Delete(keyExpireEncode(nsKeyTtl, meta.Expired, meta.Key))

			if batch.Len() >= workerLocalExpireLimit {
				break
			}
		}

		if bn := batch.Len(); bn > 0 {
			batch.Commit()
			hlog.Printf("debug", "table %s, ttl clean %d", dt.tableName, bn)
		} else {
			dt.expiredSync(tn + 1000)
		}

		if batch.Len() < workerLocalExpireLimit {
			break
		}
	}

	return nil
}

func (cn *Conn) workerLocalTableRefresh() error {

	tn := time.Now().Unix()

	if (cn.workerTableRefreshed + workerTableRefreshTime) > tn {
		return nil
	}

	rgS := []*kv2.StorageIteratorRange{
		{
			Start: []byte{},
			Limit: []byte{0xff},
		},
	}
	rgK := &kv2.StorageIteratorRange{
		Start: keyEncode(nsKeyMeta, []byte{}),
		Limit: keyEncode(nsKeyMeta, []byte{0xff}),
	}
	rgIncr := &kv2.StorageIteratorRange{
		Start: keySysIncrCutset(""),
		Limit: append(keySysIncrCutset(""), []byte{0xff}...),
	}
	rgAsync := &kv2.StorageIteratorRange{
		Start: keySysLogAsync("", ""),
		Limit: append(keySysLogAsync("", ""), []byte{0xff}...),
	}

	if cn.opts.Feature.WriteMetaDisable {
		rgK.Start = keyEncode(nsKeyData, []byte{})
		rgK.Limit = keyEncode(nsKeyData, []byte{0xff})
	}

	for _, t := range cn.tables {

		if err := cn.workerLocalLogCleanTable(t); err != nil {
			hlog.Printf("warn", "worker log clean table %s, err %s",
				t.tableName, err.Error())
		}

		// db size
		s, err := t.db.SizeOf(rgS)
		if err != nil {
			hlog.Printf("warn", "get db size error %s", err.Error())
			continue
		}
		if len(s) < 1 {
			continue
		}

		// db keys
		kn := uint64(0)
		iter := t.db.NewIterator(rgK)
		for ok := iter.First(); ok; ok = iter.Next() {
			kn++
		}
		iter.Release()

		tableStatus := kv2.TableStatus{
			Name:    t.tableName,
			KeyNum:  kn,
			DbSize:  uint64(s[0]),
			Options: map[string]int64{},
		}

		// log-id
		if ss := t.db.Get(keySysLogCutset, nil); ss.OK() {
			if logid, err := strconv.ParseInt(ss.String(), 10, 64); err == nil {
				tableStatus.Options["log_id"] = logid
			}
		}

		// incr
		iterIncr := t.db.NewIterator(rgIncr)
		for ok := iterIncr.First(); ok; ok = iterIncr.Next() {

			if bytes.Compare(iterIncr.Key(), rgIncr.Start) <= 0 {
				continue
			}

			if bytes.Compare(iterIncr.Key(), rgIncr.Limit) > 0 {
				break
			}

			incrid, err := strconv.ParseInt(string(iterIncr.Value()), 10, 64)
			if err != nil {
				continue
			}

			key := bytes.TrimPrefix(iterIncr.Key(), rgIncr.Start)
			if len(key) > 0 {
				tableStatus.Options[fmt.Sprintf("incr_id_%s", string(key))] = incrid
			}
		}
		iterIncr.Release()

		// async
		iterAsync := t.db.NewIterator(rgAsync)
		for ok := iterAsync.First(); ok; ok = iterAsync.Next() {

			if bytes.Compare(iterAsync.Key(), rgAsync.Start) <= 0 {
				continue
			}

			if bytes.Compare(iterAsync.Key(), rgAsync.Limit) > 0 {
				break
			}

			logid, err := strconv.ParseInt(string(iterAsync.Value()), 10, 64)
			if err != nil {
				continue
			}

			key := bytes.TrimPrefix(iterAsync.Key(), rgAsync.Start)
			if len(key) > 0 {
				tableStatus.Options[fmt.Sprintf("async_%s", string(key))] = logid
			}
		}
		iterAsync.Release()

		//
		rr := kv2.NewObjectWriter(nsSysTableStatus(t.tableName), tableStatus).
			TableNameSet(sysTableName)
		rs := cn.commitLocal(rr, 0)
		if !rs.OK() {
			hlog.Printf("warn", "refresh table (%s) status error %s", t.tableName, rs.Message)
		}

		if cn.close {
			break
		}
	}

	cn.workerTableRefreshed = tn

	return nil
}

func (cn *Conn) workerLocalReplicaOfLogAsync() error {

	ups := map[string]bool{}

	for _, hp := range cn.opts.Cluster.MainNodes {

		if cn.close {
			break
		}

		if hp.Addr == cn.opts.Server.Bind {
			continue
		}

		if _, ok := ups[hp.Addr]; ok {
			continue
		}

		for _, dt := range cn.tables {

			go func(hp *ClientConfig, dt *dbTable) {
				if err := cn.workerLocalReplicaOfLogAsyncTable(hp, &ConfigReplicaTableMap{
					From: dt.tableName,
					To:   dt.tableName,
				}); err != nil {
					hlog.Printf("warn", "worker replica-of log-async table %s -> %s, err %s",
						dt.tableName, dt.tableName, err.Error())
				}
			}(hp, dt)

			if cn.close {
				break
			}
		}

		ups[hp.Addr] = true
	}

	for _, hp := range cn.opts.Cluster.ReplicaOfNodes {

		if cn.close {
			break
		}

		if hp.Addr == cn.opts.Server.Bind || len(hp.TableMaps) == 0 {
			continue
		}

		if _, ok := ups[hp.Addr]; ok {
			continue
		}

		for _, tm := range hp.TableMaps {

			go func(hp *ClientConfig, tm *ConfigReplicaTableMap) {
				if err := cn.workerLocalReplicaOfLogAsyncTable(hp, tm); err != nil {
					hlog.Printf("warn", "worker replica-of log-async table %s -> %s, err %s",
						tm.From, tm.To, err.Error())
				}
			}(hp.ClientConfig, tm)

			if cn.close {
				break
			}
		}

		ups[hp.Addr] = true
	}

	return nil
}

func (cn *Conn) workerLocalReplicaOfLogAsyncTable(hp *ClientConfig, tm *ConfigReplicaTableMap) error {

	if tm.From != "main" {
		return nil
	}

	tdb := cn.tabledb(tm.To)
	if tdb == nil {
		return errors.New("no table found in local server")
	}

	lkey := hp.Addr + "/" + tm.From

	tdb.logAsyncMu.Lock()
	if _, ok := tdb.logAsyncSets[lkey]; ok {
		tdb.logAsyncMu.Unlock()
		return nil
	}
	tdb.logAsyncSets[lkey] = true
	tdb.logAsyncMu.Unlock()

	defer func() {
		tdb.logAsyncMu.Lock()
		delete(tdb.logAsyncSets, lkey)
		tdb.logAsyncMu.Unlock()
	}()

	var (
		offset = tdb.logAsyncOffset(hp.Addr, tm.From, 0)
		num    = 0
		retry  = 0
	)

	conn, err := clientConn(hp.Addr, hp.AccessKey, hp.AuthTLSCert, false)
	if err != nil {
		return err
	}

	req := &kv2.LogSyncRequest{
		Addr:      cn.opts.Server.Bind,
		TableName: tm.From,
		LogOffset: offset,
	}

	// hlog.Printf("info", "pull from %s/%s at %d", hp.Addr, tm.From, offset)

	for !cn.close && num < 1000000 {

		ctx, fc := context.WithTimeout(context.Background(), time.Second*100)
		rep, err := kv2.NewInternalClient(conn).LogSync(ctx, req)
		fc()

		if err != nil {

			hlog.SlotPrint(600, "warn", "kvgo log async from %s/%s, err %s",
				hp.Addr, tdb.tableName, err.Error())

			retry++
			if retry >= 3 {
				break
			}

			time.Sleep(1e9)
			conn, err = clientConn(hp.Addr, hp.AccessKey, hp.AuthTLSCert, true)
			continue
		}
		retry = 0

		if cn.close {
			break
		}

		if len(rep.Logs) > 0 {

			req2 := &kv2.LogSyncRequest{}

			for _, v := range rep.Logs {

				ss := tdb.db.Get(keyEncode(nsKeyMeta, v.Key), nil)
				if ss.OK() {
					meta, err := kv2.ObjectMetaDecode(ss.Bytes())
					if err == nil && meta.Version >= v.Version {
						if v.Version > offset {
							offset = v.Version
						}
						num++
						continue
					}
				}

				req2.Keys = append(req2.Keys, bytesClone(v.Key))
			}

			// hlog.Printf("debug", "log sync from %s/%s to %s, save logs %d ~ %d",
			// 	hp.Addr, tm.From, tm.To, rep.LogOffset, rep.LogCutset)

			for len(req2.Keys) > 0 {

				ctx, fc := context.WithTimeout(context.Background(), time.Second*100)
				rep2, err := kv2.NewInternalClient(conn).LogSync(ctx, req2)
				fc()

				if err != nil {
					return err
				}

				for _, item := range rep2.Items {

					ow := &kv2.ObjectWriter{
						Meta: item.Meta,
						Data: item.Data,
					}

					if kv2.AttrAllow(item.Meta.Attrs, kv2.ObjectMetaAttrDelete) {
						ow.ModeDeleteSet(true)
					}

					ow.TableNameSet(tm.To)

					rs2 := cn.commitLocal(ow, item.Meta.Version)
					if !rs2.OK() {
						break
					}

					num++
					if item.Meta.Version > offset {
						offset = item.Meta.Version
					}
				}

				// hlog.Printf("debug", "log sync from %s/%s to %s, save keys %d, next keys %d",
				// 	hp.Addr, tm.From, tm.To, len(rep2.Items), len(rep2.NextKeys))

				req2.Keys = rep2.NextKeys
			}

		} else if rep.Action == 0 {
			time.Sleep(1e9)
		}

		if offset > req.LogOffset {
			req.LogOffset = offset
			tdb.logAsyncOffset(hp.Addr, tm.From, offset)
			hlog.SlotPrint(600, "info", "log sync from %s/%s to local/%s, num %d, log offset %d",
				hp.Addr, tm.From, tm.To, num, offset)
		}
		// fmt.Printf("log async %d, offset %d, rep %v\n", num, req.LogOffset, rep)
	}

	if num > 0 {
		hlog.Printf("info", "kvgo log async from %s/%s to local/%s, num %d, offset %d",
			hp.Addr, tm.From, tm.To, num, req.LogOffset)
	}

	return nil
}

func (cn *Conn) workerLocalLogCleanTable(tdb *dbTable) error {

	var (
		offset = keyEncode(nsKeyLog, uint64ToBytes(0))
		cutset = keyEncode(nsKeyLog, []byte{0xff})
	)

	var (
		iter = tdb.db.NewIterator(&kv2.StorageIteratorRange{
			Start: offset,
			Limit: cutset,
		})
		sets  = map[string]uint64{}
		ndel  = 0
		batch = tdb.db.NewBatch()
	)

	for ok := iter.Last(); ok && !cn.close; ok = iter.Prev() {

		if bytes.Compare(iter.Key(), cutset) >= 0 {
			continue
		}

		if bytes.Compare(iter.Key(), offset) <= 0 {
			break
		}

		if len(iter.Value()) >= 2 {

			logMeta, err := kv2.ObjectMetaDecode(iter.Value())
			if err == nil && logMeta != nil {

				tdb.objectLogVersionSet(0, logMeta.Version, 0)

				if _, ok := sets[string(logMeta.Key)]; !ok {

					if ss := tdb.db.Get(keyEncode(nsKeyMeta, logMeta.Key), nil); ss.OK() {
						meta, err := kv2.ObjectMetaDecode(ss.Bytes())
						if err == nil && meta.Version > 0 && meta.Version != logMeta.Version {
							batch.Delete(iter.Key())
							ndel++
							continue
						}
					}

					sets[string(logMeta.Key)] = logMeta.Version
					continue
				}
			}
		}

		batch.Delete(iter.Key())
		ndel++

		if ndel >= 1000 {
			batch.Commit()
			batch = tdb.db.NewBatch()
			ndel = 0
			hlog.Printf("info", "table %s, log clean %d/%d", tdb.tableName, ndel, len(sets))
		}
	}

	if ndel > 0 {
		batch.Commit()
		hlog.Printf("info", "table %s, log clean %d/%d", tdb.tableName, ndel, len(sets))
	}

	iter.Release()

	return nil
}

func (cn *Conn) workerLocalSysStatusRefresh() error {

	tn := time.Now().Unix()
	if (cn.workerStatusRefreshed + workerStatusRefreshTime) > tn {
		return nil
	}

	if cn.perfStatus == nil {

		if ss := cn.dbSys.Get(nsSysApiStatus("node"), nil); ss.OK() {
			var perfStatus tsd.CycleFeed
			if kv2.StdProto.Decode(ss.Bytes(), &perfStatus) == nil && perfStatus.Unit == 10 {
				cn.perfStatus = &perfStatus
			}
		}

		if cn.perfStatus == nil {
			cn.perfStatus = tsd.NewCycleFeed(10)
		}

		for _, v := range []string{
			// API QPS
			PerfAPIReadKey, PerfAPIReadKeyRange, PerfAPIReadLogRange, PerfAPIWriteKey,
			// API BPS
			PerfAPIReadBytes, PerfAPIWriteBytes,
			// Storage QPS
			PerfStorReadKey, PerfStorReadKeyRange, PerfStorReadLogRange, PerfStorWriteKey,
			// Storage BPS
			PerfStorReadBytes, PerfStorWriteBytes,
		} {
			cn.perfStatus.Sync(v, 0, 0, tsd.ValueAttrSum)
		}

		sort.Slice(cn.perfStatus.Items, func(i, j int) bool {
			return strings.Compare(cn.perfStatus.Items[i].Name, cn.perfStatus.Items[j].Name) < 0
		})
	}

	if cn.sysStatus.Updated == 0 {
		cn.sysStatus.Caps = map[string]*kv2.SysCapacity{
			"cpu": {
				Use: int64(runtime.NumCPU()),
			},
		}
		cn.sysStatus.Addr = cn.opts.Server.Bind
		cn.sysStatus.Version = Version
		cn.sysStatus.Uptime = tn
	}

	cn.sysStatus.Updated = tn

	{
		vm, _ := ps_mem.VirtualMemory()
		cn.sysStatus.Caps["mem"] = &kv2.SysCapacity{
			Use: int64(vm.Used),
			Max: int64(vm.Total),
		}
	}

	if cn.workerStatusRefreshed == 0 || rand.Intn(10) == 0 {
		if st, err := ps_disk.Usage(cn.opts.Storage.DataDirectory); err == nil {
			cn.sysStatus.Caps["disk"] = &kv2.SysCapacity{
				Use: int64(st.Used),
				Max: int64(st.Total),
			}
		}
	}

	if bs, err := kv2.StdProto.Encode(cn.perfStatus); err == nil && len(bs) > 20 {
		cn.dbSys.Put(nsSysApiStatus("node"), bs, nil)
	}

	cn.workerStatusRefreshed = tn

	// debugPrint(cn.sysStatus)

	return nil
}
