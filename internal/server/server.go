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

	"github.com/sysinner/incore/v2/internal/config"
	"google.golang.org/grpc"
)

type RpcServer = grpc.Server

var (
	grpcMsgByteMax = 16 * 1024 * 1024
	lis            net.Listener
	server         = grpc.NewServer(
		grpc.MaxMsgSize(grpcMsgByteMax),
		grpc.MaxSendMsgSize(grpcMsgByteMax),
		grpc.MaxRecvMsgSize(grpcMsgByteMax),
	)
	err error
)

func TryRun() error {

	// addr, err := netip.ParseAddrPort(config.Config.Hostlet.LanAddr)
	// if err != nil {
	// 	return err
	// }

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
