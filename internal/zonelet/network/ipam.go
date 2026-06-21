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

package network

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/pkg/inetutil"
)

type NetworkManager struct {
	mu sync.RWMutex

	Map *inapi.ZoneNetworkMap

	Version uint64

	Bridge   uint32
	Instance uint32

	hostPeerIpv4 map[string]string

	Hosts map[string]*inapi.ZoneNetworkMap_Host

	changed bool

	zoneName string
	ready    bool
}

func NewNetworkManager() *NetworkManager {
	return &NetworkManager{
		Hosts: map[string]*inapi.ZoneNetworkMap_Host{},
	}
}

func (nm *NetworkManager) Clone() *inapi.ZoneNetworkMap {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return proto.Clone(nm.Map).(*inapi.ZoneNetworkMap)
}

func (nm *NetworkManager) Iter(fn func(hostId string, hostNet *inapi.ZoneNetworkMap_Host)) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	if len(nm.Hosts) > 0 {
		for k, v := range nm.Hosts {
			fn(k, v)
		}
	}
}

func (nm *NetworkManager) IsReady() bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.ready
}

func (nm *NetworkManager) IsChanged() bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.changed
}

// ZoneSetup initializes the zone-level VPC CIDRs.
func (nm *NetworkManager) ZoneSetup(zoneName, bridgeCIDR, instanceCIDR, domain string) error {

	if zoneName == "" || bridgeCIDR == "" || instanceCIDR == "" {
		return nil
	}

	nm.mu.Lock()
	defer nm.mu.Unlock()

	if zoneName != nm.zoneName {
		if nm.zoneName != "" {
			return fmt.Errorf("invalid zone name")
		}
		nm.zoneName = zoneName
		nm.changed = true
	}

	if nm.Map == nil {
		nm.Map = &inapi.ZoneNetworkMap{}
	}

	if bridgeCIDR != "" && bridgeCIDR != nm.Map.VpcBridgeCidr {
		ipv4Addr, _, err := net.ParseCIDR(bridgeCIDR)
		if err != nil {
			return fmt.Errorf("invalid bridge CIDR %q: %w", bridgeCIDR, err)
		}
		if _, err := inetutil.ParsePrivateIP(ipv4Addr.String()); err != nil {
			return fmt.Errorf("bridge CIDR must be a private range: %w", err)
		}
		nm.Map.VpcBridgeCidr = bridgeCIDR
		nm.Bridge = inetutil.BytesToUint32(ipv4Addr.To4())
		nm.changed = true
		slog.Info("zone vpc bridge CIDR set", "cidr", bridgeCIDR, "bridge", nm.Bridge)
	}

	if instanceCIDR != "" && instanceCIDR != nm.Map.VpcInstanceCidr {
		ipv4Addr, _, err := net.ParseCIDR(instanceCIDR)
		if err != nil {
			return fmt.Errorf("invalid instance CIDR %q: %w", instanceCIDR, err)
		}
		if _, err := inetutil.ParsePrivateIP(ipv4Addr.String()); err != nil {
			return fmt.Errorf("instance CIDR must be a private range: %w", err)
		}
		nm.Map.VpcInstanceCidr = instanceCIDR
		nm.Instance = inetutil.BytesToUint32(ipv4Addr.To4())
		nm.changed = true
		slog.Info("zone vpc instance CIDR set", "cidr", instanceCIDR, "instance", nm.Instance)
	}

	if domain != "" && domain != nm.Map.VpcNetworkDomain {
		nm.Map.VpcNetworkDomain = domain
		nm.changed = true
		slog.Info("zone vpc domain set", "domain", domain)
	}

	if nm.changed {
		if err := nm.flush(); err != nil {
			return err
		}
		nm.changed = false
	}

	nm.ready = true

	return nil
}

func (nm *NetworkManager) init() {
	if nm.Hosts == nil {
		nm.Hosts = map[string]*inapi.ZoneNetworkMap_Host{}
	}

	if nm.Map.VpcBridgeHost == nil {
		nm.Map.VpcBridgeHost = map[uint32]*inapi.ZoneNetworkMap_Host{}
	}

	if nm.Map.VpcInstance == nil {
		nm.Map.VpcInstance = map[uint32]string{}
	}
}

