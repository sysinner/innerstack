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

package apiserver

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"time"

	hauth2 "github.com/hooto/hauth/go/v2"
	"github.com/hooto/iam/iamclient"
	"github.com/lynkdb/lynkapi/go/lynkapi"
	"github.com/sysinner/incore/config"
	"github.com/sysinner/incore/inutils"
	"github.com/sysinner/incore/status"
	"github.com/sysinner/incore/v2/states"
)

type ApiService struct {
}

func (it *ApiService) PreMethod(
	ctx context.Context,
) (context.Context, error) {

	if !status.IsZoneMaster() {
		return ctx, lynkapi.NewClientError("Invalid Zone MainNode Address")
	}

	// hauth1 兼容
	if v := ctx.Value(iamclient.AccessTokenKey); v != nil {
		if s, ok := v.(string); ok {
			us, err := iamclient.Instance(s)
			if err != nil {
				return ctx, err
			}
			if !us.IsLogin() {
				return ctx, errors.New("login required")
			}
			jti := inutils.Uint32ToHexString(crc32.ChecksumIEEE([]byte(us.UserName)))
			session := states.SessionTokenManager.Token(jti)
			if session == nil {
				if _, err := states.SessionTokenManager.ReSign("", hauth2.IdentityToken{
					Jti: jti,
					Iat: time.Now().Unix(),
					Exp: time.Now().Unix() + 86400,
					Sub: us.UserName,
				}); err != nil {
					return ctx, err
				}
				session = states.SessionTokenManager.Token(jti)
				if session == nil {
					return ctx, lynkapi.NewClientError("Auth Denied")
				}
			}
			return context.WithValue(ctx, hauth2.AuthContextKey, session), nil
		}
	}

	token, err := hauth2.NewAccessTokenWithContext(ctx)
	if err != nil {
		return ctx, err
	}

	if token.IsExpired() {
		return ctx, lynkapi.NewAuthExpiredError(fmt.Sprintf("kid : %s", token.Header.Kid))
	}

	ak, err := token.Verify(config.KeyMgr)
	if err != nil {
		return ctx, err
	}
	if ak.Type == "App" {
		states.SessionTokenManager.RefreshToken(hauth2.IdentityToken{
			Jti: token.Claims.Jti,
			Iat: time.Now().Unix(),
			Exp: time.Now().Unix() + 60,
			Sub: ak.User,

			Type:   ak.Type,
			Scopes: ak.Scopes,
		})
		session := states.SessionTokenManager.Token(token.Claims.Jti)
		if session == nil {
			return ctx, lynkapi.NewClientError("Auth Denied")
		}
		return context.WithValue(ctx, hauth2.AuthContextKey, session), nil
	}

	session := states.SessionTokenManager.Token(token.Claims.Jti)
	if session == nil {
		return ctx, lynkapi.NewClientError("Auth Denied")
	}

	return context.WithValue(ctx, hauth2.AuthContextKey, session), nil
}
