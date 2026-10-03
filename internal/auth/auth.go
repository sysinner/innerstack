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

package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sysinner/innerstack/v2/internal/audit"
	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/internal/data"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

var AuthMgr = &AuthManager{
	keyMgr: inauth.NewAccessKeyManager(),
}

// AuthManager manages access keys and provides gRPC authentication
type AuthManager struct {
	keyMgr *inauth.AccessKeyManager
}

func Setup() error {

	// Load access keys from config (newly created or existing)
	for _, pub := range config.Config.Zonelet.AccessKeys {
		ak, err := inauth.ParseAccessKey(pub.AccessKey)
		if err != nil {
			slog.Warn("load access-key from zone config fail : " + err.Error())
			continue
		}
		ak.Scopes = []string{inapi.AuthScope_Wildcard}
		// The export string carries only id+secret; restore the owner
		// label and type declared alongside it.
		pub.ApplyTo(ak)
		AuthMgr.keyMgr.Set(ak)
		slog.Info("load access-key from zone config",
			"id", ak.Id,
			"user", ak.User,
		)
	}

	if ak := config.Config.Hostlet.AuthKey(); ak != nil {
		AuthMgr.keyMgr.Set(ak)
		slog.Info("load access-key from zone config",
			"host_id", ak.Id,
		)
	}

	return nil
}

// noAuthMethods defines gRPC methods that do not require authentication.
var noAuthMethods = map[string]bool{
	"/inapi.ZoneService/Ping": true,
}

// GrpcAuthInterceptor returns a gRPC unary interceptor for authentication
func (am *AuthManager) GrpcAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

		// Skip authentication for whitelisted methods (e.g. health-check)
		if noAuthMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		// Validate the gRPC credential
		if av, err := inauth.NewGrpcAppValidator(ctx, am.keyMgr); err != nil {
			slog.Warn("auth failed",
				"method", info.FullMethod,
				"error", err,
			)
			// Record the failed attempt (deduped in audit.AuthFailure);
			// the claimed key attributes the user when the kid is known.
			audit.AuthFailure(ctx, info.FullMethod, err, am.claimedKey(ctx))
			return nil, status.Errorf(
				codes.Unauthenticated,
				"authentication failed: %s",
				err.Error(),
			)
		} else {
			ctx = inauth.NewAppContext(ctx, av)
		}

		return handler(ctx, req)
	}
}

// claimedKey recovers the claimed access key from the credential header
// when the kid maps to a known key (wrong signature, replay): an auth
// failure record can then attribute the user the token claims.
func (am *AuthManager) claimedKey(ctx context.Context) *inauth.AccessKey {
	at, err := inauth.ParseAccessTokenWithContext(ctx)
	if err != nil {
		return nil
	}
	return am.keyMgr.Key(at.Header.Kid)
}

// GrpcStreamAuthInterceptor returns a gRPC stream interceptor for authentication
func (am *AuthManager) GrpcStreamAuthInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {

		if av, err := inauth.NewGrpcAppValidator(ss.Context(), am.keyMgr); err != nil {
			slog.Warn("auth failed",
				"method", info.FullMethod,
				"error", err,
			)
			return status.Errorf(codes.Unauthenticated, "authentication failed: %s", err.Error())
		} else {
			ctx := inauth.NewAppContext(ss.Context(), av)
			wrapped := &grpcServerStreamWithContext{ServerStream: ss, ctx: ctx}
			return handler(srv, wrapped)
		}
	}
}

// grpcServerStreamWithContext wraps grpc.ServerStream to carry an authenticated context
type grpcServerStreamWithContext struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *grpcServerStreamWithContext) Context() context.Context {
	return s.ctx
}

// RefreshAccessKeysFromDB loads access keys from the database
func (am *AuthManager) RefreshAccessKeysFromDB() error {

	if data.Zonelet == nil {
		return errors.New("data:zonelet not setup")
	}

	{
		offset := inapi.NsZoneletAccessKey(config.Config.Zonelet.ZoneName, "")
		rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).
			SetLimit(1000).
			Exec()
			// kvgo default Limit is 10

		for _, item := range rs.Items {
			var key inauth.AccessKey
			if err := item.JsonDecode(&key); err != nil {
				slog.Warn("failed to decode access key", "error", err)
				continue
			}
			if key.Id != "" && key.Secret != "" {
				// Revoked keys (user disabled) stay in the DB for
				// listing but must never authenticate again.
				if key.State == inauth.AccessKey_State_Disable {
					continue
				}
				am.keyMgr.Set(&key)
				slog.Debug("auth key loaded from db", "key_id", key.Id)
			}
		}
	}

	{
		offset := inapi.NsHostInfo(config.Config.Zonelet.ZoneName, "")
		rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).
			SetLimit(inapi.Zonelet_MaxHosts).
			Exec()
			// kvgo default Limit is 10

		for _, item := range rs.Items {
			var host inapi.Host
			if err := item.JsonDecode(&host); err != nil {
				slog.Warn("failed to decode access key", "error", err)
				continue
			}
			ak, err := inauth.ParseAccessKey(host.AccessKey)
			if err != nil {
				slog.Warn("load host access-key fail", "host_id", host.Id)
			} else {
				ak.Scopes = []string{
					inapi.AuthScope_Host_Write + ":" + host.Id,
					inapi.AuthScope_Package_Read,
				}
				ak.Type = inauth.AccessKey_Type_Host
				AuthMgr.keyMgr.Set(ak)

				slog.Warn("load host access-key", "host_id", host.Id)
			}
		}
	}

	return nil
}

