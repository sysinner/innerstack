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

/**
type userValidator struct {
	at *AccessToken
	// ak *AccessKey
	it *IdentityToken
}

func NewUserValidator(token string) (UserValidator, error) {

	at, err := ParseAccessToken(token)
	if err != nil {
		return nil, err
	}

	return &userValidator{
		at: at,
	}, nil
}

func (it *userValidator) Valid() error {
	if it == nil || it.at == nil || it.at.Claims.Sub == "" {
		return ErrInvalidAccessToken
	}
	return nil
}

func (it *userValidator) AccessToken() *AccessToken {
	if it == nil {
		return &AccessToken{}
	}
	return it.at
}

func (it *userValidator) IsExpired() bool {
	return it.at.IsExpired()
}

func (it *userValidator) Allow(user string, scopes ...string) bool {
	if it == nil || user == "" {
		return false
	}

	if it.at.IsExpired() {
		return false
	}

	// if user == it.Sub ||
	// 	slices.Contains(it.Groups, user) {
	// 	return true
	// }

	// if len(it.Scopes) > 0 && len(scopes) > 0 {
	// 	if len(scopes) > len(it.Scopes) {
	// 		return false
	// 	}
	// 	for _, scope := range scopes {
	// 		if !scopeMatch(it.Scopes, scope) {
	// 			return false
	// 		}
	// 	}
	// 	return true
	// }

	return false
}
*/
