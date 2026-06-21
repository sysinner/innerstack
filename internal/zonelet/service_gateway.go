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
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lynkdb/lynkapi/go/lynkapi"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"

	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/internal/status"
	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/pkg/inauth"
)

func (it *zoneServer) GatewayIngressList(
	ctx context.Context,
	req *inapi.GatewayIngressListRequest,
) (*inapi.GatewayIngressListResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_GatewayIngress_Read) {
		return nil, errors.New("auth fail: missing read scope")
	}

	rsp := &inapi.GatewayIngressListResponse{}

	if !status.IsZoneletLeader() {
		return nil, lynkapi.NewClientError("Invalid Zone MainNode Address")
	}

	// load domain list
	var (
		// session = hauth2.AuthContextSession(ctx)

		offset = inapi.NsZoneletGatewayIngress(config.Config.Zonelet.ZoneName, "")
		rs     = data.Zonelet.NewRanger(offset, offset).
			SetLimit(1000).Exec() // TODO
	)

	for _, v := range rs.Items {
		var item inapi.GatewayIngress
		if err := v.JsonDecode(&item); err != nil || item.Meta == nil {
			continue
		}
		// if !session.Allow(item.Meta.User, inapi.AuthPermSysAll) {
		// 	continue
		// }
		rsp.Items = append(rsp.Items, &item)
	}

	sort.Slice(rsp.Items, func(i, j int) bool {
		return rsp.Items[i].Meta.Updated > rsp.Items[j].Meta.Updated
	})

	return rsp, nil
}

func (it *zoneServer) GatewayIngressInfo(
	ctx context.Context,
	req *inapi.GatewayIngressInfoRequest,
) (*inapi.GatewayIngressInfoResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_GatewayIngress_Read) {
		return nil, errors.New("auth fail: missing read scope")
	}

	if !status.IsZoneletLeader() {
		return nil, lynkapi.NewClientError("Invalid Zone MainNode Address")
	}

	var (
		// session = hauth2.AuthContextSession(ctx)

		rsp  inapi.GatewayIngressInfoResponse
		item inapi.GatewayIngress
		key  = inapi.NsZoneletGatewayIngress(config.Config.Zonelet.ZoneName, req.Name)
		rs   = data.Zonelet.NewReader(key).Exec()
	)

	if rs.NotFound() || len(rs.Items) == 0 {
		return nil, lynkapi.NewNotFoundError("Domain Not Found")
	} else if !rs.OK() {
		return nil, lynkapi.NewInternalServerError(rs.ErrorMessage())
	}

	if err := rs.Items[0].JsonDecode(&item); err != nil {
		return nil, lynkapi.NewInternalServerError(err.Error())
	}

	// if !session.Allow(rsp.Meta.User, inapi.AuthPermSysAll) {
	// 	return nil, lynkapi.NewUnAuthError("Access Denied")
	// }

	rsp.Item = &item

	return &rsp, nil
}

