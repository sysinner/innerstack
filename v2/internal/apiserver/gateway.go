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

package apiserver

import (
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	hauth2 "github.com/hooto/hauth/go/v2"
	"github.com/lessos/lessgo/crypto/idhash"
	"github.com/lynkdb/lynkapi/go/lynkapi"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"

	"github.com/sysinner/incore/data"
	"github.com/sysinner/incore/status"
	inapi2 "github.com/sysinner/incore/v2/inapi"
)

var (
	gatewayDomainPodRX = regexp.MustCompile("^[0-9a-f]{12,16}$")
)

func (it *ApiService) GatewayDomainList(
	ctx lynkapi.Context,
	req *inapi2.GatewayService_DomainListRequest,
) (*inapi2.GatewayService_DomainListResponse, error) {

	rsp := &inapi2.GatewayService_DomainListResponse{}

	if !status.IsZoneMaster() {
		return nil, lynkapi.NewClientError("Invalid Zone MainNode Address")
	}

	// load domain list
	var (
		session = hauth2.AuthContextSession(ctx)

		offset = inapi2.NsGlobalGatewayServiceDomain("")
		rs     = data.DataGlobal.NewRanger(offset, offset).
			SetLimit(1000).Exec() // TODO
	)

	for _, v := range rs.Items {
		var item inapi2.GatewayService_Domain
		if err := v.JsonDecode(&item); err != nil || item.Meta == nil {
			continue
		}
		if !session.Allow(item.Meta.User, inapi2.AuthPermSysAll) {
			continue
		}
		rsp.Domains = append(rsp.Domains, &item)
	}

	sort.Slice(rsp.Domains, func(i, j int) bool {
		return rsp.Domains[i].Meta.Updated > rsp.Domains[j].Meta.Updated
	})

	return rsp, nil
}

func (it *ApiService) GatewayDomain(
	ctx lynkapi.Context,
	req *inapi2.GatewayService_DomainRequest,
) (*inapi2.GatewayService_Domain, error) {

	if !status.IsZoneMaster() {
		return nil, lynkapi.NewClientError("Invalid Zone MainNode Address")
	}

	var (
		session = hauth2.AuthContextSession(ctx)

		rsp inapi2.GatewayService_Domain
		key = inapi2.NsGlobalGatewayServiceDomain(req.Name)
		rs  = data.DataGlobal.NewReader(key).Exec()
	)

	if rs.NotFound() || len(rs.Items) == 0 {
		return nil, lynkapi.NewNotFoundError("Domain Not Found")
	} else if !rs.OK() {
		return nil, lynkapi.NewInternalServerError(rs.ErrorMessage())
	}

	if err := rs.Items[0].JsonDecode(&rsp); err != nil {
		return nil, lynkapi.NewInternalServerError(err.Error())
	}

	if !session.Allow(rsp.Meta.User, inapi2.AuthPermSysAll) {
		return nil, lynkapi.NewUnAuthError("Access Denied")
	}

	return &rsp, nil
}

func (it *ApiService) GatewayDomainSet(
	ctx lynkapi.Context,
	req *inapi2.GatewayService_Domain,
) (*inapi2.GatewayService_Domain, error) {

	if !status.IsZoneMaster() {
		return nil, lynkapi.NewClientError("Invalid Zone MainNode Address")
	}

	if req.Meta == nil {
		return nil, lynkapi.NewNotFoundError("Domain Not Found")
	}

	{
		req.Meta.Name = strings.TrimSpace(strings.ToLower(req.Meta.Name))

		if _, ok := publicsuffix.PublicSuffix(req.Meta.Name); !ok {
			return nil, lynkapi.NewClientError("Invalid Domain Name (TLD)")
		}

		if _, err := idna.ToASCII(req.Meta.Name); err != nil {
			return nil, lynkapi.NewClientError("Invalid Domain Name : " + err.Error())
		}

		// Per RFC 1035:
		// - Total length must not exceed 253 characters
		// - Each label (dot-separated segment) must not exceed 63 characters
		if len(req.Meta.Name) > 253 {
			return nil, lynkapi.NewClientError("Invalid Domain Name : Total length must not exceed 253 characters")
		}
		labels := strings.Split(req.Meta.Name, ".")
		for _, label := range labels {
			if len(label) > 63 {
				return nil, lynkapi.NewClientError("Invalid Domain Name : Each label (dot-separated segment) must not exceed 63 characters")
			}
		}
	}

	var (
		session = hauth2.AuthContextSession(ctx)

		rsp inapi2.GatewayService_Domain
		key = inapi2.NsGlobalGatewayServiceDomain(req.Meta.Name)
		rs  = data.DataGlobal.NewReader(key).Exec()
	)

	if rs.NotFound() || len(rs.Items) == 0 {

		rsp.Meta = &inapi2.Common_Meta{
			Id:      idhash.HashToHexString([]byte(req.Meta.Name), 16),
			Name:    req.Meta.Name,
			Created: time.Now().UnixMilli(),
		}

		// return nil, lynkapi.NewNotFoundError("Domain Not Found")
	} else if !rs.OK() {
		return nil, lynkapi.NewInternalServerError(rs.ErrorMessage())
	} else if err := rs.Items[0].JsonDecode(&rsp); err != nil {
		return nil, lynkapi.NewInternalServerError(err.Error())
	}

	{
		if rsp.Meta.User != "" &&
			!session.Allow(rsp.Meta.User, inapi2.AuthPermSysAll) {
			return nil, lynkapi.NewUnAuthError("Access Denied")
		}

		if req.Meta.User != "" &&
			!session.Allow(req.Meta.User, inapi2.AuthPermSysAll) {
			return nil, lynkapi.NewUnAuthError("Access Denied to bind owner to " + req.Meta.User)
		}

		if req.Meta.User != "" {
			rsp.Meta.User = req.Meta.User
		} else if rsp.Meta.User == "" {
			rsp.Meta.User = session.Sub
		}
	}

	rsp.ZoneId = req.ZoneId
	rsp.Action = req.Action
	rsp.Description = req.Description
	rsp.Options = req.Options

	rsp.Meta.Updated = time.Now().UnixMilli()

	if rs := data.DataGlobal.NewWriter(key, rsp).Exec(); !rs.OK() {
		return nil, lynkapi.NewInternalServerError(rs.ErrorMessage())
	}

	return &rsp, nil
}

