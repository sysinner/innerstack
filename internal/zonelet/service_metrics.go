// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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
	"context"
	"errors"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// AlertList lists built-in alert state records from the store (the
// evaluation loop is write-through, so the store is always current).
func (s *zoneServer) AlertList(
	ctx context.Context, req *inapi.AlertListRequest,
) (*inapi.AlertListResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Zone_Read); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.State != "" &&
		req.State != inapi.AlertStateFiring &&
		req.State != inapi.AlertStateResolved {
		return nil, errors.New("invalid state filter")
	}

	pageSize := int(req.PageSize)
	if pageSize <= 0 {
		pageSize = inapi.AlertPageSizeDefault
	}
	if pageSize > inapi.AlertPageSizeMax {
		pageSize = inapi.AlertPageSizeMax
	}

	resp := &inapi.AlertListResponse{}

	prefix := inapi.NsZoneletAlertPrefix(config.Config.Zonelet.ZoneName)
	rs := data.Zonelet.NewRanger(prefix, append(prefix, 0xff)).
		// kvgo default Limit is 10
		SetLimit(inapi.AlertScanLimit).
		Exec()
	if !rs.OK() && !rs.NotFound() {
		return nil, rs.Error()
	}

	for _, item := range rs.Items {
		a := &inapi.Alert{}
		if err := item.JsonDecode(a); err != nil {
			continue
		}
		if a.Rule == "" || a.Target == "" {
			continue
		}
		if req.Rule != "" && a.Rule != req.Rule {
			continue
		}
		if req.State != "" && a.State != req.State {
			continue
		}
		// Key order is "{rule}/{target}"; the page token is the exclusive
		// cursor of the previous page's last item.
		if req.PageToken != "" && a.Rule+"/"+a.Target <= req.PageToken {
			continue
		}
		if len(resp.Items) >= pageSize {
			// The next exclusive cursor is the page's last item id.
			last := resp.Items[len(resp.Items)-1]
			resp.NextPageToken = last.Rule + "/" + last.Target
			break
		}
		resp.Items = append(resp.Items, a)
	}

	return resp, nil
}

// HostMetrics queries the built-in host metrics history (monstore blocks)
// with an optional field filter, time window and step downsampling.
func (s *zoneServer) HostMetrics(
	ctx context.Context, req *inapi.HostMetricsRequest,
) (*inapi.HostMetricsResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Zone_Read); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.HostId == "" {
		return nil, errors.New("host_id required")
	}

	fields := inapi.MonFields
	if req.Field != "" {
		if !inapi.MonFieldValid(req.Field) {
			return nil, errors.New("invalid field")
		}
		fields = []string{req.Field}
	}

	maxPoints := int64(req.MaxPoints)
	if maxPoints <= 0 {
		maxPoints = inapi.HostMetricsMaxPointsDefault
	}
	if maxPoints > inapi.HostMetricsMaxPointsMax {
		maxPoints = inapi.HostMetricsMaxPointsMax
	}

	resp := &inapi.HostMetricsResponse{}
	for _, field := range fields {
		points, err := monQuery(req.HostId, field,
			req.StartTime, req.EndTime, req.StepSeconds, int(maxPoints))
		if err != nil {
			return nil, err
		}
		resp.Series = append(resp.Series, &inapi.HostMetricsSeries{
			Field:  field,
			Points: points,
		})
	}

	return resp, nil
}
