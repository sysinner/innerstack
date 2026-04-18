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

package zonelet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/auth"
	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/config"
	"github.com/sysinner/incore/v2/internal/data"
	"github.com/sysinner/incore/v2/internal/status"
	"github.com/sysinner/incore/v2/pkg/inauth"
	"github.com/sysinner/incore/v2/pkg/inetutil"
)

// uploadMutex provides per-package mutex for concurrent upload protection
var uploadMutex sync.Map // map[string]*sync.Mutex

// calcTotalChunks calculates total chunks from file size and chunk size
func calcTotalChunks(totalSize, chunkSize int64) int64 {
	return (totalSize + chunkSize - 1) / chunkSize
}

type zoneServer struct {
	inapi.UnimplementedZoneServiceServer
}

func NewServer() inapi.ZoneServiceServer {
	return &zoneServer{}
}

func (s *zoneServer) ZoneInit(
	ctx context.Context, req *inapi.ZoneInitRequest,
) (*inapi.ZoneInitResponse, error) {

	req.Name = strings.ToLower(req.Name)

	if err := inapi.NameValid(req.Name); err != nil {
		return nil, err
	}

	if config.Config.Zonelet.ZoneName != "" ||
		len(config.Config.Server.ZoneHosts) > 0 {
		return nil, errors.New("System already initialized")
	}

	config.Config.Zonelet.ZoneName = req.Name
	config.Config.Server.ZoneHosts = []string{
		fmt.Sprintf("%s:%d", config.Config.Hostlet.LanAddr, config.Config.Server.PeerPort),
	}

	zone := &inapi.Zone{
		Name:  req.Name,
		Hosts: config.Config.Server.ZoneHosts,
	}

	if rs := data.Zonelet.NewWriter(
		inapi.NsZoneletInfo(zone.Name), zone).
		SetCreateOnly(true).Exec(); !rs.OK() {
		return nil, rs.Error()
	}

	slog.Warn("zonelet init-zone",
		"zone_name", zone.Name,
		"host_id", config.Config.Hostlet.HostId,
	)

	if err := config.Flush(); err != nil {
		return nil, err
	}

	{
		host := &inapi.Host{
			Id:        config.Config.Hostlet.HostId,
			PeerAddr:  fmt.Sprintf("%s:%d", config.Config.Hostlet.LanAddr, config.Config.Server.PeerPort),
			AccessKey: config.Config.Hostlet.AccessKey,
		}

		if rs := data.Zonelet.NewWriter(
			inapi.NsHostInfo(config.Config.Zonelet.ZoneName, host.Id), host).
			SetCreateOnly(true).Exec(); !rs.OK() {
			return nil, rs.Error()
		}

		slog.Warn("zonelet init-host",
			"host_id", config.Config.Hostlet.HostId,
		)
	}

	status.Zonelet_ForceRefresh.Store(true)

	return &inapi.ZoneInitResponse{}, nil
}

func (s *zoneServer) ZoneInfo(
	ctx context.Context, req *inapi.ZoneInfoRequest,
) (*inapi.ZoneInfoResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_Zone_Read) {
		return nil, errors.New("auth fail: missing zone:ro scope")
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	var zone inapi.Zone

	if rs := data.Zonelet.NewReader(
		inapi.NsZoneletInfo(config.Config.Zonelet.ZoneName)).
		Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("System uninitialized")
		}
		return nil, rs.Error()
	} else if err := rs.Item().JsonDecode(&zone); err != nil {
		return nil, err
	}

	info := &inapi.ZoneInfoResponse{
		Zone: &zone,
	}

	if zoneNetMgr.IsReady() {
		info.NetworkMap = zoneNetMgr.Map
	}

	return info, nil
}

func (s *zoneServer) ZoneSet(
	ctx context.Context, req *inapi.ZoneSetRequest,
) (*inapi.ZoneSetResponse, error) {

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	if config.Config.Zonelet.ZoneName == "" {
		return nil, errors.New("zone not initialized")
	}

	// All three VPC fields must be provided together; partial updates not allowed
	if req.VpcBridgeCidr == "" || req.VpcInstanceCidr == "" || req.VpcNetworkDomain == "" {
		return nil, errors.New("vpc_bridge_cidr, vpc_instance_cidr, and vpc_network_domain are all required")
	}

	// Validate CIDR formats and ensure private network addresses (RFC 1918)
	bridgeNet, err := validatePrivateCIDR(req.VpcBridgeCidr, "vpc_bridge_cidr", 24)
	if err != nil {
		return nil, err
	}
	instanceNet, err := validatePrivateCIDR(req.VpcInstanceCidr, "vpc_instance_cidr", 16)
	if err != nil {
		return nil, err
	}

	// Ensure bridge and instance CIDRs do not overlap
	if cidrsOverlap(bridgeNet, instanceNet) {
		return nil, errors.New("vpc_bridge_cidr and vpc_instance_cidr must not overlap")
	}

	// Load current zone
	var zone inapi.Zone
	if rs := data.Zonelet.NewReader(
		inapi.NsZoneletInfo(config.Config.Zonelet.ZoneName)).Exec(); !rs.OK() {
		if rs.NotFound() {
			return nil, errors.New("zone not found")
		}
		return nil, rs.Error()
	} else if err := rs.Item().JsonDecode(&zone); err != nil {
		return nil, err
	}

	// Update zone VPC fields
	zone.VpcBridgeCidr = req.VpcBridgeCidr
	zone.VpcInstanceCidr = req.VpcInstanceCidr
	zone.VpcNetworkDomain = req.VpcNetworkDomain

	// Persist updated zone
	if rs := data.Zonelet.NewWriter(
		inapi.NsZoneletInfo(zone.Name), &zone).Exec(); !rs.OK() {
		return nil, rs.Error()
	}

	// Apply zone VPC networking
	if err := zoneNetMgr.ZoneSetup(
		zone.Name,
		zone.VpcBridgeCidr,
		zone.VpcInstanceCidr,
		zone.VpcNetworkDomain); err != nil {
		slog.Error("zone VPC setup failed",
			"zone", zone.Name,
			"err", err.Error())
		return nil, fmt.Errorf("zone VPC setup failed: %w", err)
	}

	slog.Warn("zonelet zone-set",
		"zone_name", zone.Name,
		"vpc_bridge_cidr", zone.VpcBridgeCidr,
		"vpc_instance_cidr", zone.VpcInstanceCidr,
		"vpc_network_domain", zone.VpcNetworkDomain,
	)

	status.Zonelet_ForceRefresh.Store(true)

	return &inapi.ZoneSetResponse{
		Zone: &zone,
	}, nil
}