func (it *ApiService) GatewayDomainRouteSet(
	ctx lynkapi.Context,
	req *inapi2.GatewayService_Domain,
) (*inapi2.GatewayService_Domain, error) {

	if !status.IsZoneMaster() {
		return nil, lynkapi.NewClientError("Invalid Zone MainNode Address")
	}

	if req.Meta == nil {
		return nil, lynkapi.NewNotFoundError("Domain Not Found")
	}

	if len(req.Routes) == 0 {
		return nil, lynkapi.NewClientError("No Route(s) request")
	}

	var (
		session = hauth2.AuthContextSession(ctx)

		rsp inapi2.GatewayService_Domain
		key = inapi2.NsGlobalGatewayServiceDomain(req.Meta.Name)
		rs  = data.DataGlobal.NewReader(key).Exec()
	)

	if rs.NotFound() || len(rs.Items) == 0 {
		return nil, lynkapi.NewNotFoundError("Domain Not Found")
	} else if !rs.OK() {
		return nil, lynkapi.NewInternalServerError(rs.ErrorMessage())
	} else if err := rs.Items[0].JsonDecode(&rsp); err != nil {
		return nil, lynkapi.NewInternalServerError(err.Error())
	}

	if !session.Allow(rsp.Meta.User, inapi2.AuthPermSysAll) {
		return nil, lynkapi.NewUnAuthError("Access Denied")
	}

	for _, route := range req.Routes {

		route.Path = filepath.Clean(route.Path)

		switch route.Type {
		case "pod":
			for _, v := range route.Targets {
				ups := strings.Split(v, ":")
				if len(ups) != 2 {
					return nil, lynkapi.NewClientError("Invalid Pod ID:Port")
				}
				if !gatewayDomainPodRX.MatchString(ups[0]) {
					return nil, lynkapi.NewClientError("Invalid Pod ID:Port")
				}

				if port, err := strconv.Atoi(ups[1]); err != nil || port < 80 || port > 65505 {
					return nil, lynkapi.NewClientError("Invalid Pod ID:Port")
				}
			}

		case "upstream":
			for _, v := range route.Targets {

				vs := strings.Split(v, ":")
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

		case "redirect":
			for i, v := range route.Targets {
				uri, err := url.ParseRequestURI(v)
				if err != nil {
					return nil, lynkapi.NewClientError("Invalid Redirect URL or Path: " + err.Error())
				}
				uri.Path = filepath.Clean(uri.Path)
				if uri.Path == "." {
					uri.Path = "/"
				}
				route.Targets[i] = uri.String()
			}

		default:
			return nil, lynkapi.NewClientError("Invalid Route Type")
		}

		if prev := lynkapi.SlicesSearchFunc(rsp.Routes, func(a *inapi2.GatewayService_Domain_Route) bool {
			return route.Path == a.Path
		}); prev == nil {
			rsp.Routes = append(rsp.Routes, route)
		} else {
			prev.Action = route.Action
			prev.Type = route.Type
			prev.Targets = route.Targets
		}
	}

	sort.Slice(rsp.Routes, func(i, j int) bool {
		return strings.Compare(rsp.Routes[i].Path, rsp.Routes[j].Path) > 0
	})

	rsp.Meta.Updated = time.Now().UnixMilli()

	if rs := data.DataGlobal.NewWriter(key, rsp).Exec(); !rs.OK() {
		return nil, lynkapi.NewInternalServerError(rs.ErrorMessage())
	}

	return &rsp, nil
}
