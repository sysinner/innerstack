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

type TokenHeader struct {
	Alg string `json:"alg" toml:"alg"`

	Typ string `json:"typ,omitempty" toml:"typ,omitempty"`

	// AccessKey ID
	Kid string `json:"kid,omitempty" toml:"kid,omitempty"`
}

type AuthClaims struct {
	// JWT ID
	Jti string `json:"jti,omitempty" toml:"jti,omitempty"`

	// issuer
	// Iss string `json:"iss,omitempty" toml:"iss,omitempty"`

	// Issued At Time
	Iat int64 `json:"iat" toml:"iat"`

	// Expire time
	Exp int64 `json:"exp" toml:"exp"`

	// user id
	Sub string `json:"sub,omitempty" toml:"sub,omitempty"`

	// app id
	// Aud string `json:"aud,omitempty" toml:"aud,omitempty"`

	State string `json:"state,omitempty" toml:"state,omitempty"`
}

type AppCredential interface {
	AuthToken() string
}

type AppValidator interface {
	Verify(keyMgr *AccessKeyManager) error
	AccessKey() *AccessKey
	Allow(scopes ...string) bool
}

/**
type UserValidator interface {
	Valid() error
	AccessToken() *AccessToken
	// Verify(keyMgr *AccessKeyManager) error
	// AccessKey() *AccessKey
	Allow(user string, scopes ...string) bool
}
*/

type SessionManager interface {
	// Allow(user string, scopes ...string) bool

	IsLogin(accessToken string) (*AccessToken, error)

	Token(jti string) *SessionToken

	RefreshToken(accessToken string, token *IdentityToken) (*SessionToken, error)
}
