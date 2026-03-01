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

package inapi

import (
	"fmt"

	hauth2 "github.com/hooto/hauth/go/v2"
)

const (
	HostSetupStart   = "start"
	HostSetupStop    = "stop"
	HostSetupDestroy = "destroy"
)

var (
	AuthPermSysAll = hauth2.NewScopeFilter("sys", "*")
)

func NsGlobalGatewayServiceDomain(name string) []byte {
	return []byte(fmt.Sprintf("v2/service/gateway/domain/%s", name))
}

func NsZoneletInfo(zone string) []byte {
	return []byte(fmt.Sprintf("v2/zone/%s/info", zone))
}

func NsZoneletLeader(zone string) []byte {
	return []byte(fmt.Sprintf("v2/zone/%s/leader", zone))
}

func NsZoneSysAccessKey(zone, kid string) []byte {
	return []byte(fmt.Sprintf("v2/zone/%s/sys-ak/%s", zone, kid))
}

func NsHostInfo(zone, host string) []byte {
	return []byte(fmt.Sprintf("v2/host/%s/info/%s", zone, host))
}

func NsHostStatus(zone, host string) []byte {
	return []byte(fmt.Sprintf("v2/host/%s/status/%s", zone, host))
}

func NsAppInstance(zone, id string) []byte {
	return []byte(fmt.Sprintf("v2/app/instance/%s/%s", zone, id))
}
