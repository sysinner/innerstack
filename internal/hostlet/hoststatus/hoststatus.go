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

package hoststatus

import (
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/internal/inutil/syncx"
	"github.com/sysinner/innerstack/v2/internal/stateflow"
)

var (
	StatusSet sync.Map

	ActiveAppList syncx.Map

	ContainerList syncx.Map

	ImageList syncx.Map

	Active HostActiveConfig

	AppWorkflow stateflow.AppStateWorkflow
)

var (
	HostReady      atomic.Bool
	ContainerReady atomic.Bool
)

type HostActiveConfig struct {
	mu sync.RWMutex

	AppInstances []*inapi.AppInstance `json:"app_instances,omitempty"`

	// AppliedRevisions records the last-applied Deploy.Revision per
	// container name. It is persisted to hostlet_active.json so that a
	// Deploy.Revision increment issued while the hostlet was down is still
	// detected after restart and triggers a container recreate.
	AppliedRevisions map[string]uint64 `json:"applied_revisions,omitempty"`

	index map[string]*inapi.AppInstance
}

// SetAppliedRevision records the last-applied Deploy.Revision for a
// container. Safe for concurrent use.
func (it *HostActiveConfig) SetAppliedRevision(containerName string, rev uint64) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.AppliedRevisions == nil {
		it.AppliedRevisions = make(map[string]uint64)
	}
	it.AppliedRevisions[containerName] = rev
}

// AppliedRevision returns the last-applied Deploy.Revision for a container,
// or (0, false) if none is recorded.
func (it *HostActiveConfig) AppliedRevision(containerName string) (uint64, bool) {
	it.mu.RLock()
	defer it.mu.RUnlock()
	if it.AppliedRevisions == nil {
		return 0, false
	}
	rev, ok := it.AppliedRevisions[containerName]
	return rev, ok
}

// MarshalJSON serializes the persisted fields under the read lock. The alias
// avoids recursing back into MarshalJSON.
func (it *HostActiveConfig) MarshalJSON() ([]byte, error) {
	it.mu.RLock()
	defer it.mu.RUnlock()
	type Alias HostActiveConfig
	return json.Marshal((*Alias)(it))
}

func (h *HostActiveConfig) UnmarshalJSON(data []byte) error {
	// 定义一个别名，避免递归调用 UnmarshalJSON 导致死循环
	type Alias HostActiveConfig
	aux := &struct {
		*Alias
	}{
		Alias: (*Alias)(h),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	h.index = make(map[string]*inapi.AppInstance)

	for _, app := range h.AppInstances {
		if app != nil {
			h.index[app.InstanceName()] = app
		}
	}

	return nil
}
