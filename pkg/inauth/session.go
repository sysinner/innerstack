// Copyright 2020 Eryx <evorui at gmail dot com>, All rights reserved.
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
	"slices"
)

type SessionToken struct {
	AccessToken   *AccessToken   `json:"access_token" toml:"access_token"`
	IdentityToken *IdentityToken `json:"identity_token" toml:"identity_token"`
}

func (it *SessionToken) Allow(user string, permission string) bool {

	if it == nil || it.AccessToken == nil || it.IdentityToken == nil {
		return false
	}

	if user != "" && it.AccessToken.Claims.Sub != user {
		return false
	}

	if slices.Contains(it.IdentityToken.Permissions, permission) {
		return true
	}

	return false
}

/**
type sessionManager struct {
	mu      sync.RWMutex
	keyMgr  *AccessKeyManager
	items   map[string]*SessionToken
	cleared int64
}

func NewSessionManager(keyMgr *AccessKeyManager) SessionManager {
	return &sessionManager{
		keyMgr: keyMgr,
		items:  map[string]*SessionToken{},
	}
}

func (it *sessionManager) IsLogin(accessToken string) (*AccessToken, error) {

	at, err := ParseAccessToken(accessToken)
	if err != nil {
		return nil, err
	}

	if _, err := at.Verify(it.keyMgr); err != nil {
		return nil, err
	}

	return at, nil
}

func (it *sessionManager) Token(jti string) *SessionToken {
	if jti == "" {
		return nil
	}

	it.mu.RLock()
	defer it.mu.RUnlock()

	if it.items != nil {
		if token, ok := it.items[jti]; ok &&
			!token.AccessToken.IsExpired() {
			return token
		}
	}

	return nil
}

func (it *sessionManager) RefreshToken(accessToken string, token *IdentityToken) (*SessionToken, error) {

	at, err := ParseAccessToken(accessToken)
	if err != nil {
		return nil, err
	}

	if !at.IsExpired() {
		return nil, errors.New("access token expired")
	}

	if at.Claims.Jti == "" {
		return nil, errors.New("invalid identity token")
	}

	it.clear()

	it.mu.Lock()
	defer it.mu.Unlock()

	session := &SessionToken{
		AccessToken:   at,
		IdentityToken: token,
	}

	it.items[at.Claims.Jti] = session

	return session, nil
}

func (it *sessionManager) clear() {
	t := time.Now().Unix()
	if (it.cleared + 600) > t {
		return
	}

	it.mu.Lock()
	defer it.mu.Unlock()

	it.cleared = t
	dels := []string{}

	for k, v := range it.items {
		if v.AccessToken.Claims.Exp <= t {
			dels = append(dels, k)
		}
	}
	for _, k := range dels {
		delete(it.items, k)
	}
}
*/
