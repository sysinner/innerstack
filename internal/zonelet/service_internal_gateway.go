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

package zonelet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/lynkdb/lynkapi/go/lynkapi"

	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/status"
	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/pkg/inauth"
)

func (it *zoneInternalServer) GatewayIngressDeployList(
	ctx context.Context,
	req *inapi.GatewayIngressDeployListRequest,
) (*inapi.GatewayIngressDeployListResponse, error) {

	rsp := &inapi.GatewayIngressDeployListResponse{
		Revision: 1,
	}

	if !status.IsZoneletLeader() {
		return nil, lynkapi.NewClientError("Invalid Zone MainNode Address")
	}

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_GatewayIngressDeploy_Read) {
		return nil, errors.New("auth fail")
	}

	var (
		items []*inapi.GatewayIngress
		apps  = map[string]*inapi.AppInstance{}
	)

	{ // load domain list
		var (
			offset = inapi.NsZoneletGatewayIngress(config.Config.Zonelet.ZoneName, "")
			rs     = data.Zonelet.NewRanger(offset, append(offset, 0xff)).
				SetLimit(10000).Exec() // TODO
		)

		for _, v := range rs.Items {
			var item inapi.GatewayIngress
			if err := v.JsonDecode(&item); err != nil {
				continue
			}
			if item.Meta == nil {
				item.Meta = &inapi.Metadata{}
			}

			if item.Action != inapi.GatewayIngressActionEnable || len(item.Routes) == 0 {
				continue
			}

			item.Meta.Version = v.Meta.Version

			items = append(items, &item)
		}
	}

	if len(items) == 0 {
		return rsp, nil
	}

	{ // load app list
		offset := inapi.NsAppInstance(config.Config.Zonelet.ZoneName, "")
		rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).
			SetLimit(10000).Exec()

		for _, v := range rs.Items {

			var app inapi.AppInstance
			if err := v.JsonDecode(&app); err != nil {
				continue
			}
			app.Revision = v.Meta.Version

			apps[app.InstanceName()] = &app
		}
	}

	for _, item := range items {

		if req.Revision > 0 && req.Revision >= item.Meta.Version {
			continue
		}

		deploy := &inapi.GatewayIngressDeploy{
			Domain:   item.Domain,
			Revision: item.Meta.Version,
		}

		for _, route := range item.Routes {

			if len(route.Targets) == 0 || route.Action != inapi.GatewayIngressActionEnable {
				continue
			}

			p := lynkapi.SlicesSearchFunc(deploy.Routes, func(a *inapi.GatewayIngressDeploy_HttpRoute) bool {
				return route.Path == a.Path
			})
			add := false

			if p == nil {
				p = &inapi.GatewayIngressDeploy_HttpRoute{
					Path: route.Path,
				}
				add = true
			} else {
				p.Targets = nil
			}

			switch route.Type {
			case inapi.GatewayIngressType_Instance:

				ar := strings.Split(route.Targets[0].Backend, ":")
				app, ok := apps[ar[0]]
				if !ok || len(app.Deploy.Replicas) == 0 ||
					app.Deploy.Action != inapi.OpActionStart {
					continue
				}

				appPort, err := strconv.Atoi(ar[1])
				if err != nil || appPort <= 0 || appPort >= 65536 {
					continue
				}

				for _, rep := range app.Deploy.Replicas {
					if rep.State != "" && rep.State != inapi.OpActionStart {
						continue
					}
					v := gHostSet.Load(rep.HostId)
					if v == nil {
						continue
					}
					host := v.Value.(*inapi.Host)
					var (
						hostIp   = ""
						hostPort = 0
					)
					if i := strings.IndexByte(host.PeerAddr, ':'); i > 0 {
						hostIp = host.PeerAddr[:i]
					} else {
						hostIp = host.PeerAddr
					}
					if port := lynkapi.SlicesSearchFunc(rep.ServicePorts, func(a *inapi.AppDeployServicePort) bool {
						return a.Port == uint32(appPort)
					}); port != nil {
						hostPort = int(port.HostPort)
					} else {
						continue
					}
					addr := fmt.Sprintf("%s:%d", hostIp, hostPort)
					if !slices.ContainsFunc(p.Targets, func(t *inapi.GatewayIngressDeploy_HttpRoute_Target) bool {
						return t.Backend == addr
					}) {
						p.Targets = append(p.Targets, &inapi.GatewayIngressDeploy_HttpRoute_Target{
							Backend: addr,
						})
					}
				}

			case inapi.GatewayIngressType_Upstream:
				for _, tgt := range route.Targets {
					ar := strings.Split(tgt.Backend, ":")
					if len(ar) != 2 {
						continue
					}

					var (
						hostIp   = ""
						hostPort = 0
					)

					if ip := net.ParseIP(ar[0]); len(ip) >= 4 {
						hostIp = ip.String()
					}
					if v, err := strconv.Atoi(ar[1]); err == nil && v > 0 && v < 65536 {
						hostPort = int(v)
					} else {
						continue
					}
					addr := fmt.Sprintf("%s:%d", hostIp, hostPort)
					if !slices.ContainsFunc(p.Targets, func(t *inapi.GatewayIngressDeploy_HttpRoute_Target) bool {
						return t.Backend == addr
					}) {
						p.Targets = append(p.Targets, &inapi.GatewayIngressDeploy_HttpRoute_Target{
							Backend: addr,
						})
					}
				}

			case inapi.GatewayIngressType_Redirect:
				for _, tgt := range route.Targets {
					if u, err := url.Parse(tgt.Backend); err == nil {
						if !slices.ContainsFunc(p.Targets, func(t *inapi.GatewayIngressDeploy_HttpRoute_Target) bool {
							return t.Backend == u.String()
						}) {
							p.Targets = append(p.Targets, &inapi.GatewayIngressDeploy_HttpRoute_Target{
								Backend: u.String(),
							})
						}
					}
				}
			}

			if len(p.Targets) == 0 {
				continue
			}

			p.Type = route.Type
			if add {
				deploy.Routes = append(deploy.Routes, p)
			}
		}

		if len(deploy.Routes) == 0 {
			continue
		}

		if item.Options != nil {
			if item.Options.LetsencryptEnable {
				deploy.LetsencryptEnable = true
			}
		}

		rsp.Items = append(rsp.Items, deploy)
	}

	return rsp, nil
}
