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
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/sysinner/innerstack/v2/internal/auth"
	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

// userKeysMu serializes key creation (AccessKeySet) against the UserSet
// disable/enable cascade and the UserDelete guard: the cascade snapshots a
// user's keys, so a key created inside that window must either join the
// snapshot (and get revoked) or be rejected against the already-disabled
// user row.
var userKeysMu sync.Mutex

// UserSet creates a user or updates its description/state. Disabling a user
// revokes all of its DB-managed access keys until re-enabled. Config-declared
// keys (the bootstrap sysadmin/ingate credentials in innerstack.toml) carry a
// user label for audit attribution but are intentionally outside this
// lifecycle: they are the root of trust and can only be revoked via config.
func (s *zoneServer) UserSet(
	ctx context.Context, req *inapi.UserSetRequest,
) (*inapi.UserSetResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Zone_Write); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if err := inapi.NameValid(req.Name); err != nil {
		return nil, fmt.Errorf("invalid user name: %w", err)
	}

	existing, err := userGet(req.Name)
	if err != nil {
		return nil, err
	}

	state := req.State
	if state == "" {
		state = inapi.UserStateActive
		if existing != nil {
			state = existing.State
		}
	}
	if !validUserState(state) {
		return nil, errors.New("invalid state: active | disabled")
	}

	now := time.Now().UnixMilli()
	item := &inapi.User{
		Name:    req.Name,
		State:   state,
		Created: now,
		Updated: now,
	}
	if existing != nil {
		item.Created = existing.Created
		item.Description = existing.Description
	}
	if req.Description != "" {
		item.Description = req.Description
	}

	// The key cascade runs before the user row is persisted: a failed
	// cascade leaves the row at its old state, so a retry re-runs it
	// (SetUserKeysEnabled skips keys already in the target state) instead
	// of reporting success with keys stranded in the wrong state.
	if existing == nil || existing.State != state {
		userKeysMu.Lock()
		err = auth.AuthMgr.SetUserKeysEnabled(
			req.Name, state == inapi.UserStateActive)
		userKeysMu.Unlock()
		if err != nil {
			return nil, err
		}
	}

	if rs := data.Zonelet.NewWriter(
		inapi.NsZoneletUser(config.Config.Zonelet.ZoneName, req.Name), item).
		Exec(); !rs.OK() {
		return nil, fmt.Errorf("failed to save user: %s", rs.ErrorMessage())
	}

	return &inapi.UserSetResponse{Item: item}, nil
}

// UserGet retrieves one user by name.
func (s *zoneServer) UserGet(
	ctx context.Context, req *inapi.UserGetRequest,
) (*inapi.UserGetResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Zone_Read); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Name == "" {
		return nil, errors.New("user name is required")
	}

	user, err := userGet(req.Name)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, grpcstatus.Errorf(codes.NotFound, "user %s not found", req.Name)
	}

	return &inapi.UserGetResponse{Item: user}, nil
}

// UserDelete removes a user; rejected while any access key still references
// it so revocation stays explicit.
func (s *zoneServer) UserDelete(
	ctx context.Context, req *inapi.UserDeleteRequest,
) (*inapi.UserDeleteResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Zone_Write); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Name == "" {
		return nil, errors.New("user name is required")
	}

	// The key check and row removal are serialized against key creation
	// so no key can be added to a user that is being deleted.
	userKeysMu.Lock()
	defer userKeysMu.Unlock()

	if keys, _, err := auth.AuthMgr.AccessKeysOf(
		req.Name, "", inapi.AccessKeyListLimitMax); err != nil {
		return nil, err
	} else if len(keys) > 0 {
		return nil, fmt.Errorf(
			"user %s still has %d access keys, revoke them first",
			req.Name, len(keys))
	}

	if rs := data.Zonelet.NewDeleter(
		inapi.NsZoneletUser(config.Config.Zonelet.ZoneName, req.Name)).
		Exec(); !rs.OK() && !rs.NotFound() {
		return nil, fmt.Errorf("failed to delete user: %s", rs.Error())
	}

	return &inapi.UserDeleteResponse{}, nil
}

