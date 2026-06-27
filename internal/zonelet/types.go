// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
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

package zonelet

import (
	"log/slog"
	"sync"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

type appInstanceEntry struct {
	Meta  *kvapi.Meta
	Value *inapi.AppInstance
}

type appInstanceSet struct {
	mu      sync.RWMutex
	idx     map[string]*appInstanceEntry
	arr     []*appInstanceEntry
	version uint64
	ready   bool
}

func (it *appInstanceSet) Flush(app *appInstanceEntry) error {
	it.mu.Lock()
	defer it.mu.Unlock()

	key := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, app.Value.InstanceName())
	if rs := data.Zonelet.NewWriter(key, app.Value).SetPrevVersion(
		app.Meta.Version).Exec(); !rs.OK() {
		slog.Warn("update instance fail",
			"instance_name", app.Value.InstanceName(),
			"err", rs.ErrorMessage())
		return rs.Error()
	} else {
		app.Meta = rs.Item().Meta
		it.store(app.Value, nil)
	}
	return nil
}

func (it *appInstanceSet) Refresh() error {
	it.mu.Lock()
	defer it.mu.Unlock()

	offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, "")
	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).
		SetLimit(inapi.Zonelet_MaxInstances).Exec()
	if !rs.OK() && !rs.NotFound() {
		return rs.Error()
	}
	for _, item := range rs.Items {
		var instance inapi.AppInstance
		if err := item.JsonDecode(&instance); err != nil {
			continue
		}

		if instance.Deploy == nil || instance.Spec == nil ||
			instance.Spec.Resources == nil {
			slog.Warn("refresh instance with invalid deploy or spec",
				"instance_name", instance.InstanceName())
			continue
		}

		it.store(&instance, item.Meta)
	}

	it.ready = true

	return nil
}

func (it *appInstanceSet) Store(app *inapi.AppInstance, meta *kvapi.Meta) {
	if meta == nil || app == nil {
		return
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	it.store(app, meta)
}

func (it *appInstanceSet) store(app *inapi.AppInstance, meta *kvapi.Meta) {

	if it.idx == nil {
		it.idx = map[string]*appInstanceEntry{}
	}

	if app != nil && meta != nil {
		entry, ok := it.idx[app.InstanceName()]
		if ok && meta.Version <= entry.Meta.Version {
			return
		} else if ok {
			entry.Meta = meta
			entry.Value = app
		} else {
			entry = &appInstanceEntry{
				Meta:  meta,
				Value: app,
			}
			it.idx[app.InstanceName()] = entry
			it.arr = append(it.arr, entry)
		}
		it.version = max(it.version, meta.Version)
	}
}

func (it *appInstanceSet) Load(id string) *appInstanceEntry {
	it.mu.RLock()
	defer it.mu.RUnlock()

	if it.idx == nil {
		return nil
	}

	if entry, ok := it.idx[id]; ok {
		return entry
	}

	return nil
}

func (it *appInstanceSet) Iter(f func(entry *appInstanceEntry) bool) {
	for _, v := range it.arr {
		if !f(v) {
			break
		}
	}
}

func (it *appInstanceSet) Len() int {
	return len(it.arr)
}

func (it *appInstanceSet) IsReady() bool {
	return it.ready
}

func (it *appInstanceSet) Clear() {
	it.mu.Lock()
	defer it.mu.Unlock()

	it.idx = map[string]*appInstanceEntry{}
	it.arr = []*appInstanceEntry{}
	it.version = 0
	it.ready = false
}