func (nm *NetworkManager) Restore(zoneName string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if zoneName == "" {
		return fmt.Errorf("[NetworkManager.Restore] zone init not ready")
	} else {
		nm.zoneName = zoneName
	}

	key := inapi.NsZoneletNetworkIPAM(nm.zoneName)
	rs := data.Zonelet.NewReader(key).Exec()
	if rs.NotFound() {
		slog.Info("zone network IPAM state not found, starting fresh", "zone", nm.zoneName)
		return nil
	}
	if !rs.OK() {
		return fmt.Errorf("[NetworkManager.Restore] read: %s", rs.ErrorMessage())
	}
	if rs.Item().Meta.Version <= nm.Version {
		return nil
	}

	var state inapi.ZoneNetworkMap
	if err := json.Unmarshal(rs.Item().Value, &state); err != nil {
		return fmt.Errorf("[NetworkManager.Restore] unmarshal: %w", err)
	}

	nm.Map = &state
	nm.Hosts = map[string]*inapi.ZoneNetworkMap_Host{}

	{
		ipv4Addr, _, err := net.ParseCIDR(nm.Map.VpcBridgeCidr)
		if err != nil {
			return err
		}
		nm.Bridge = inetutil.BytesToUint32(ipv4Addr.To4()) & 0xFFFFFF00
	}

	{
		ipv4Addr, _, err := net.ParseCIDR(nm.Map.VpcInstanceCidr)
		if err != nil {
			return err
		}
		nm.Instance = inetutil.BytesToUint32(ipv4Addr.To4()) & 0xFFFF0000
	}

	if len(state.VpcBridgeHost) > 0 {
		dels := []uint32{}
		for k, host := range state.VpcBridgeHost {
			if k&0xFFFFFF00 != nm.Bridge {
				dels = append(dels, k)
				continue
			}
			if host != nil && host.Bridge == k {
				nm.Hosts[host.Id] = host
			}
		}
		for _, k := range dels {
			slog.Warn("zone network IPAM remove bridge " + inetutil.Uint32ToIpv4(k))
			delete(state.VpcBridgeHost, k)
		}
	}
	if len(state.VpcInstance) > 0 {
		dels := []uint32{}
		for k, _ := range state.VpcInstance {
			if k&0xFFFF0000 != nm.Instance {
				dels = append(dels, k)
				continue
			}
		}
		for _, k := range dels {
			slog.Warn("zone network IPAM remove instance-ip " + inetutil.Uint32ToIpv4(k))
			delete(state.VpcInstance, k)
		}
	}

	nm.Version = rs.Item().Meta.Version

	nm.ready = true

	slog.Info("zone network IPAM state restored",
		"zone", zoneName,
		"hosts", len(nm.Hosts),
		"bridge_cidr", state.VpcBridgeCidr,
		"bridge", nm.Bridge,
		"instance_cidr", state.VpcInstanceCidr,
		"instance", nm.Instance)

	return nil
}

func (nm *NetworkManager) HostPeerIp(hostId string) (string, bool) {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	if len(nm.hostPeerIpv4) > 0 {
		if ip, ok := nm.hostPeerIpv4[hostId]; ok {
			return ip, true
		}
	}
	return "", false
}

func (nm *NetworkManager) HostNetwork(hostId string) *inapi.ZoneNetworkMap_Host {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if len(nm.Hosts) > 0 {
		if host, ok := nm.Hosts[hostId]; ok {
			return host
		}
	}
	return nil
}

