// Copyright 2026 Eryx <evoruri at gmail dot com>, All rights reserved.
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

package zonelet

import (
	"context"
	"errors"
	"time"

	"github.com/sysinner/innerstack/v2/internal/client"
	"github.com/sysinner/innerstack/v2/internal/status"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

// AppLogs proxies one replica's recent container logs through the hosting
// hostlet: instance name -> app cache -> replica host -> hostlet gRPC ->
// docker logs. Single pull, no follow (docker json-file has no seek-by-time
// cursor and follow would need a stream RPC, both deliberately out of
// scope).
func (s *zoneServer) AppLogs(
	ctx context.Context, req *inapi.AppLogsRequest,
) (*inapi.AppLogsResponse, error) {

	if err := authAllow(ctx, inapi.AuthScope_App_Read); err != nil {
		return nil, err
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if req.Name == "" {
		return nil, errors.New("name required")
	}

	app := gAppInstanceSet.Load(req.Name)
	if app == nil || app.Value == nil || app.Value.Deploy == nil {
		return nil, errors.New("app instance not found")
	}

	// Pick the requested replica, or the first placed one.
	var rep *inapi.AppDeployReplica
	for _, r := range app.Value.Deploy.Replicas {
		if r == nil || r.HostId == "" {
			continue
		}
		if req.ReplicaId != 0 && r.Id != req.ReplicaId {
			continue
		}
		rep = r
		break
	}
	if rep == nil {
		return nil, errors.New("no placed replica")
	}

	kvHost := gHostSet.Load(rep.HostId)
	if kvHost == nil {
		return nil, errors.New("replica host not found")
	}
	host, ok := kvHost.Value.(*inapi.Host)
	if !ok || host.PeerAddr == "" || host.AccessKey == "" {
		return nil, errors.New("replica host address unknown")
	}

	ak, err := inauth.ParseAccessKey(host.AccessKey)
	if err != nil {
		return nil, err
	}

	conn, err := client.Connect(host.PeerAddr, ak, false)
	if err != nil {
		return nil, err
	}

	hctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// The tail default and max are applied by the hostlet (single source).
	resp, err := inapi.NewHostInternalServiceClient(conn).ContainerLogs(hctx,
		&inapi.ContainerLogsRequest{
			Container: inapi.ContainerName(req.Name, rep.Id),
			Tail:      req.Tail,
			StartTime: req.StartTime,
		})
	if err != nil {
		return nil, err
	}

	return &inapi.AppLogsResponse{
		Lines:     resp.Lines,
		Truncated: resp.Truncated,
		HostId:    rep.HostId,
	}, nil
}
