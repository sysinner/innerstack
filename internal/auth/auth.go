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
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/pkg/inauth"
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
	for _, ak := range config.Config.Zonelet.AccessKeys {
		ak, err := inauth.ParseAccessKey(ak.AccessKey)
		if err != nil {
			slog.Warn("load access-key from zone config fail : " + err.Error())
			continue
		}
		ak.Scopes = []string{inapi.AuthScope_Wildcard}
		AuthMgr.keyMgr.Set(ak)
		slog.Info("load access-key from zone config",
			"id", ak.Id,
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

// GrpcAuthInterceptor returns a gRPC unary interceptor for authentication
func (am *AuthManager) GrpcAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

		// Validate the gRPC credential
		if av, err := inauth.NewGrpcAppValidator(ctx, am.keyMgr); err != nil {
			slog.Warn("auth failed",
				"method", info.FullMethod,
				"error", err,
			)
			return nil, status.Errorf(codes.Unauthenticated, "authentication failed: %s", err.Error())
		} else {
			ctx = inauth.NewAppContext(ctx, av)
		}

		return handler(ctx, req)
	}
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
		rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()

		for _, item := range rs.Items {
			var key inauth.AccessKey
			if err := item.JsonDecode(&key); err != nil {
				slog.Warn("failed to decode access key", "error", err)
				continue
			}
			if key.Id != "" && key.Secret != "" {
				am.keyMgr.Set(&key)
				slog.Debug("auth key loaded from db", "key_id", key.Id)
			}
		}
	}

	{
		offset := inapi.NsHostInfo(config.Config.Zonelet.ZoneName, "")
		rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()

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

// DeleteAccessKey deletes an access key from the database
func (am *AuthManager) DeleteAccessKey(keyId string) error {

	if data.Zonelet == nil {
		return errors.New("data:zonelet not setup")
	}

	if keyId == "" {
		return status.Error(codes.InvalidArgument, "access key id is required")
	}

	dbKey := inapi.NsZoneletAccessKey(config.Config.Zonelet.ZoneName, keyId)

	if rs := data.Zonelet.NewDeleter(dbKey).Exec(); !rs.OK() {
		if !rs.NotFound() {
			return status.Errorf(codes.Internal, "failed to delete access key: %s", rs.Error())
		}
	}

	am.keyMgr.Del(keyId)

	slog.Info("zonelet access key deleted", "key_id", keyId)

	return nil
}

func (it *AuthManager) KeyMgr() *inauth.AccessKeyManager {
	return it.keyMgr
}
