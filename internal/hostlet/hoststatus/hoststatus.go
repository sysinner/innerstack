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

	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/internal/inutil/syncx"
	"github.com/sysinner/incore/v2/internal/stateflow"
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
	mu           sync.RWMutex
	AppInstances []*inapi.AppInstance `json:"app_instances,omitempty"`
	index        map[string]*inapi.AppInstance
}

func (it *HostActiveConfig) Sync(app *inapi.AppInstance) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.index == nil {
		it.index = make(map[string]*inapi.AppInstance)
	}
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
