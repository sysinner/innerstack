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
	mrand "math/rand"
	"sync"
	"time"

	"github.com/hooto/hlog4g/hlog"
	kv2 "github.com/lynkdb/kvspec/go/kvspec/v2"
)

const (
	logSyncBufferTableQueueMin        = 1000
	logSyncBufferTableQueueMax        = 100000
	logSyncBufferTableQueueTTL        = int64(60000) // ms
	logSyncBufferQueueMax             = 1000
	logSyncBufferActionAccept  uint64 = 1 << 1
	logSyncBufferActionFree    uint64 = 1 << 2
)

type logSyncBufferItem struct {
	id      uint64
	key     []byte
	attrs   uint64
	created int64
	prev    *logSyncBufferItem
	next    *logSyncBufferItem
	action  uint64
}

type logSyncBufferTableEvent struct {
	queue   chan uint64
	created int64
	offset  uint64
}

type logSyncBufferTableClientStatus struct {
	logNum int64
	keyNum int64
}

type logSyncBufferTable struct {
	mu     sync.RWMutex
	queue  *logSyncBufferItem
	last   *logSyncBufferItem
	keys   map[uint64]*logSyncBufferItem
	cutset uint64
	evsets map[uint64]*logSyncBufferTableEvent
	cssets map[string]*logSyncBufferTableClientStatus
}

func newLogSyncBufferTable() *logSyncBufferTable {
	return &logSyncBufferTable{
		keys:   map[uint64]*logSyncBufferItem{},
		evsets: map[uint64]*logSyncBufferTableEvent{},
		cssets: map[string]*logSyncBufferTableClientStatus{},
	}
}

func newLogSyncPullTableEvent(offset uint64) *logSyncBufferTableEvent {
	return &logSyncBufferTableEvent{
		queue:   make(chan uint64, 8),
		created: timems(),
		offset:  offset,
	}
}

func (it *logSyncBufferTable) status(addr string, logs, keys int) *logSyncBufferTableClientStatus {
	it.mu.Lock()
	defer it.mu.Unlock()

	p, ok := it.cssets[addr]
	if !ok {
		p = &logSyncBufferTableClientStatus{}
		it.cssets[addr] = p
	}
	p.logNum += int64(logs)
	p.keyNum += int64(keys)
	return p
}

func (it *logSyncBufferTable) put(id, attrs uint64, key []byte, hit bool) {

	it.mu.Lock()
	defer it.mu.Unlock()

	var (
		tn  = timems()
		ttl = (tn - logSyncBufferTableQueueTTL)
	)

	if it.queue != nil && ttl > it.queue.created &&
		len(it.keys) > logSyncBufferTableQueueMin {
		for it.queue != nil && it.queue.next != nil && ttl > it.queue.created {
			q := it.queue

			it.queue = it.queue.next
			if it.queue != nil {
				it.queue.prev = nil
			} else {
				it.last = nil
			}

			q.next = nil
			delete(it.keys, q.id)
		}
		hlog.SlotPrint(600, "info", "clean ttl log queue, active %d", len(it.keys))
	}

	if len(it.keys) > logSyncBufferTableQueueMax {
		for ndel := 0; it.queue != nil && ndel < logSyncBufferTableQueueMax/100; ndel++ {
			q := it.queue

			it.queue = it.queue.next
			if it.queue != nil {
				it.queue.prev = nil
			} else {
				it.last = nil
			}

			q.next = nil
			delete(it.keys, q.id)
		}
		hlog.Printf("info", "clean log queue, active %d", len(it.keys))
	}

	item := &logSyncBufferItem{
		key:     bytesClone(key),
		attrs:   attrs,
		id:      id,
		created: tn,
		prev:    it.last,
	}

	if it.queue == nil {
		it.queue, it.last = item, item
	} else {
		it.last.next = item
		it.last = item
	}

	it.keys[id] = item

	if id > it.cutset {
		it.cutset = id
	}

	if hit {
		// hlog.Printf("info", "log id %d, key %s, hit %v, qlen %d, queue %d, last %v",
		// 	id, string(item.key), hit, len(it.keys), it.queue.id, it.last.id)

		item.action = logSyncBufferActionAccept

		for _, ev := range it.evsets {
			if item.id > ev.offset {
				ev.queue <- item.id
			}
		}
	}
}

func (it *logSyncBufferTable) hit(id uint64) int {

	it.mu.Lock()
	defer it.mu.Unlock()

	v, ok := it.keys[id]
	hlog.Printf("debug", "push hit %d, key %v, qlen %d", id, v, len(it.keys))

	if !ok {
		return -1
	}

	v.action = logSyncBufferActionAccept

	for _, ev := range it.evsets {
		if v.id > ev.offset {
			ev.queue <- 1
		}
	}

	return 0
}

// 0: wait, 1: cold, 2: hot
func (it *logSyncBufferTable) query(req *kv2.LogSyncRequest) *kv2.LogSyncReply {

	rs := &kv2.LogSyncReply{
		LogOffset: req.LogOffset,
	}

	if it.last != nil && req.LogOffset > it.last.id {
		rs.Action = 0
		return rs
	}

	var (
		num = 1000
		siz = 2 * 1024 * 1024
	)

	if it.queue != nil {

		if rs.LogOffset < it.queue.id {
			rs.Action = 1
			it.mu.Lock()
			for v := it.queue; v != nil && v.action == logSyncBufferActionAccept && num > 0; v = v.next {
				rs.LogCutset = v.id
				num--
			}
			it.mu.Unlock()
			return rs
		}

		it.mu.Lock()
		q, ok := it.keys[req.LogOffset]
		if !ok {
			q = it.queue
		}

		for v := q.next; v != nil && v.action == logSyncBufferActionAccept && num > 0 && siz > 0; v = v.next {
			if v.id <= req.LogOffset {
				continue
			}
			rs.Logs = append(rs.Logs, &kv2.ObjectMeta{
				Version: v.id,
				Key:     bytesClone(v.key),
				Attrs:   v.attrs,
			})
			rs.LogCutset = v.id
			num--
			siz -= (len(v.key) + 20)
		}

		it.mu.Unlock()
	}

	if len(rs.Logs) == 0 {

		var (
			eid = mrand.Uint64()
			ev  = newLogSyncPullTableEvent(req.LogOffset)
		)

		it.mu.Lock()
		it.evsets[eid] = ev
		it.mu.Unlock()

		select {
		case sig := <-ev.queue:

			if sig > 0 {

				if req.LogOffset < 100 {
					rs.LogCutset = sig
					rs.Action = 1
				} else {
					it.mu.Lock()
					q, ok := it.keys[req.LogOffset]
					if !ok {
						q = it.queue
					}
					for v := q.next; v != nil && v.action == logSyncBufferActionAccept && num > 0 && siz > 0; v = v.next {
						if v.id <= req.LogOffset {
							continue
						}
						rs.Logs = append(rs.Logs, &kv2.ObjectMeta{
							Version: v.id,
							Key:     bytesClone(v.key),
							Attrs:   v.attrs,
						})
						rs.LogCutset = v.id
						num--
						siz -= (len(v.key) + 20)
					}
					it.mu.Unlock()
				}
			}

		case <-time.After(30 * time.Second):
		}

		it.mu.Lock()
		delete(it.evsets, eid)
		it.mu.Unlock()
	}

	if len(rs.Logs) > 0 {
		rs.Action = 2
	}

	return rs
}
