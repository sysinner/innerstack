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

package client

import (
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

const (
	grpcMsgByteMax = 12 << 20
)

var (
	rpcClientConns = map[string]*ClientConn{}
	rpcClientMu    sync.Mutex
)

type ClientConn struct {
	*grpc.ClientConn
}

func (c *ClientConn) Close() error {
	return nil // c.ClientConn.Close()
}

func Connect(addr string,
	ak *inauth.AccessKey,
	forceNew bool) (*ClientConn, error) {

	ck := fmt.Sprintf("%s", addr)
	if ak != nil {
		ck += "." + ak.Id
	}

	rpcClientMu.Lock()
	defer rpcClientMu.Unlock()

	if c, ok := rpcClientConns[ck]; ok {
		if forceNew {
			if c.ClientConn != nil {
				c.ClientConn.Close()
			}
			c = nil
			delete(rpcClientConns, ck)
		} else {
			return c, nil
		}
	}

	dialOptions := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(grpcMsgByteMax * 2)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(grpcMsgByteMax * 2)),
	}

	if ak != nil {
		dialOptions = append(dialOptions,
			grpc.WithPerRPCCredentials(inauth.NewGrpcAppCredential(ak)))
	}

	dialOptions = append(dialOptions,
		grpc.WithTransportCredentials(insecure.NewCredentials()))

	c, err := grpc.NewClient(addr, dialOptions...)
	if err != nil {
		return nil, err
	}

	cc := &ClientConn{c}
	rpcClientConns[ck] = cc

	return cc, nil
}