func (it *zoneServer) GatewayIngressSet(
	ctx context.Context,
	req *inapi.GatewayIngressSetRequest,
) (*inapi.GatewayIngressSetResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_GatewayIngress_Write) {
		return nil, errors.New("auth fail: missing write scope")
	}

	if !status.IsZoneletLeader() {
		return nil, lynkapi.NewClientError("Invalid Zone MainNode Address")
	}

	if req.Item == nil || req.Item.Domain == "" {
		return nil, lynkapi.NewNotFoundError("Domain Not Found")
	}

	{
		req.Item.Domain = strings.TrimSpace(strings.ToLower(req.Item.Domain))

		if _, ok := publicsuffix.PublicSuffix(req.Item.Domain); !ok {
			return nil, lynkapi.NewClientError("Invalid Domain Name (TLD)")
		}

		if _, err := idna.ToASCII(req.Item.Domain); err != nil {
			return nil, lynkapi.NewClientError("Invalid Domain Name : " + err.Error())
		}

		// Per RFC 1035:
		// - Total length must not exceed 253 characters
		// - Each label (dot-separated segment) must not exceed 63 characters
		if len(req.Item.Domain) > 253 {
			return nil, lynkapi.NewClientError("Invalid Domain Name : Total length must not exceed 253 characters")
		}
		labels := strings.Split(req.Item.Domain, ".")
		for _, label := range labels {
			if len(label) > 63 {
				return nil, lynkapi.NewClientError("Invalid Domain Name : Each label (dot-separated segment) must not exceed 63 characters")
			}
		}
	}

	var (
		// session = hauth2.AuthContextSession(ctx)

		item inapi.GatewayIngress
		key  = inapi.NsZoneletGatewayIngress(config.Config.Zonelet.ZoneName, req.Item.Domain)
		rs   = data.Zonelet.NewReader(key).Exec()
	)

	// if rs.NotFound() || len(rs.Items) == 0 {
	if !rs.OK() || len(rs.Items) == 0 {

		item.Meta = &inapi.Metadata{
			Id:      inutil.SeqRandHexString(4, 8),
			Created: time.Now().Unix(),
		}

		item.Domain = req.Item.Domain

		// return nil, lynkapi.NewNotFoundError("Domain Not Found")
	} else if !rs.OK() {
		return nil, lynkapi.NewInternalServerError(rs.ErrorMessage())
	} else if err := rs.Items[0].JsonDecode(&item); err != nil {
		return nil, lynkapi.NewInternalServerError(err.Error())
	} else {
		if item.Meta == nil {
			return nil, lynkapi.NewClientError("metadata not found")
		}
		item.Meta.Version = rs.Meta().Version
	}

	{
		// if rsp.Meta.User != "" &&
		// 	!session.Allow(rsp.Meta.User, inapi.AuthPermSysAll) {
		// 	return nil, lynkapi.NewUnAuthError("Access Denied")
		// }

		// if req.Meta.User != "" &&
		// 	!session.Allow(req.Meta.User, inapi.AuthPermSysAll) {
		// 	return nil, lynkapi.NewUnAuthError("Access Denied to bind owner to " + req.Meta.User)
		// }

		// if req.Meta.User != "" {
		// 	rsp.Meta.User = req.Meta.User
		// } else if rsp.Meta.User == "" {
		// 	rsp.Meta.User = session.Sub
		// }
	}

	// validate action value, default to enable
	switch req.Item.Action {
	case inapi.GatewayIngressActionEnable, inapi.GatewayIngressActionDisable:
		item.Action = req.Item.Action
	default:
		item.Action = inapi.GatewayIngressActionEnable
	}
	item.Description = req.Item.Description

	if req.Item.Options != nil {
		item.Options = req.Item.Options
	}

	{
		for _, route := range req.Item.Routes {

			route.Path = filepath.Clean(route.Path)

			switch route.Type {
			case inapi.GatewayIngressType_Instance:
				for _, tgt := range route.Targets {
					ups := strings.Split(tgt.Backend, ":")
					if len(ups) != 2 {
						return nil, lynkapi.NewClientError("Invalid AppInstance Name:Port")
					}
					if err := inapi.DNSLabelValid(ups[0]); err != nil {
						return nil, lynkapi.NewClientError("Invalid AppInstance Name:Port")
					}

					if port, err := strconv.Atoi(ups[1]); err != nil || port < 80 || port > 65505 {
						return nil, lynkapi.NewClientError("Invalid AppInstance Name:Port")
					}
				}

			case inapi.GatewayIngressType_Upstream:
				for _, tgt := range route.Targets {
					vs := strings.Split(tgt.Backend, ":")
					if len(vs) != 2 {
						return nil, lynkapi.NewClientError("Invalid IP:Port")
					}

					if ip := net.ParseIP(vs[0]); ip == nil || ip.To4() == nil {
						return nil, lynkapi.NewClientError("Invalid IP:Port")
					}
					if port, err := strconv.Atoi(vs[1]); err != nil || port < 80 || port > 65505 {
						return nil, lynkapi.NewClientError("Invalid IP:Port")
					}
				}

			case inapi.GatewayIngressType_Redirect:
				for _, tgt := range route.Targets {
					uri, err := url.ParseRequestURI(tgt.Backend)
					if err != nil {
						return nil, lynkapi.NewClientError("Invalid Redirect URL or Path: " + err.Error())
					}
					uri.Path = filepath.Clean(uri.Path)
					if uri.Path == "." {
						uri.Path = "/"
					}
					tgt.Backend = uri.String()
				}

			default:
				return nil, lynkapi.NewClientError("Invalid Route Type")
			}

			if prev := lynkapi.SlicesSearchFunc(item.Routes, func(a *inapi.GatewayIngress_HttpRoute) bool {
				return route.Path == a.Path
			}); prev == nil {
				item.Routes = append(item.Routes, route)
			} else {
				prev.Action = route.Action
				prev.Type = route.Type
				prev.Targets = route.Targets
			}
		}

		sort.Slice(item.Routes, func(i, j int) bool {
			return strings.Compare(item.Routes[i].Path, item.Routes[j].Path) > 0
		})
	}

	item.Meta.Updated = time.Now().Unix()
	wr := data.Zonelet.NewWriter(key, &item)
	if item.Meta.Version > 0 {
		wr.SetPrevVersion(item.Meta.Version)
	}
	if rs2 := wr.Exec(); !rs2.OK() {
		return nil, lynkapi.NewInternalServerError(rs2.ErrorMessage())
	}

	return &inapi.GatewayIngressSetResponse{Item: &item}, nil
}