// SaveAccessKey saves an access key to the database
func (am *AuthManager) SaveAccessKey(key *inauth.AccessKey) error {

	if data.Zonelet == nil {
		return errors.New("data:zonelet not setup")
	}

	if key.Id == "" || key.Secret == "" {
		return errors.New("access key id and secret are required")
	}

	dbKey := inapi.NsZoneletAccessKey(config.Config.Zonelet.ZoneName, key.Id)

	if rs := data.Zonelet.NewWriter(dbKey, key).Exec(); !rs.OK() {
		return errors.New("failed to save access key: " + rs.ErrorMessage())
	}

	// Update in-memory key manager
	am.keyMgr.Set(key)

	slog.Info("access key saved",
		"key_id", key.Id,
		"user", key.User,
	)

	return nil
}

// DeleteAccessKey deletes a DB-managed access key. A key that lives only
// in memory (config-declared bootstrap sysadmin/ingate, or a host key) is
// refused: it is revoked via config or host re-assignment, not this API.
// Unknown keys are an idempotent no-op.
func (am *AuthManager) DeleteAccessKey(keyId string) error {

	if data.Zonelet == nil {
		return errors.New("data:zonelet not setup")
	}

	if keyId == "" {
		return status.Error(codes.InvalidArgument, "access key id is required")
	}

	dbKey := inapi.NsZoneletAccessKey(config.Config.Zonelet.ZoneName, keyId)

	// Read first: the deleter reports OK for a missing key, so only a
	// read tells a DB-managed key from one that lives in memory alone.
	if rs := data.Zonelet.NewReader(dbKey).Exec(); !rs.OK() {
		if !rs.NotFound() {
			return status.Errorf(codes.Internal,
				"failed to load access key: %s", rs.Error())
		}
		// Absent from the DB but known in memory: not user-managed.
		if am.keyMgr.Key(keyId) != nil {
			return status.Errorf(codes.FailedPrecondition,
				"key %s is config-declared, revoke it via innerstack.toml", keyId)
		}
		// Genuinely unknown: no-op success.
		return nil
	}

	if rs := data.Zonelet.NewDeleter(dbKey).Exec(); !rs.OK() && !rs.NotFound() {
		return status.Errorf(codes.Internal, "failed to delete access key: %s", rs.Error())
	}

	am.keyMgr.Del(keyId)

	slog.Info("zonelet access key deleted", "key_id", keyId)

	return nil
}

func (it *AuthManager) KeyMgr() *inauth.AccessKeyManager {
	return it.keyMgr
}

// scanPageSize is the kvgo page size for filtered key scans; a page may
// be short (kvgo also caps by total size), so scans stop on an empty page.
const scanPageSize = 1000

// AccessKeysOf loads the DB-managed access keys of one user (all keys when
// userId is empty), in key-id order with an exclusive-cursor offset.
// hasMore reports matches beyond limit. A filtered scan pages through the
// namespace until limit matches are collected (plus one to prove hasMore)
// or the range is exhausted, so non-matching keys cannot end the scan
// early.
func (am *AuthManager) AccessKeysOf(
	userId, offset string, limit int,
) ([]*inauth.AccessKey, bool, error) {

	if limit <= 0 {
		limit = inapi.AccessKeyListLimitDefault
	}

	prefix := inapi.NsZoneletAccessKey(config.Config.Zonelet.ZoneName, "")
	// kvgo ranges are (lower, upper]: the offset is the exclusive cursor.
	lower := append(bytes.Clone(prefix), offset...)
	upper := append(bytes.Clone(prefix), 0xff)

	var keys []*inauth.AccessKey
	for {
		rs := data.Zonelet.NewRanger(bytes.Clone(lower), bytes.Clone(upper)).
			SetLimit(scanPageSize).
			Exec()
		if !rs.OK() {
			if rs.NotFound() {
				break
			}
			return nil, false, rs.Error()
		}
		if len(rs.Items) == 0 {
			break
		}

		for _, item := range rs.Items {
			var key inauth.AccessKey
			if err := item.JsonDecode(&key); err != nil {
				continue
			}
			if userId != "" && key.User != userId {
				continue
			}
			if len(keys) >= limit {
				// One extra match proves more pages exist.
				return keys, true, nil
			}
			keys = append(keys, &key)
		}

		// Follow the page: the last scanned key is the next exclusive
		// bound.
		lower = bytes.Clone(rs.Items[len(rs.Items)-1].Key)
	}

	return keys, false, nil
}

// SetUserKeysEnabled toggles every DB-managed key of a user: disabling
// marks each key AccessKey_State_Disable (the library-level revocation
// vocabulary enforced by inauth token verification) and evicts it from the
// in-memory manager; enabling restores AccessKey_State_Active.
func (am *AuthManager) SetUserKeysEnabled(userId string, enabled bool) error {

	keys, _, err := am.AccessKeysOf(userId, "", inapi.AccessKeyListLimitMax)
	if err != nil {
		return err
	}

	for _, key := range keys {

		if enabled == (key.State != inauth.AccessKey_State_Disable) {
			continue // already in the target state
		}

		if enabled {
			key.State = inauth.AccessKey_State_Active
		} else {
			key.State = inauth.AccessKey_State_Disable
		}

		dbKey := inapi.NsZoneletAccessKey(config.Config.Zonelet.ZoneName, key.Id)
		if rs := data.Zonelet.NewWriter(dbKey, key).Exec(); !rs.OK() {
			return fmt.Errorf("failed to save access key %s: %s",
				key.Id, rs.ErrorMessage())
		}

		if enabled {
			am.keyMgr.Set(key)
		} else {
			am.keyMgr.Del(key.Id)
		}
	}

	return nil
}
