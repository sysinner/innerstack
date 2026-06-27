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
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sysinner/innerstack/v2/internal/inutil"
)

const accessKeyPrefix = "ak_"

var (
	akIdValid     = regexp.MustCompile("^[0-9a-f]{12,32}$")
	akSecretValid = regexp.MustCompile("^[0-9a-zA-Z]{16,64}$")

	akDefault = NewAccessKey()
)

func NewAccessKey() *AccessKey {
	sk, _ := inutil.GenerateSecretKeyBase62(48)
	return &AccessKey{
		Id:     inutil.SeqRandHexString(4, 12),
		Secret: sk,
	}
}

func NewUserAccessKey() *AccessKey {
	ak := NewAccessKey()
	ak.Type = "User"
	return ak
}

func NewAppAccessKey() *AccessKey {
	ak := NewAccessKey()
	ak.Type = "App"
	return ak
}

func (it *AccessKey) Allow(scopes ...string) bool {
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

// scopeMatch checks if the required scope is satisfied by any of the granted scopes.
// Supports wildcard matching: "*" matches everything, "zone:rw" matches "zone:ro".
func scopeMatch(granted []string, required string) bool {
	for _, g := range granted {
		if g == "*" {
			return true
		}
		if g == required {
			return true
		}
		// Write scopes imply read scopes (e.g., "zone:rw" covers "zone:ro")
		if strings.HasSuffix(g, ":rw") && g[:len(g)-3]+":ro" == required {
			return true
		}
	}
	return false
}

func (it *AccessKey) Export() string {
	if it != nil {
		return accessKeyPrefix + fmt.Sprintf("%s_%s", it.Id, it.Secret)
	}
	return ""
}

func ParseAccessKey(ak string) (*AccessKey, error) {
	if !strings.HasPrefix(ak, accessKeyPrefix) {
		return nil, errors.New("invalid access-key format: missing prefix")
	}

	parts := strings.SplitN(ak[len(accessKeyPrefix):], "_", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid access-key structure")
	}

	if !akIdValid.MatchString(parts[0]) ||
		!akSecretValid.MatchString(parts[1]) {
		return nil, errors.New("invalid access-key id format")
	}

	return &AccessKey{Id: parts[0], Secret: parts[1]}, nil
}
