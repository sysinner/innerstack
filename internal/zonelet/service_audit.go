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

	"github.com/sysinner/innerstack/v2/internal/audit"
	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// AuditList queries this node's local audit records (audit.List; newest
// first unless Revert).
func (s *zoneServer) AuditList(
	ctx context.Context, req *inapi.AuditListRequest,
) (*inapi.AuditListResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Audit_Read); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Status != "" && !auditStatusValid(req.Status) {
		return nil, errors.New("invalid status filter")
	}

	items, hasMore, err := audit.List(data.Zonelet, audit.ListOptions{
		Zone:      config.Config.Zonelet.ZoneName,
		TsStart:   req.TsStart,
		TsEnd:     req.TsEnd,
		ActorId:   req.ActorId,
		ActorUser: req.ActorUser,
		Action:    req.Action,
		TargetId:  req.TargetId,
		Status:    req.Status,
		Limit:     int(req.Limit),
		Revert:    req.Revert,
		Offset:    req.Offset,
	})
	if err != nil {
		return nil, err
	}

	return &inapi.AuditListResponse{
		Items:   items,
		HasMore: hasMore,
	}, nil
}

// AuditVerify verifies the hash chain of this node's local store
// (audit.Verify).
func (s *zoneServer) AuditVerify(
	ctx context.Context, req *inapi.AuditVerifyRequest,
) (*inapi.AuditVerifyResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Audit_Read); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	rs := audit.Verify(data.Zonelet, audit.VerifyOptions{
		Zone:        config.Config.Zonelet.ZoneName,
		RetentionMs: config.Config.Audit.RetentionMs(),
	})

	return &inapi.AuditVerifyResponse{
		Valid:    rs.Valid,
		Count:    rs.Count,
		FirstId:  rs.FirstId,
		LastId:   rs.LastId,
		HeadHash: rs.HeadHash,
		BrokenAt: rs.BrokenAt,
	}, nil
}

func auditStatusValid(s string) bool {
	switch s {
	case inapi.AuditStatusOK,
		inapi.AuditStatusDenied,
		inapi.AuditStatusError,
		inapi.AuditStatusUnauthenticated:
		return true
	}
	return false
}
