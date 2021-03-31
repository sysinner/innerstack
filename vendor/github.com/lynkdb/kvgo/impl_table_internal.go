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
	"strconv"
	"sync"
	"time"

	"github.com/hooto/hlog4g/hlog"
	kv2 "github.com/lynkdb/kvspec/go/kvspec/v2"
	"github.com/valuedig/apis/go/tsd/v1"
)

type dbTableIncrSet struct {
	offset uint64
	cutset uint64
}

type dbTable struct {
	instId         string
	tableId        uint32
	tableName      string
	db             kv2.StorageEngine
	incrMu         sync.RWMutex
	incrSets       map[string]*dbTableIncrSet
	logMu          sync.RWMutex
	logOffset      uint64
	logCutset      uint64
	logAsyncMu     sync.Mutex
	logAsyncSets   map[string]bool
	logLockSets    map[uint64]uint64
	perfStatus     *tsd.CycleFeed
	logSyncBuffer  *logSyncBufferTable
	logSyncOffsets map[string]uint64
	closed         bool
	expiredNext    int64
	expiredMu      sync.RWMutex
}

func (tdb *dbTable) setup() error {

	tdb.logMu.Lock()
	defer tdb.logMu.Unlock()

	var (
		offset = keyEncode(nsKeyLog, []byte{0x00})
		cutset = keyEncode(nsKeyLog, []byte{0xff})
		iter   = tdb.db.NewIterator(&kv2.StorageIteratorRange{
			Start: offset,
			Limit: cutset,
		})
		num = 10
	)

	for ok := iter.Last(); ok && num > 0; ok = iter.Prev() {
		num--
	}

	for ok := iter.Next(); ok; ok = iter.Next() {

		if bytes.Compare(iter.Key(), cutset) >= 0 {
			continue
		}

		if len(iter.Value()) < 2 {
			continue
		}

		meta, err := kv2.ObjectMetaDecode(iter.Value())
		if err == nil && meta != nil {
			tdb.logSyncBuffer.put(meta.Version, meta.Attrs, meta.Key, true)
			// hlog.Printf("info", "meta.Version %d, meta.Key %s", meta.Version, string(meta.Key))
		}
	}

	iter.Release()

	return nil
}

func (tdb *dbTable) objectLogVersionSet(incr, set, updated uint64) (uint64, error) {

	tdb.logMu.Lock()
	defer tdb.logMu.Unlock()

	if incr == 0 && set == 0 {
		return tdb.logOffset, nil
	}

	var err error

	if tdb.logCutset <= 100 {

		if ss := tdb.db.Get(keySysLogCutset, nil); !ss.OK() {
			if !ss.NotFound() {
				return 0, ss.Error()
			}
		} else {
			if tdb.logCutset, err = strconv.ParseUint(ss.String(), 10, 64); err != nil {
				return 0, err
			}
			if tdb.logOffset < tdb.logCutset {
				tdb.logOffset = tdb.logCutset
			}
		}
	}

	if tdb.logOffset < 100 {
		tdb.logOffset = 100
	}

	if set > 0 && set > tdb.logOffset {
		incr += (set - tdb.logOffset)
	}

	if incr > 0 {

		if (tdb.logOffset + incr) >= tdb.logCutset {

			cutset := tdb.logOffset + incr + 100

			if n := cutset % 100; n > 0 {
				cutset += n
			}

			if ss := tdb.db.Put(keySysLogCutset,
				[]byte(strconv.FormatUint(cutset, 10)), nil); !ss.OK() {
				return 0, ss.Error()
			}

			hlog.Printf("debug", "table %s, reset log-version to %d~%d",
				tdb.tableName, tdb.logOffset+incr, cutset)

			tdb.logCutset = cutset
		}

		tdb.logOffset += incr

		if updated > 0 {
			tdb.logLockSets[tdb.logOffset] = updated
		}
	}

	return tdb.logOffset, nil
}

func (tdb *dbTable) objectLogFree(logId uint64) {
	tdb.logMu.Lock()
	defer tdb.logMu.Unlock()
	delete(tdb.logLockSets, logId)
}

func (tdb *dbTable) objectLogDelay() uint64 {
	tdb.logMu.Lock()
	defer tdb.logMu.Unlock()
	var (
		tn    = uint64(time.Now().UnixNano() / 1e6)
		dels  = []uint64{}
		delay = tn
	)
	for k, v := range tdb.logLockSets {
		if v+3000 < tn {
			dels = append(dels, k)
		} else if v < delay {
			delay = v
		}
	}
	for _, k := range dels {
		delete(tdb.logLockSets, k)
	}
	if len(tdb.logLockSets) == 0 {
		return tn
	}
	return delay
}