func (nm *NetworkManager) RefreshHostNetwork(zone string, host *inapi.Host) (bool, error) {

	if host.PeerAddr == "" || host.Deploy == nil {
		return false, nil
	}

	peerIp, err := inetutil.ParsePrivateAddress(host.PeerAddr)
	if err != nil {
		return false, err
	}
	peer := inetutil.BytesToUint32(peerIp)

	nm.mu.Lock()
	defer nm.mu.Unlock()

	if nm.hostPeerIpv4 == nil {
		nm.hostPeerIpv4 = map[string]string{}
	}
	nm.hostPeerIpv4[host.Id] = inetutil.IP4ToString(peerIp)

	if !nm.ready {
		return false, nil
	}

	nm.init()

	hostNet := nm.Hosts[host.Id]

	if hostNet != nil &&
		inetutil.Uint32ToIpv4(hostNet.Bridge) == host.Deploy.VpcBridgeIp &&
		hostNet.Peer == peer {
		return false, nil
	}

	baseBridge := nm.Bridge & 0xFFFFFF00

	if hostNet != nil &&
		inetutil.Uint32ToIpv4(hostNet.Bridge) != host.Deploy.VpcBridgeIp {
		hostNet = nil
		for n := uint32(inapi.VpcAllocMin); n <= uint32(inapi.VpcAllocMax); n++ {
			if v, ok := nm.Map.VpcBridgeHost[baseBridge+n]; ok && v.Id == host.Id {
				hostNet = v
				break
			}
		}
	}

	if hostNet == nil {

		for n := uint32(inapi.VpcAllocMin); n <= uint32(inapi.VpcAllocMax); n++ {

			bn := baseBridge + n

			if _, ok := nm.Map.VpcBridgeHost[bn]; ok {
				continue
			}

			instanceNet := (nm.Instance & 0xFFFF0000) + (n << 8)

			hostNet = &inapi.ZoneNetworkMap_Host{
				Id:       host.Id,
				Bridge:   bn,
				Instance: instanceNet,
			}

			nm.Map.VpcBridgeHost[bn] = hostNet
			nm.Hosts[host.Id] = hostNet

			break
		}

		if hostNet == nil {
			return false, nil
		}
	}

	host.Deploy.VpcBridgeIp = inetutil.Uint32ToIpv4(hostNet.Bridge)
	host.Deploy.VpcInstanceCidr = inetutil.Uint32ToIpv4(hostNet.Instance) + "/24"

	if inetutil.BytesToUint32(peerIp) != hostNet.Peer {
		hostNet.Peer = inetutil.BytesToUint32(peerIp)
	}

	if err := nm.flush(); err != nil {
		return false, err
	}

	return true, nil
}

func (nm *NetworkManager) VpcInstance(ipv4 string) string {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if nm.Map != nil && len(nm.Map.VpcInstance) > 0 {
		if id, ok := nm.Map.VpcInstance[inetutil.Ipv4ToUint32(ipv4)]; ok {
			return id
		}
	}
	return ""
}

func (nm *NetworkManager) AllocHostSubNetwork(zone, hostId, instanceName string, repId uint32) string {

	insRepId := fmt.Sprintf("%s_%d", instanceName, repId)

	nm.mu.Lock()
	defer nm.mu.Unlock()

	if !nm.ready {
		return ""
	}

	nm.init()

	host, ok := nm.Hosts[hostId]
	if !ok {
		return ""
	}

	baseSubnet := host.Instance & 0xFFFFFF00
	for n := uint32(inapi.VpcAllocMin); n <= uint32(inapi.VpcAllocMax); n++ {
		ipn := baseSubnet + n
		if id, ok := nm.Map.VpcInstance[ipn]; ok && insRepId == id {
			return inetutil.Uint32ToIpv4(ipn)
		}
	}

	for n := uint32(inapi.VpcAllocMin); n <= uint32(inapi.VpcAllocMax); n++ {
		ipn := baseSubnet + n
		if _, ok := nm.Map.VpcInstance[ipn]; ok {
			continue
		}
		nm.Map.VpcInstance[ipn] = insRepId

		if err := nm.flush(); err != nil {
			return ""
		}
		return inetutil.Uint32ToIpv4(ipn)
	}

	return ""
}

func (nm *NetworkManager) flush() error {

	if nm.Map == nil {
		nm.Map = &inapi.ZoneNetworkMap{}
	}

	nm.Map.Updated = time.Now().Unix()
	nm.Map.Revision += 1

	key := inapi.NsZoneletNetworkIPAM(nm.zoneName)
	if rs := data.Zonelet.NewWriter(key, nm.Map).Exec(); !rs.OK() {
		return fmt.Errorf("[NetworkManager.flush] write: %s", rs.ErrorMessage())
	} else {
		nm.Version = rs.Item().Meta.Version
	}

	return nil
}
