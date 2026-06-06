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
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
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

func NewAccessToken() *AccessToken {

	tn := time.Now().Unix()

	at := &AccessToken{
		Header: TokenHeader{
			Alg: DefaultSigner.Name(),
		},
		Claims: AuthClaims{
			Iat:   tn,
			Exp:   tn + appAuthExp,
			State: uuid.NewString(),
		},
		signer: DefaultSigner,
	}

	return at
}

func ParseAccessToken(accessToken string) (*AccessToken, error) {
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

func ParseAccessTokenWithHttpRequest(req *http.Request) (*AccessToken, error) {
	if req == nil {
		return nil, errors.New("http request not found")
	}

	t := req.Header.Get(AppHttpHeaderKey)
	if t == "" {
		return nil, errors.New("token not found")
	}

	return ParseAccessToken(t)
}

func ParseAccessTokenWithContext(ctx context.Context) (*AccessToken, error) {

	if ctx == nil {
		return nil, errors.New("context not found")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok || len(md) < 1 {
		return nil, errors.New("context not found")
	}

	//
	t, ok := md[AppHttpHeaderKey]
	if !ok || len(t) == 0 || len(t[0]) < 5 {
		return nil, errors.New("token not found")
	}

	token, err := ParseAccessToken(t[0])
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

func (it *AccessToken) Verify(keyMgr *AccessKeyManager) (*AccessKey, error) {

	ak := keyMgr.Key(it.Header.Kid)
	if ak == nil {
		return nil, fmt.Errorf("access-key(%s) not found", it.Header.Kid)
	}

	if ak.State == AccessKey_State_Disable {
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

func (it *AccessToken) SignToken(keyMgr *AccessKeyManager) (string, error) {

	var ak *AccessKey

	if it.Header.Kid == "" {
		ak = keyMgr.RandKey()
		it.Header.Kid = ak.Id
	} else {
		ak = keyMgr.Key(it.Header.Kid)
		if ak == nil {
			return "", fmt.Errorf("access-key(%s) not found", it.Header.Kid)
		}
	}

	if ak.State == AccessKey_State_Disable {
		return "", fmt.Errorf("access-key(%s) is disabled", it.Header.Kid)
	}

	tn := time.Now().Unix()

	if it.Claims.Iat == 0 {
		it.Claims.Iat = tn
	}

	if it.Claims.Exp <= it.Claims.Iat {
		it.Claims.Exp = it.Claims.Iat + appAuthExp
	}

	if it.signer == nil || it.signer.Name() != it.Header.Alg {
		it.Header.Alg = DefaultSigner.Name()
		it.signer = Signers.Signer(it.Header.Alg)
	}

	signingString := bytesEncode(jsonEncode(it.Header)) + "." +
		bytesEncode(jsonEncode(it.Claims))

	bs, err := it.signer.Sign(signingString, ak.Secret)
	if err != nil {
		return "", nil
	}

	signString := bytesEncode(bs)

	return signingString + "." + signString, nil
}
