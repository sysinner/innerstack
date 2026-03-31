package status

import (
	"strings"
	"sync"
	"time"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
	"github.com/sysinner/incore/v2/internal/config"
)

var (
	ZoneletLeader        string
	ZoneletLeaderTTL     int64 = 12000 // milliseconds
	ZoneletLeaderUpdated int64
	ZoneletLeaderVersion uint64
)

var (
	Zonelet_HostStatusSet sync.Map

	Zonelet_HostOperateSet KvSet
)

func IsZonelet() bool {
	if config.Config.Zonelet.ZoneName == "" {
		return false
	}
	for _, v := range config.Config.Server.ZoneHosts {
		if strings.HasPrefix(v, config.Config.Hostlet.LanAddr+":") {
			return true
		}
	}
	return false
}

func IsZoneletLeader() bool {
	tn := time.Now().UnixMilli()
	return ZoneletLeader == config.Config.Hostlet.HostId &&
		(ZoneletLeaderUpdated+ZoneletLeaderTTL) > tn
}

type KvEntry struct {
	Meta  *kvapi.Meta
	Value interface{}
}

type KvSet struct {
	mu      sync.RWMutex
	idx     map[string]*KvEntry
	arr     []*KvEntry
	version uint64
}

func (it *KvSet) Store(key string, meta *kvapi.Meta, val any) {
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

func (it *KvSet) Load(key string) (*KvEntry, bool) {
	it.mu.RLock()
	defer it.mu.RUnlock()

	if it.idx == nil {
		return nil, false
	}

	if entry, ok := it.idx[key]; ok {
		return entry, true
	}

	return nil, false
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

func (it *KvSet) Clear() {
	it.mu.Lock()
	defer it.mu.Unlock()

	it.idx = map[string]*KvEntry{}
	it.arr = []*KvEntry{}
	it.version = 0
}