func (tdb *dbTable) objectIncrSet(ns string, incr, set uint64) (uint64, error) {

	tdb.incrMu.Lock()
	defer tdb.incrMu.Unlock()

	incrSet := tdb.incrSets[ns]
	if incrSet == nil {
		incrSet = &dbTableIncrSet{
			offset: 0,
			cutset: 0,
		}
		tdb.incrSets[ns] = incrSet
	}

	if incr == 0 && set == 0 {
		return incrSet.offset, nil
	}

	var err error

	if incrSet.cutset <= 100 {

		if ss := tdb.db.Get(keySysIncrCutset(ns), nil); !ss.OK() {
			if !ss.NotFound() {
				return 0, ss.Error()
			}
		} else {
			if incrSet.cutset, err = strconv.ParseUint(ss.String(), 10, 64); err != nil {
				return 0, err
			}
			if incrSet.offset < incrSet.cutset {
				incrSet.offset = incrSet.cutset
			}
		}
	}

	if incrSet.offset < 100 {
		incrSet.offset = 100
	}

	if set > 0 && set > incrSet.offset {
		incr += (set - incrSet.offset)
	}

	if incr > 0 {

		if (incrSet.offset + incr) >= incrSet.cutset {

			cutset := incrSet.offset + incr + 100

			if ss := tdb.db.Put(keySysIncrCutset(ns),
				[]byte(strconv.FormatUint(cutset, 10)), nil); !ss.OK() {
				return 0, ss.Error()
			}

			incrSet.cutset = cutset
		}

		incrSet.offset += incr
	}

	return incrSet.offset, nil
}

func (tdb *dbTable) logAsyncOffset(hostAddr, tableFrom string, offset uint64) uint64 {

	lkey := hostAddr + "/" + tableFrom

	tdb.logAsyncMu.Lock()
	defer tdb.logAsyncMu.Unlock()

	var (
		prevOffset, ok = tdb.logSyncOffsets[lkey]
		err            error
	)
	if !ok || prevOffset < 1 {
		if ss := tdb.db.Get(keySysLogAsync(hostAddr, tableFrom), nil); !ss.OK() {
			if !ss.NotFound() {
				return offset
			}
		} else {
			if prevOffset, err = strconv.ParseUint(ss.String(), 10, 64); err == nil {
				tdb.logSyncOffsets[lkey] = prevOffset
			}
		}
	}

	if offset <= prevOffset {
		return prevOffset
	}

	tdb.logSyncOffsets[lkey] = offset
	tdb.db.Put(keySysLogAsync(hostAddr, tableFrom),
		[]byte(strconv.FormatUint(offset, 10)), nil)

	return offset
}

func (it *dbTable) expiredSync(t int64) int64 {

	it.expiredMu.Lock()
	defer it.expiredMu.Unlock()

	if t == -1 {
		it.expiredNext = workerLocalExpireMax
	} else if t > 0 && t < it.expiredNext {
		it.expiredNext = t
	}

	return it.expiredNext
}

func (it *dbTable) Close() error {

	if it.closed || it.db == nil {
		return nil
	}

	for ns, incrSet := range it.incrSets {

		if incrSet.cutset > incrSet.offset {

			incrSet.cutset = incrSet.offset

			if ss := it.db.Put(keySysIncrCutset(ns),
				[]byte(strconv.FormatUint(incrSet.cutset, 10)), nil); !ss.OK() {
				hlog.Printf("info", "db error %s", ss.ErrorMessage())
			} else {
				hlog.Printf("info", "kvgo table %s, flush incr ns:%s offset %d",
					it.tableName, ns, incrSet.offset)
			}
		}
	}

	it.logMu.Lock()
	defer it.logMu.Unlock()
	if it.logCutset > it.logOffset {

		it.logCutset = it.logOffset

		if ss := it.db.Put(keySysLogCutset,
			[]byte(strconv.FormatUint(it.logCutset, 10)), nil); !ss.OK() {
			hlog.Printf("info", "db error %s", ss.ErrorMessage())
		} else {
			hlog.Printf("info", "kvgo table %s, flush log-id offset %d", it.tableName, it.logCutset)
		}
	}

	it.db.Close()

	it.closed = true

	return nil
}
