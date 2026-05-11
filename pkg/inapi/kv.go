package inapi

import (
	"sync"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
)

type KvEntry struct {
	Meta  *kvapi.Meta
	Value interface{}
}

type KvSet struct {
	mu      sync.RWMutex
	idx     map[string]*KvEntry
	arr     []*KvEntry
	version uint64
	ready   bool
}

func (it *KvSet) Fresh(fn func() error) {
	if err := fn(); err == nil && len(it.arr) > 0 {
		it.ready = true
	}
}

func (it *KvSet) Store(key string, meta *kvapi.Meta, val any) {
	if val == nil {
		return
	}
	it.mu.Lock()
	defer it.mu.Unlock()

	if it.idx == nil {
		it.idx = map[string]*KvEntry{}
	}

	if entry, ok := it.idx[key]; ok {
		entry.Meta = meta
		entry.Value = val
	} else {
		entry = &KvEntry{
			Meta:  meta,
			Value: val,
		}
		it.idx[key] = entry
		it.arr = append(it.arr, entry)
	}

	if meta != nil {
		it.version = max(it.version, meta.Version)
	}
}

func (it *KvSet) Load(key string) *KvEntry {
	it.mu.RLock()
	defer it.mu.RUnlock()

	if it.idx == nil {
		return nil
	}

	if entry, ok := it.idx[key]; ok {
		return entry
	}

	return nil
}

func (it *KvSet) Iter(f func(entry *KvEntry) bool) {
	for _, v := range it.arr {
		if !f(v) {
			break
		}
	}
}

func (it *KvSet) Len() int {
	return len(it.arr)
}

func (it *KvSet) IsReady() bool {
	return it.ready
}

func (it *KvSet) Clear() {
	it.mu.Lock()
	defer it.mu.Unlock()

	it.idx = map[string]*KvEntry{}
	it.arr = []*KvEntry{}
	it.version = 0
	it.ready = false
}
