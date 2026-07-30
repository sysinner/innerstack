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

package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/hooto/httpsrv/v2"
	"google.golang.org/grpc"

	"github.com/sysinner/innerstack/v2/internal/auth"
	"github.com/sysinner/innerstack/v2/internal/config"
)

type RpcServer = grpc.Server

var (
	grpcMsgByteMax = 16 * 1024 * 1024
	lis            net.Listener
	server         *grpc.Server

	httpListener net.Listener
	httpServer   httpsrv.App

	err error
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

	httpListener, err = net.Listen("tcp", fmt.Sprintf(":%d", config.Config.Server.HttpPort))
	if err != nil {
		return err
	}

	httpServer = httpsrv.New()

	return nil
}

func Run() {
	go func() {
		if err := httpServer.Run(httpListener); err != nil {
			slog.Error("http server run", "err", err)
		}
	}()
	server.Serve(lis)
	slog.Info("server quit")
}

func Close() {
	server.GracefulStop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		slog.Warn("http server shutdown", "err", err)
	}
}

func RegisterServer(fn func(s *RpcServer)) error {
	fn(server)
	return nil
}

// HttpRouter returns the root HTTP router so other packages can mount their
// routes (typically via Group) on the shared HTTP server.
func HttpRouter() httpsrv.Router {
	return httpServer
}
