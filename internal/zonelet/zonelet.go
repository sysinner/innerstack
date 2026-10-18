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
	"fmt"
	"log/slog"
	"time"

	"github.com/sysinner/innerstack/v2/internal/audit"
	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/zonelet/network"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/signals"
)

var (
	zoneNetMgr = network.NewNetworkManager()

	gHostSet        inapi.KvSet
	gHostOperateSet inapi.KvSet

	gAppInstanceSet appInstanceSet
)

func Run() {

	// ticks counts 1s loop passes to drive sub-cadence tasks without extra
	// goroutines: alert evaluation every alertEvalTicks (5s), monstore
	// sampling every 30 ticks.
	var ticks uint64

	tr := time.NewTimer(1e9)
	defer tr.Stop()

	for {
		select {
		case <-signals.Done():
			slog.Warn("zonelet quit")
			return

		case <-tr.C:
			forceRefresh := false
			var err error
			if forceRefresh, err = leaderRefresh(); err != nil {
				slog.Error(fmt.Sprintf("zonelet leader refresh, err %s", err.Error()))
			}
			// if forceRefresh {
			// 	if err := auth.AuthMgr.RefreshAccessKeysFromDB(); err != nil {
			// 		slog.Error(fmt.Sprintf("zonelet auth refresh, err %s", err.Error()))
			// 	}
			// }
			if err = schedulerRefresh(forceRefresh); err != nil {
				slog.Error(fmt.Sprintf("zonelet scheduler refresh, err %s", err.Error()))
			}
			// Pin the audit chain head once per local day (no-op otherwise).
			audit.AnchorRefresh()

			ticks++
			if ticks%alertEvalTicks == 0 {
				alertsRefresh(time.Now().Unix())
			}
			if ticks%uint64(config.Config.Monstore.SampleIntervalSec()) == 0 {
				monstoreTick(time.Now().Unix())
			}
		}
		tr.Reset(1e9)
	}
}
