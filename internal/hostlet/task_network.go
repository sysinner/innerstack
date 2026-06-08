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
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"strings"

	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/hostlet/network"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/pkg/inetutil"
)

var (
	hostNetworkBridgeCurrent  = ""
	hostNetworkPeerIP         net.IP
	zoneNetworkMap            inapi.ZoneNetworkMap
	hostDNSUpdateSetup        uint64
	zoneNetworkMapUpdateSetup uint64
)

const (
	hostNetworkVxlanIdDefault = 10
)

func networkRefresh() error {

	if len(config.Config.Hostlet.DnsServers) > 0 &&
		hostDNSUpdateSetup < zoneNetworkMap.Revision {
		dnsConf := ""
		if len(zoneNetworkMap.VpcInstance) > 0 {
			for ipn, instanceId := range zoneNetworkMap.VpcInstance {
				ipb := inetutil.Uint32ToIp(ipn)
				dnsConf += fmt.Sprintf("[[records]]\nname = \"app-%s.%s\"\nips = [\"%s\"]\n",
					instanceId, zoneNetworkMap.VpcNetworkDomain, ipb.String())
			}
			if len(dnsConf) > 10 {
				if err := inutil.FsWrite(config.Prefix+"/etc/indns_conf.d/innerstack.toml",
					[]byte(dnsConf)); err != nil {
					return err
				}
			}
		}
		hostDNSUpdateSetup = zoneNetworkMap.Revision
	}

	if runtime.GOOS != "linux" {
		return nil
	}

	if config.Config.Hostlet.VpcBridgeIP == "" {
		return nil
	}

	if len(zoneNetworkMap.VpcBridgeHost) == 0 {
		return nil
	}

	if len(hostNetworkPeerIP) == 0 {

		prip := config.Config.Hostlet.LanAddr
		if n := strings.IndexByte(prip, ':'); n > 0 {
			prip = prip[:n]
		}

		pip, err := inetutil.ParsePrivateIP(prip)
		if err != nil {
			return err
		}

		hostNetworkPeerIP = pip
	}

	if hostNetworkBridgeCurrent != config.Config.Hostlet.VpcBridgeIP {

		brip, err := inetutil.ParsePrivateIP(config.Config.Hostlet.VpcBridgeIP)
		if err != nil {
			return err
		}

		//
		if err = network.LinkManager.VxlanSetup(brip, hostNetworkVxlanIdDefault,
			hostNetworkPeerIP); err != nil {
			return err
		}

		slog.Info(fmt.Sprintf("vpc bridge %s setup ok",
			config.Config.Hostlet.VpcBridgeIP))

		hostNetworkBridgeCurrent = config.Config.Hostlet.VpcBridgeIP
	}

	if zoneNetworkMapUpdateSetup < zoneNetworkMap.Revision {

		next := zoneNetworkMap.Revision

		for bridge, hostNet := range zoneNetworkMap.VpcBridgeHost {

			if hostNet.Peer == inetutil.BytesToUint32(hostNetworkPeerIP) {
				continue
			}

			//
			if err := network.LinkManager.VxlanForward(
				hostNetworkVxlanIdDefault, inetutil.Uint32ToIp(hostNet.Peer)); err != nil {
				slog.Warn(fmt.Sprintf("network vpc vxlan forward to %s error %s",
					inetutil.Uint32ToIp(hostNet.Peer).String(), err.Error()))
				return err
			}
			slog.Warn(fmt.Sprintf("network vpc vxlan forward to %s ok",
				inetutil.Uint32ToIp(hostNet.Peer).String()))

			//
			brIP := inetutil.Uint32ToIp(bridge)
			vpcIP := inetutil.Uint32ToIp(hostNet.Instance)
			if err := network.LinkManager.RouteReplace(vpcIP, brIP); err != nil {
				slog.Warn(fmt.Sprintf("network vpc route (%s via %s) replace error %s",
					vpcIP.String(), brIP.String(), err.Error()))
				return err
			}

			slog.Warn(fmt.Sprintf("network vpc route (%s via %s) replace ok",
				vpcIP.String(), brIP.String()))
		}

		zoneNetworkMapUpdateSetup = next
	}

	return nil
}
