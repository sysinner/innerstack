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
	AppHttpHeaderKey = "x-inauth2"

	appAuthContextKey = "inauth_ctx"

	userAppAuthTtlMin int64 = 600        // seconds
	userAppAuthTtlMax int64 = 86400 * 30 // seconds

	appAuthExp int64 = 60
)

const (
	AccessKey_State_Active  = "active"
	AccessKey_State_Disable = "disable"
)

var (
	ErrInvalidAccessToken   = NewError("invalid access token")
	ErrInvalidIdentityToken = NewError("invalid identity token")
	ErrInvalidAppCredential = NewError("invalid app credential")
	ErrInvalidAppValidator  = NewError("invalid app validator")
	ErrInvalidUserValidator = NewError("invalid user validator")
)

func NewError(msg string) error {
	return &Error{Msg: msg}
}

type Error struct {
	Msg string
}

func (e *Error) Error() string {
	return e.Msg
}
