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

package audit

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/sysinner/innerstack/v2/internal/config"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

const (
	// errorMaxLen bounds the error field (may echo user input).
	errorMaxLen = 256

	// authFailureWindowSec dedups auth-failure records per (kid, method,
	// code): the first failure is recorded, later ones accumulate into the
	// next record's SuppressedCount.
	authFailureWindowSec = 60

	// authFailureSweepEvery lazily sweeps the dedup map on insert (no
	// background goroutine).
	authFailureSweepEvery = 1024

	// authFailureEntryTTL drops idle dedup entries after one day.
	authFailureEntryTTL = 24 * 3600
)

// Interceptor returns the audit unary interceptor. It must chain after auth
// (reads the actor from ctx). Nil Mgr = pass-through. No stream RPCs today;
// add a stream interceptor if one is introduced.
func Interceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {

		if Mgr == nil {
			return handler(ctx, req)
		}

		resp, err := handler(ctx, req)

		if rec := buildRecord(ctx, info.FullMethod, req, resp, err); rec != nil {
			Mgr.emit(rec)
		}

		return resp, err
	}
}

// buildRecord assembles an AuditRecord from a finished unary call, or nil
// when not audited: unknown methods are recorded only when denied, and an
// extractor may decline.
func buildRecord(
	ctx context.Context, method string,
	req, resp any, rpcErr error,
) *inapi.AuditRecord {

	st := statusOf(rpcErr)

	ext, known := extractors[method]
	if !known && st != inapi.AuditStatusDenied {
		return nil
	}

	var (
		targetType, targetId string
		detail               map[string]any
		audited              = true
	)
	if known {
		targetType, targetId, detail, audited = ext(req, resp, rpcErr)
		if !audited && st != inapi.AuditStatusDenied {
			return nil
		}
	}

	rec := &inapi.AuditRecord{
		Zone:       config.Config.Zonelet.ZoneName,
		Action:     method,
		Status:     st,
		SourceIp:   peerIP(ctx),
		TargetType: targetType,
		TargetId:   targetId,
		Detail:     encodeDetail(detail),
	}

	if rpcErr != nil {
		rec.Error = truncateString(rpcErr.Error(), errorMaxLen)
	}

	fillActor(ctx, rec)

	return rec
}

// AuthFailure records an authentication failure from the auth interceptor
// (the audit interceptor never runs on that path). The kid is recovered
// from the credential token when parseable.
func AuthFailure(ctx context.Context, method string, authErr error) {

	if Mgr == nil {
		return
	}

	now := time.Now().Unix()

	kid := credentialKid(ctx)
	k := kid + "\x00" + method + "\x00" + codes.Unauthenticated.String()

	Mgr.authFailures.mu.Lock()

	if e := Mgr.authFailures.items[k]; e != nil && now-e.window < authFailureWindowSec {
		e.suppressed++
		Mgr.authFailures.mu.Unlock()
		return
	}

	var suppressed int32
	if e := Mgr.authFailures.items[k]; e != nil {
		suppressed = e.suppressed
	}

	Mgr.authFailures.items[k] = &authFailureEntry{window: now}
	Mgr.authFailures.inserts++
	if Mgr.authFailures.inserts%authFailureSweepEvery == 0 {
		Mgr.authFailures.sweepLocked(now)
	}
	Mgr.authFailures.mu.Unlock()

	rec := &inapi.AuditRecord{
		Zone:            config.Config.Zonelet.ZoneName,
		Action:          method,
		Status:          inapi.AuditStatusUnauthenticated,
		ActorId:         kid,
		ActorType:       inapi.AuditActorAnonymous,
		SourceIp:        peerIP(ctx),
		SuppressedCount: suppressed,
	}
	if authErr != nil {
		rec.Error = truncateString(authErr.Error(), errorMaxLen)
	}

	Mgr.emit(rec)
}

// authFailureEntry is the dedup state of one (kid, method, code) key.
type authFailureEntry struct {
	window     int64 // unix seconds of the last emitted record
	suppressed int32 // failures accumulated since that record
}

type authFailureCache struct {
	mu      sync.Mutex
	items   map[string]*authFailureEntry
	inserts int64
}

func (it *authFailureCache) sweepLocked(now int64) {
	for k, e := range it.items {
		if now-e.window > authFailureEntryTTL {
			delete(it.items, k)
		}
	}
}

// statusOf maps a gRPC error to an audit status: OK -> ok,
// PermissionDenied -> denied, Unauthenticated -> unauthenticated, anything
// else -> error.
func statusOf(err error) string {
	if err == nil {
		return inapi.AuditStatusOK
	}
	switch status.Code(err) {
	case codes.PermissionDenied:
		return inapi.AuditStatusDenied
	case codes.Unauthenticated:
		return inapi.AuditStatusUnauthenticated
	default:
		return inapi.AuditStatusError
	}
}

// fillActor copies the authenticated access key identity into the record.
// Host-scoped keys (no Type, host:rw:{id} scope) are labeled Host; keys
// without any identity fall back to Anonymous.
func fillActor(ctx context.Context, rec *inapi.AuditRecord) {

	ak := inauth.AppContext(ctx).AccessKey()

	if ak.Id == "" {
		rec.ActorType = inapi.AuditActorAnonymous
		return
	}

	rec.ActorId = ak.Id
	rec.ActorUser = ak.User

	switch {
	case ak.Type != "":
		rec.ActorType = ak.Type
	case hasHostScope(ak.Scopes):
		rec.ActorType = inapi.AuditActorHost
	default:
		rec.ActorType = inapi.AuditActorUser
	}
}

func hasHostScope(scopes []string) bool {
	for _, s := range scopes {
		if strings.HasPrefix(s, inapi.AuthScope_Host_Write+":") {
			return true
		}
	}
	return false
}

// credentialKid extracts the access key id from the incoming credential
// header when the token is parseable (an unknown or wrongly signed key
// still has a well-formed header; the kid lives in the token header, not
// the verified access key).
func credentialKid(ctx context.Context) string {
	at, err := inauth.ParseAccessTokenWithContext(ctx)
	if err != nil {
		return ""
	}
	return at.Header.Kid
}

// peerIP returns the IP part of the gRPC peer address.
func peerIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(p.Addr.String())
	if err != nil {
		return p.Addr.String()
	}
	return host
}

// truncateString bounds a field to n bytes, cutting on a rune boundary when
// possible so no invalid UTF-8 is stored.
func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for i := len(s) - 1; i >= 0 && !utf8.RuneStart(s[i]); i-- {
		s = s[:len(s)-1]
	}
	return s
}
