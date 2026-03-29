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
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/sysinner/incore/v2/internal/inutil"
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
		if !slices.Contains(it.Scopes, scope) {
			return false
		}
	}
	return true
}

const accessKeyPrefix = "ak_"

var akIdValid = regexp.MustCompile("^[0-9a-f]{12,16}$")

func (it *AccessKey) Export() string {
	if it != nil {
		raw := fmt.Sprintf("%s:%s", it.Id, it.Secret)
		enc := base64.RawURLEncoding.EncodeToString([]byte(raw))
		return accessKeyPrefix + enc
	}
	return ""
}

func ParseAccessKey(ak string) (*AccessKey, error) {
	if !strings.HasPrefix(ak, accessKeyPrefix) {
		return nil, errors.New("invalid access-key format: missing prefix")
	}

	raw := strings.TrimPrefix(ak, accessKeyPrefix)
	dec, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(string(dec), ":", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid access-key structure")
	}

	if !akIdValid.MatchString(parts[0]) {
		return nil, errors.New("invalid access-key id format")
	}

	return &AccessKey{Id: parts[0], Secret: parts[1]}, nil
}
