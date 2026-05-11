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

package hostlet

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/pkg/inauth"
)

type hostInternalServer struct {
	inapi.UnimplementedHostInternalServiceServer
}

func NewInternalServer() inapi.HostInternalServiceServer {
	return &hostInternalServer{}
}

func (s *hostInternalServer) HostInit(
	ctx context.Context, req *inapi.HostInitRequest,
) (*inapi.HostInitResponse, error) {

	// Only allow callers with host:rw:<host_id> scope on this host
	if !inauth.AppContext(ctx).Allow(fmt.Sprintf("%s:%s", inapi.AuthScope_Host_Write, config.Config.Hostlet.HostId)) {
		return nil, fmt.Errorf("auth fail: caller not authorized for host %s", config.Config.Hostlet.HostId)
	}

	if len(req.ZoneHosts) > 0 {
		config.Config.Server.ZoneHosts = req.ZoneHosts
		if err := config.Flush(); err != nil {
			return nil, err
		}
		slog.Warn("hostlet init-host",
			"zone_hosts", req.ZoneHosts,
		)
	}

	return &inapi.HostInitResponse{
		HostId: config.Config.Hostlet.HostId,
		Status: hostStatus.clone(),
	}, nil
}