func (s *zoneServer) HostJoin(
	ctx context.Context, req *inapi.HostJoinRequest,
) (*inapi.HostJoinResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_Host_Write) {
		return nil, errors.New("auth fail: missing host:rw scope")
	}

	if err := inapi.Ip4AddrValid(req.Addr); err != nil {
		return nil, err
	}

	ak, err := inauth.ParseAccessKey(req.AccessKey)
	if err != nil {
		return nil, fmt.Errorf("invalid access_key: %w", err)
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	conn, err := client.Connect(req.Addr, ak, false)
	if err != nil {
		return nil, err
	}

	hc := inapi.NewHostInternalServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req2 := &inapi.HostInitRequest{
		ZoneName:  config.Config.Zonelet.ZoneName,
		ZoneHosts: config.Config.Server.ZoneHosts,
	}

	resp, err := hc.HostInit(ctx, req2)
	if err != nil {
		return nil, fmt.Errorf("failed to join host: %s", err.Error())
	}

	host := &inapi.Host{
		Id:        resp.HostId,
		PeerAddr:  req.Addr,
		AccessKey: ak.Export(),
	}

	if rs := data.Zonelet.NewWriter(
		inapi.NsHostInfo(config.Config.Zonelet.ZoneName, resp.HostId), host).
		SetCreateOnly(true).Exec(); !rs.OK() {
		return nil, rs.Error()
	}

	if !inapi.ObjectIdValid.MatchString(resp.HostId) {
		return nil, errors.New("invalid host_id")
	}

	ak.Scopes = []string{
		inapi.AuthScope_Host_Write + ":" + resp.HostId,
		inapi.AuthScope_Package_Read,
	}
	auth.AuthMgr.SaveAccessKey(ak)

	slog.Warn("zonelet init-host",
		"host_id", resp.HostId,
	)

	status.Zonelet_ForceRefresh.Store(true)

	return &inapi.HostJoinResponse{
		Status: resp.Status,
	}, nil
}

func (s *zoneServer) HostList(
	ctx context.Context, req *inapi.HostListRequest,
) (*inapi.HostListResponse, error) {

	if !inauth.AppContext(ctx).Allow(inapi.AuthScope_Host_Read) {
		return nil, errors.New("auth fail: missing host:ro scope")
	}

	if !status.IsZoneletLeader() {
		return nil, errors.New("zonelet leader")
	}

	resp := &inapi.HostListResponse{}

	offset := inapi.NsHostInfo(config.Config.Zonelet.ZoneName, "")

	rs := data.Zonelet.NewRanger(offset, append(offset, 0xff)).Exec()
	for _, item := range rs.Items {
		var host inapi.Host
		if err := item.JsonDecode(&host); err == nil {
			host.AccessKey = ""
			if val, ok := status.Zonelet_HostStatusSet.Load(host.Id); ok {
				host.Status = val.(*inapi.HostStatus)
			}
			if val := gHostOperateSet.Load(host.Id); val != nil {
				host.Operate = val.Value.(*inapi.HostOperate)
			}
			resp.Hosts = append(resp.Hosts, &host)
		}
	}

	return resp, nil
}

// cidrsOverlap checks whether two IP networks overlap.
func cidrsOverlap(a, b *net.IPNet) bool {
	return a.Contains(b.IP) || b.Contains(a.IP)
}

// validatePrivateCIDR parses a CIDR string and validates that its network
// address belongs to a private range (RFC 1918) with the required prefix length.
// Returns the parsed *net.IPNet on success.
func validatePrivateCIDR(cidr, fieldName string, requiredPrefixLen int) (*net.IPNet, error) {

	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", fieldName, err)
	}

	if ip.To4() == nil {
		return nil, fmt.Errorf("%s must be an IPv4 CIDR", fieldName)
	}

	if _, err := inetutil.ParsePrivateIP(ip.String()); err != nil {
		return nil, fmt.Errorf("%s must be a private network address (RFC 1918)", fieldName)
	}

	prefixLen, _ := ipNet.Mask.Size()
	if prefixLen != requiredPrefixLen {
		return nil, fmt.Errorf("%s must be a /%d network", fieldName, requiredPrefixLen)
	}

	return ipNet, nil
}
