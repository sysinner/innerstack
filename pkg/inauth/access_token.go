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
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/metadata"
)

type AccessToken struct {
	raw string `json:"-" toml:"-"`

	signingString string
	signString    string

	signer Signer

	Header TokenHeader

	Claims AuthClaims
}

func NewAccessToken(accessToken string) (*AccessToken, error) {
	n := strings.LastIndexByte(accessToken, '.')
	if n < 0 {
		return nil, errors.New("invalid access_token")
	}

	av := &AccessToken{
		raw:           accessToken,
		signingString: accessToken[:n],
		signString:    accessToken[n+1:],
	}

	n = strings.IndexByte(av.signingString, '.')
	if n < 0 {
		return nil, errors.New("invalid access_token")
	}

	b, err := bytesDecode(av.signingString[:n])
	if err != nil {
		return nil, errors.New("invalid access_token")
	}
	if err = jsonDecode(b, &av.Header); err != nil {
		return nil, errors.New("invalid access_token : " + err.Error())
	}

	b, err = bytesDecode(av.signingString[n+1:])
	if err != nil {
		return nil, errors.New("invalid access_token")
	}
	if err = jsonDecode(b, &av.Claims); err != nil {
		return nil, errors.New("invalid access_token : " + err.Error())
	}

	av.signer = Signers.Signer(av.Header.Alg)

	return av, nil
}

func NewAccessTokenWithContext(ctx context.Context) (*AccessToken, error) {

	if ctx == nil {
		return nil, errors.New("context not found")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(md) < 1 {
		return nil, errors.New("context not found")
	}

	//
	t, ok := md[appHttpHeaderName]
	if !ok || len(t) == 0 || len(t[0]) < 5 {
		return nil, errors.New("token not found")
	}

	token, err := NewAccessToken(t[0])
	if err != nil {
		return nil, err
	}

	return token, nil
}

func (it *AccessToken) IsExpired() bool {
	return (it.Claims.Exp <= time.Now().Unix())
}

func (it *AccessToken) String() string {
	return it.raw
}

const (
	AccessKeyStateActive   = "active"
	AccessKeyStateDisabled = "disabled"
)

func (it *AccessToken) Verify(keyMgr *AccessKeyManager) (*AccessKey, error) {

	ak := keyMgr.Key(it.Header.Kid)
	if ak == nil {
		return nil, fmt.Errorf("access-key(%s) not found", it.Header.Kid)
	}

	if ak.State == AccessKeyStateDisabled {
		return nil, fmt.Errorf("access-key(%s) is disabled", it.Header.Kid)
	}

	if it.IsExpired() {
		return nil, errors.New("access-token expired")
	}

	if ak.Type == "App" &&
		absInt64(time.Now().Unix()-it.Claims.Iat) > 60 {
		return nil, errors.New("auth-denied : iat expired")
	}

	b, err := it.signer.Sign(it.signingString, ak.Secret)
	if err != nil {
		return nil, err
	}

	if bytesEncode(b) != it.signString {
		return nil, errors.New("verify denied")
	}

	return ak, nil
}
