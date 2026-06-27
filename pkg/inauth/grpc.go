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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type grpcAppCredential struct {
	ac AppCredential
}

func NewGrpcAppCredential(ak *AccessKey) credentials.PerRPCCredentials {
	return grpcAppCredential{
		ac: NewAppCredential(ak),
	}
}

func (s grpcAppCredential) GetRequestMetadata(
	ctx context.Context, uri ...string,
) (map[string]string, error) {
	return map[string]string{
		AppHttpHeaderKey: s.ac.AuthToken(),
	}, nil
}

func (s grpcAppCredential) RequireTransportSecurity() bool {
	return false
}

func NewGrpcAppValidator(ctx context.Context, keyMgr *AccessKeyManager) (AppValidator, error) {

	if ctx == nil {
		return nil, status.Errorf(codes.Unauthenticated, "no context found")
	}

	if keyMgr == nil {
		return nil, status.Errorf(codes.Unauthenticated, "no AccessKeyManager found")
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "metadata is not provided")
	}

	values := md[AppHttpHeaderKey]
	if len(values) == 0 {
		return nil, status.Errorf(codes.Unauthenticated, "authorization token is missing")
	}

	av, err := NewAppValidator(values[0])
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	if err = av.Verify(keyMgr); err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	return av, nil
}
