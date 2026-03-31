// Copyright 2015 Eryx <evorui аt gmаil dοt cοm>, All rights reserved.
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

package server

import (
	"fmt"
	"log/slog"
	"net"

	"google.golang.org/grpc"

	"github.com/sysinner/incore/v2/internal/auth"
	"github.com/sysinner/incore/v2/internal/config"
)

type RpcServer = grpc.Server

var (
	grpcMsgByteMax = 16 * 1024 * 1024
	lis            net.Listener
	server         *grpc.Server
	err            error
)

// Setup initializes the gRPC server with optional interceptors
func Setup() error {
	opts := []grpc.ServerOption{
		grpc.MaxSendMsgSize(grpcMsgByteMax),
		grpc.MaxRecvMsgSize(grpcMsgByteMax),
		grpc.ChainUnaryInterceptor(auth.AuthMgr.GrpcAuthInterceptor()),
		grpc.ChainStreamInterceptor(auth.AuthMgr.GrpcStreamAuthInterceptor()),
	}

	server = grpc.NewServer(opts...)

	lis, err = net.Listen("tcp", fmt.Sprintf(":%d", config.Config.Server.PeerPort))
	if err != nil {
		return err
	}

	return nil
}

func Run() {
	server.Serve(lis)
	slog.Info("server quit")
}

func Close() {
	server.GracefulStop()
}

func RegisterServer(fn func(s *RpcServer)) error {
	fn(server)
	return nil
}
