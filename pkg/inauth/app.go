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

package inauth

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type appCredential struct {
	ak *AccessKey

	Header TokenHeader
	Claims AuthClaims

	jti string

	signer Signer

	signingString string
	signString    string
}

func NewAppCredential(ak *AccessKey, args ...any) AppCredential {
	ac := &appCredential{
		ak: ak,
	}

	for _, arg := range args {
		if arg == nil {
			continue
		}
		switch v := arg.(type) {
		case Signer:
			ac.signer = v
		}
	}

	if ac.signer == nil {
		ac.signer = DefaultSigner
	}

	return ac
}

func (it *appCredential) AuthToken() string {

	if it.jti == "" {
		it.jti = uuid.NewString()
	}

	tn := time.Now().Unix()

	it.Header = TokenHeader{
		Alg: it.signer.Name(),
		Kid: it.ak.Id,
	}

	it.Claims = AuthClaims{
		Iat: tn,
		Exp: tn + appAuthExp,
	}

	if it.ak.Type == "App" {
		it.Claims.Jti = it.jti
	} else {
		it.Claims.State = uuid.NewString()
	}

	it.signingString = bytesEncode(jsonEncode(it.Header)) + "." +
		bytesEncode(jsonEncode(it.Claims))

	bs, _ := it.signer.Sign(it.signingString, []byte(it.ak.Secret))

	it.signString = bytesEncode(bs)

	return it.signingString + "." + it.signString
}

type appValidator struct {
	at *AccessToken
	ak *AccessKey
}

func NewAppValidator(token string) (AppValidator, error) {

	at, err := ParseAccessToken(token)
	if err != nil {
		return nil, err
	}

	return &appValidator{
		at: at,
	}, nil
}

func (it *appValidator) Verify(keyMgr *AccessKeyManager) error {
	if it.ak == nil {
		ak, err := it.at.Verify(keyMgr)
		if err != nil {
			return err
		}
		it.ak = ak
	}
	return nil
}

func (it *appValidator) AccessKey() *AccessKey {
	if it.ak != nil {
		return it.ak
	}
	return &AccessKey{} // empty access key
}

func (it *appValidator) Allow(scopes ...string) bool {
	if it.ak != nil {
		return it.ak.Allow(scopes...)
	}
	return false
}

func AppContext(ctx context.Context) AppValidator {
	if v := ctx.Value(appAuthContextKey); v != nil {
		if av, ok := v.(AppValidator); ok {
			return av
		}
	}
	return &appValidator{} // empty validator
}

func NewAppContext(ctx context.Context, av AppValidator) context.Context {
	return context.WithValue(ctx, appAuthContextKey, av)
}
