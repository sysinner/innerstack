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

const (
	appHttpHeaderName = "x-inauth2"

	appAuthContextKey = "inauth_ctx"

	userAppAuthTtlMin int64 = 600        // seconds
	userAppAuthTtlMax int64 = 86400 * 30 // seconds

	appAuthExp int64 = 60
)

type AppCredential interface {
	AuthToken() string
}

type AppValidator interface {
	Verify(keyMgr *AccessKeyManager) error
	AccessKey() *AccessKey
	Allow(scopes ...string) bool
}

type TokenHeader struct {
	Alg string `json:"alg" toml:"alg"`
	Typ string `json:"typ,omitempty" toml:"typ,omitempty"`
	Kid string `json:"kid,omitempty" toml:"kid,omitempty"`
}

type AuthClaims struct {
	Jti string `json:"jti,omitempty" toml:"jti,omitempty"` // JWT ID
	Iat int64  `json:"iat" toml:"iat"`                     // Issued At Time
	Exp int64  `json:"exp" toml:"exp"`
	Sub string `json:"sub,omitempty" toml:"sub,omitempty"`

	State string `json:"state,omitempty" toml:"state,omitempty"`
}
