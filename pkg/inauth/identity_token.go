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

type IdentityToken struct {
	Roles  []string `json:"roles,omitempty" toml:"roles,omitempty"`
	Groups []string `json:"groups,omitempty" toml:"groups,omitempty"`

	// Type string `json:"type,omitempty" toml:"type,omitempty"`

	Scopes []string `json:"scopes,omitempty" toml:"scopes,omitempty"`

	Permissions []string `json:"permissions,omitempty" toml:"permissions,omitempty"`
}

func (it *IdentityToken) Allow(user string, scopes ...string) bool {
	if it == nil || user == "" {
		return false
	}

	if slices.Contains(it.Groups, user) {
		return true
	}

	if len(it.Scopes) > 0 && len(scopes) > 0 {
		if len(scopes) > len(it.Scopes) {
			return false
		}
		for _, scope := range scopes {
			if !scopeMatch(it.Scopes, scope) {
				return false
			}
		}
		return true
	}

	return false
}
