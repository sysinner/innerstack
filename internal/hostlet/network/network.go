// Copyright 2015 Eryx <evorui at gmail dot com>, All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in comdevLinkance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by apdevLinkcable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or imdevLinked.
// See the License for the specific language governing permissions and
// limitations under the License.

package network

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/vishvananda/netlink"
)

type Addr struct {
	IPNet *net.IPNet
}

type LinkInfo struct {
	Name              string
	Index             int
	Type              string
	Addrs             []*Addr
	VxlanId           int
	VxlanPort         int
	VxlanVtepDevIndex int
	link              netlink.Link
}

func (it *LinkInfo) IpString() string {
	if len(it.Addrs) > 0 {
		return it.Addrs[0].IPNet.IP.String()
	}
	return ""
}

var (
	LinkManager = newLinkManager()
)

type linkManager struct {
	mu     sync.RWMutex
	links  []*LinkInfo
	routes map[string]string
	fdb    map[string]string
}

func newLinkManager() *linkManager {
	return &linkManager{
		// routes: map[string]string{},
	}
}

func (it *linkManager) init() {
	if it.links == nil {
		ls, err := netlink.LinkList()
		if err == nil {
			for _, v := range ls {
				it.links = append(it.links, newLinkInfo(v))
			}
		}
	}
}

func newLinkInfo(v netlink.Link) *LinkInfo {

	attrs := v.Attrs()

	li := &LinkInfo{
		Name:  attrs.Name,
		Index: attrs.Index,
		Type:  v.Type(),
		link:  v,
	}

	switch vl := v.(type) {

	case *netlink.Vxlan:
		li.VxlanId = vl.VxlanId
		li.VxlanPort = vl.Port
		li.VxlanVtepDevIndex = vl.VtepDevIndex
	}

	//
	addrs, _ := netlink.AddrList(v, syscall.AF_INET)
	for _, v2 := range addrs {
		li.Addrs = append(li.Addrs, &Addr{
			IPNet: v2.IPNet,
		})
	}

	return li
}

func (it *linkManager) LinkList() []*LinkInfo {
	it.init()
	return it.links
}

func (it *linkManager) GetLinkByIp(ip net.IP) *LinkInfo {

	it.init()

	for _, v := range it.links {
		for _, addr := range v.Addrs {
			if !ip.Equal(addr.IPNet.IP) {
				continue
			}
			return v
		}
	}

	return nil
}

func (it *linkManager) LinkDel(li *LinkInfo) error {
	return netlink.LinkDel(li.link)
}

func (it *linkManager) VxlanSetup(ip net.IP, vid int, devIP net.IP) error {

	addr, err := netlink.ParseAddr(ip.String() + "/24")
	if err != nil {
		return err
	}

	devLink := it.GetLinkByIp(devIP)
	if devLink == nil {
		return errors.New("peer network not ready")
	}

	var (
		name = fmt.Sprintf("invpc2_vxlan.%d", vid)
	)
	vxlanLink, err := netlink.LinkByName(name)

	if err == nil {
		devLink := newLinkInfo(vxlanLink)

		if devLink.Type != "vxlan" ||
			devLink.VxlanId != vid ||
			devLink.VxlanVtepDevIndex != devLink.Index {
			if err = netlink.LinkDel(vxlanLink); err != nil {
				return err
			}
		} else {
			for _, pAddr := range devLink.Addrs {
				if addr.IPNet.IP.Equal(pAddr.IPNet.IP) {
					return nil
				}
			}
		}

		vxlanLink = nil
	}

	if vxlanLink == nil {

		la := netlink.NewLinkAttrs()
		la.Name = name
		la.MTU = 1500
		li := &netlink.Vxlan{
			LinkAttrs:    la,
			VxlanId:      vid,
			VtepDevIndex: devLink.Index,
			Port:         0,
		}

		err = netlink.LinkAdd(li)
		if err != nil {
			slog.Warn(fmt.Sprintf("ip link add: vxlan %s, id %d, err %s",
				la.Name, li.VxlanId, err.Error()))
			return err
		}

		vxlanLink = li
	}

	slog.Warn(fmt.Sprintf("ip link setup: vxlan %s, id %d, ok",
		name, vid))

	if err = netlink.AddrReplace(vxlanLink, addr); err != nil {
		return err
	}

	if err = netlink.LinkSetUp(vxlanLink); err != nil {
		return err
	}

	return nil
}

const (
	vxlanForwardMacAddrDefault = "00:00:00:00:00:00"
)

func (it *linkManager) VxlanForwardList(vid int) (map[string]string, error) {

	var (
		name = fmt.Sprintf("invpc2_vxlan.%d", vid)
		args = []string{"fdb", "show", "dev", name}
	)

	out, err := exec.Command("bridge", args...).Output()
	if err != nil {
		return nil, err
	}

	ls := map[string]string{}
	ar := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, v := range ar {

		vs := strings.Split(v, " ")
		if len(vs) < 7 ||
			vs[0] != vxlanForwardMacAddrDefault ||
			vs[2] != name {
			continue
		}

		ls[vs[4]] = vs[0]
	}

	return ls, err
}

func (it *linkManager) VxlanForward(vid int, ip net.IP) error {
	it.mu.Lock()
	defer it.mu.Unlock()
	if it.fdb == nil {
		it.fdb = map[string]string{}
	}
	if _, ok := it.fdb[ip.String()]; ok {
		return nil
	}
	var name = fmt.Sprintf("invpc2_vxlan.%d", vid)

	{
		args := []string{
			"fdb", "del", "00:00:00:00:00:00",
			"dev", name,
			"dst", ip.String(),
			"self",
		}
		exec.Command("bridge", args...).Output()
	}

	// bridge fdb show dev vxlan0
	var args = []string{"fdb", "add", "00:00:00:00:00:00",
		"dst", ip.String(),
		"dev", name,
		"self", "static",
	}

	_, err := exec.Command("bridge", args...).Output()
	if err == nil {
		it.fdb[ip.String()] = name
	}
	return err
}

func (it *linkManager) RouteReplace(vpcIp, brIp net.IP) error {
	/**
	r := &netlink.Route{
		//
	}
	return netlink.RouteReplace(r)
	*/
	args := []string{
		"route", "replace", vpcIp.String() + "/24",
		"via", brIp.String(),
	}
	_, err := exec.Command("ip", args...).Output()
	return err
}

func ParsePrivateIP(ipAddr string) (net.IP, error) {

	// Private IPv4
	// 10.0.0.0 ~ 10.255.255.255
	// 172.16.0.0 ~ 172.31.255.255
	// 192.168.0.0 ~ 192.168.255.255

	ip := net.ParseIP(ipAddr)
	if ip == nil {
		return nil, errors.New("invalid ip address")
	}

	ip = ip.To4()

	ipa := int(ip[0])
	ipb := int(ip[1])

	if ipa == 10 ||
		(ipa == 172 && ipb >= 16 && ipb <= 31) ||
		(ipa == 192 && ipb == 168) {
		return ip, nil
	}

	return nil, errors.New("invalid private ip address")
}
