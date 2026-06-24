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

package hostlet

import (
	_ "embed"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/hostlet/hoststatus"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/pkg/signals"
)

//go:embed scripts/ininit
var ininitScript []byte

func TryRun() error {
	cfgfile := hostActiveConfigPath()
	if err := inutil.JsonDecodeFromFile(cfgfile, &hoststatus.Active); err != nil {
		slog.Warn("hostlet load config failed", "error", err)
	}
	return nil
}

// hostActiveConfigPath returns the on-disk path of the hostlet active config.
func hostActiveConfigPath() string {
	return filepath.Join(config.Prefix + "/etc/hostlet_active.json")
}

// saveHostActiveConfig persists the hostlet active config (including the
// last-applied Deploy.Revision map) to disk so that revision increments
// survive a hostlet restart. Failures are logged but non-fatal: a stale
// file only risks a spurious recreate on the next restart.
func saveHostActiveConfig() {
	if err := inutil.JsonEncodeToFileIndent(hostActiveConfigPath(), &hoststatus.Active, 0644); err != nil {
		slog.Warn("hostlet active config save failed", "error", err)
	}
}

var once sync.Once

func Run() {

	once.Do(taskContainerInit)

	if err := statusRefresh(); err != nil {
		slog.Error("hostlet", "err", err.Error())
	}

	tr := time.NewTimer(3e9)
	defer tr.Stop()

	tc := time.NewTimer(5e9)
	defer tc.Stop()

	tq := time.NewTimer(10e9)
	defer tq.Stop()

	for {
		select {
		case <-signals.Done():
			slog.Warn("hostlet quit")
			return

		case <-tr.C:
			if err := statusRefresh(); err != nil {
				slog.Error("hostlet", "err", err.Error())
			} else if err = networkRefresh(); err != nil {
				slog.Error("hostlet", "err", err.Error())
			}
			tr.Reset(3e9)

		case <-tc.C:
			if err := containerRefresh(); err != nil {
				slog.Error("hostlet", "err", err.Error())
			}
			tc.Reset(5e9)

		case <-tq.C:
			if err := xfsQuotaRefresh(); err != nil {
				slog.Error("hostlet quota refresh", "err", err.Error())
			}
			tq.Reset(10e9)
		}
	}
}
