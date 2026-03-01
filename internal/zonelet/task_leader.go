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

package zonelet

import (
	"log/slog"

	inapi2 "github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/status"
)

// refresh zone-master leader ttl
func leaderRefresh() (forceRefresh bool, err error) {

	if config.Config.Zonelet.ZoneId == "" {
		return false, nil
	}

	var (
		zmLeaderKey = inapi2.NsZoneletLeader(config.Config.Zonelet.ZoneId)
	)

	if status.IsZoneletLeader() {

		if rs := data.Zonelet.NewWriter(
			zmLeaderKey, config.Config.Hostlet.HostId).
			SetPrevChecksum(config.Config.Hostlet.HostId).
			SetTTL(status.ZoneletLeaderTTL).Exec(); rs.OK() {

			status.ZoneletLeaderVersion = rs.Item().Meta.Version
			status.ZoneletLeaderUpdated = rs.Item().Meta.Updated

			slog.Debug("zonelet/leader refresh",
				"host_id", config.Config.Hostlet.HostId,
				"version", rs.Item().Meta.Version,
			)

		} else {
			slog.Warn("zonelet/leader refresh",
				"err", rs.ErrorMessage())

		}

		return false, nil
	}

	if rs := data.Zonelet.NewReader(zmLeaderKey).Exec(); rs.NotFound() {

		if rs2 := data.Zonelet.NewWriter(
			zmLeaderKey, config.Config.Hostlet.HostId).
			SetPrevChecksum(config.Config.Hostlet.HostId).
			SetTTL(status.ZoneletLeaderTTL).Exec(); rs2.OK() {

			status.ZoneletLeader = config.Config.Hostlet.HostId
			status.ZoneletLeaderVersion = rs2.Item().Meta.Version
			status.ZoneletLeaderUpdated = rs2.Item().Meta.Updated
			forceRefresh = true

			slog.Info("zonelet/leader new",
				"host_id", config.Config.Hostlet.HostId,
				"version", rs2.Item().Meta.Version)

		} else {
			slog.Warn("zonelet/leader new fail",
				"host_id", config.Config.Hostlet.HostId,
				"err", rs2.ErrorMessage(),
			)
		}

	} else if rs.OK() && len(rs.Items) > 0 {

		hostId := rs.Item().StringValue()
		if inapi2.ObjectIdValid.MatchString(hostId) &&
			(hostId != status.ZoneletLeader ||
				rs.Items[0].Meta.Version > status.ZoneletLeaderVersion) {

			status.ZoneletLeader = hostId
			status.ZoneletLeaderVersion = rs.Items[0].Meta.Version

			slog.Warn("zonelet leader refresh",
				"host_id", hostId,
				"version", status.ZoneletLeaderVersion,
				"expired", rs.Items[0].Meta.Expired)
		}

		status.ZoneletLeaderUpdated = int64(rs.Items[0].Meta.Updated)

	} else {
		slog.Warn("zonelet leader refresh fail",
			"err", rs.ErrorMessage())
	}

	return forceRefresh, nil
}
