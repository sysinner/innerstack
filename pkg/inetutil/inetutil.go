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

package inetutil

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// ParsePrivateIP validates that addr is a private IPv4 address (RFC 1918)
// and returns its 4-byte representation.
//
// Private ranges:
//   - 10.0.0.0/8
//   - 172.16.0.0/12
//   - 192.168.0.0/16
func ParsePrivateIP(addr string) ([]byte, error) {
	ip := net.ParseIP(addr)
	if ip == nil {
		return nil, errors.New("invalid ip address")
	}

	ip = ip.To4()
	if ip == nil {
		return nil, errors.New("invalid ipv4 address")
	}

	a := int(ip[0])
	b := int(ip[1])

	if a == 10 ||
		(a == 172 && b >= 16 && b <= 31) ||
		(a == 192 && b == 168) {
		return ip, nil
	}

	return nil, errors.New("invalid private ip address " + addr)
}

func ParsePrivateAddress(addr string) ([]byte, error) {
	ip, _, err := net.SplitHostPort(addr)
	if err != nil {
		// 如果地址不含端口，SplitHostPort 会报错，此时尝试直接解析
		ip = addr
	}

	return ParsePrivateIP(ip)
}

// BytesToUint32 converts a 4-byte big-endian slice to uint32.
func BytesToUint32(b []byte) uint32 {
	return binary.BigEndian.Uint32(b)
}

// Uint32ToBytes converts a uint32 to a 4-byte big-endian slice.
func Uint32ToBytes(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func Uint32ToIpv4(v uint32) string {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return IP4ToString(b)
}

func Uint32ToIp(v uint32) net.IP {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return net.IP(b)
}

// IP4ToString formats 4 bytes as a dotted-quad IP string.
func IP4ToString(b []byte) string {
	if len(b) < 4 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}

func Ipv4ToUint32(addr string) uint32 {
	ip := net.ParseIP(addr)
	if ip == nil {
		return 0
	}

	ip = ip.To4()
	if ip == nil {
		return 0
	}

	return BytesToUint32(ip)
}

// ParseCIDR parses a CIDR string and returns the IP and network.
func ParseCIDR(cidr string) (net.IP, *net.IPNet, error) {
	return net.ParseCIDR(cidr)
}