// UserList lists users in name order with an exclusive-cursor offset.
func (s *zoneServer) UserList(
	ctx context.Context, req *inapi.UserListRequest,
) (*inapi.UserListResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Zone_Read); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.State != "" && !validUserState(req.State) {
		return nil, errors.New("invalid state filter: active | disabled")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = inapi.UserListLimitDefault
	}
	if limit > inapi.UserListLimitMax {
		limit = inapi.UserListLimitMax
	}

	prefix := inapi.NsZoneletUser(config.Config.Zonelet.ZoneName, "")
	// kvgo ranges are (lower, upper]: the offset is the exclusive cursor.
	lower := append(bytes.Clone(prefix), req.Offset...)
	upper := append(bytes.Clone(prefix), 0xff)

	// The state-filtered scan pages through the namespace until limit
	// matches are collected (plus one to prove hasMore) or the range is
	// exhausted; a bounded first page would end the scan at filtered-out
	// users.
	resp := &inapi.UserListResponse{}
	for {
		rs := data.Zonelet.NewRanger(bytes.Clone(lower), bytes.Clone(upper)).
			SetLimit(userScanPageSize).
			Exec()
		if !rs.OK() {
			if rs.NotFound() {
				break
			}
			return nil, rs.Error()
		}
		if len(rs.Items) == 0 {
			break
		}

		for _, item := range rs.Items {

			var user inapi.User
			if err := item.JsonDecode(&user); err != nil {
				continue
			}
			if req.State != "" && user.State != req.State {
				continue
			}

			if len(resp.Items) >= limit {
				resp.HasMore = true
				return resp, nil
			}
			resp.Items = append(resp.Items, &user)
		}

		// Follow the page: the last scanned user is the next exclusive
		// bound.
		lower = bytes.Clone(rs.Items[len(rs.Items)-1].Key)
	}

	return resp, nil
}

// AccessKeySet creates a user access key. Each call generates a fresh key;
// retrying a failed request creates an additional key (revoke the unwanted
// one via AccessKeyDelete).
func (s *zoneServer) AccessKeySet(
	ctx context.Context, req *inapi.AccessKeySetRequest,
) (*inapi.AccessKeySetResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Zone_Write); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.User == "" {
		return nil, errors.New("user is required")
	}
	if len(req.Scopes) == 0 {
		return nil, errors.New("at least one scope is required")
	}

	// The lock spans the state check and the key install: a disable
	// cascade holding the lock snapshots this key (and revokes it), while
	// a completed cascade leaves the row disabled and the check below
	// rejects the creation.
	userKeysMu.Lock()
	defer userKeysMu.Unlock()

	user, err := userGet(req.User)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user %s not found", req.User)
	}
	if user.State == inapi.UserStateDisabled {
		return nil, fmt.Errorf("user %s is disabled", req.User)
	}

	ak := inauth.NewUserAccessKey()
	ak.User = req.User
	ak.Scopes = req.Scopes
	ak.Description = req.Description

	if err := auth.AuthMgr.SaveAccessKey(ak); err != nil {
		return nil, err
	}

	return &inapi.AccessKeySetResponse{
		KeyId:     ak.Id,
		AccessKey: ak.Export(),
	}, nil
}

// AccessKeyDelete revokes an access key; idempotent (unknown keys are a
// no-op success).
func (s *zoneServer) AccessKeyDelete(
	ctx context.Context, req *inapi.AccessKeyDeleteRequest,
) (*inapi.AccessKeyDeleteResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Zone_Write); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if err := auth.AuthMgr.DeleteAccessKey(req.KeyId); err != nil {
		return nil, err
	}

	return &inapi.AccessKeyDeleteResponse{}, nil
}

// AccessKeyList lists access keys without secrets, in key-id order.
func (s *zoneServer) AccessKeyList(
	ctx context.Context, req *inapi.AccessKeyListRequest,
) (*inapi.AccessKeyListResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_Zone_Read); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	limit := int(req.Limit)
	if limit <= 0 {
		limit = inapi.AccessKeyListLimitDefault
	}
	if limit > inapi.AccessKeyListLimitMax {
		limit = inapi.AccessKeyListLimitMax
	}

	keys, hasMore, err := auth.AuthMgr.AccessKeysOf(req.User, req.Offset, limit)
	if err != nil {
		return nil, err
	}

	resp := &inapi.AccessKeyListResponse{HasMore: hasMore}
	for _, key := range keys {
		resp.Items = append(resp.Items, &inapi.AccessKeyItem{
			KeyId:       key.Id,
			User:        key.User,
			Type:        key.Type,
			Description: key.Description,
			Scopes:      key.Scopes,
			State:       key.State,
		})
	}

	return resp, nil
}

// validUserState reports whether s is a settable User.state value.
func validUserState(s string) bool {
	return s == inapi.UserStateActive || s == inapi.UserStateDisabled
}

// userScanPageSize is the kvgo page size for filtered user scans.
const userScanPageSize = 1000

// userGet loads one user entity; nil with a nil error when absent.
func userGet(name string) (*inapi.User, error) {

	rs := data.Zonelet.NewReader(
		inapi.NsZoneletUser(config.Config.Zonelet.ZoneName, name)).Exec()
	if !rs.OK() {
		if rs.NotFound() {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load user %s: %s", name, rs.Error())
	}

	var user inapi.User
	if err := rs.Item().JsonDecode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode user %s: %w", name, err)
	}
	return &user, nil
}
