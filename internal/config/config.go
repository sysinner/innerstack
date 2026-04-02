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

package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/hooto/htoml4g/htoml"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/pkg/inauth"
	"github.com/sysinner/incore/v2/pkg/inlog"
)

var (
	User = &user.User{
		Uid:      "2048",
		Gid:      "2048",
		Username: "action",
		HomeDir:  "/home/action",
	}
	DefaultUserID  = 2048
	DefaultGroupID = 2048
)

type ConfigCommon struct {
	filepath string

	Server  ServerConfig  `json:"server" toml:"server"`
	Zonelet ZoneletConfig `json:"zonelet" toml:"zonelet"`
	Hostlet HostletConfig `json:"hostlet" toml:"hostlet"`
}

type ServerConfig struct {
	HttpPort  int      `json:"http_port" toml:"http_port"`
	PeerPort  int      `json:"peer_port" toml:"peer_port"`
	ZoneHosts []string `json:"zone_hosts" toml:"zone_hosts"`
}

type HostletConfig struct {
	HostId  string `json:"host_id" toml:"host_id"`
	LanAddr string `json:"lan_addr" toml:"lan_addr"`

	AccessKey string `json:"access_key" toml:"access_key"`
	ak        *inauth.AccessKey

	PodPath string `json:"pod_path" toml:"pod_path"`

	VpcBridgeIP     string `json:"vpc_bridge_ip,omitempty" toml:"vpc_bridge_ip,omitempty"`
	VpcInstanceCIDR string `json:"vpc_instance_cidr,omitempty" toml:"vpc_instance_cidr,omitempty"`
}

type ZoneletConfig struct {
	ZoneName string `json:"zone_name" toml:"zone_name"`

	VpcBridgeCidr    string `json:"vpc_bridge_cidr,omitempty" toml:"vpc_bridge_cidr,omitempty"`
	VpcInstanceCidr  string `json:"vpc_instance_cidr,omitempty" toml:"vpc_instance_cidr,omitempty"`
	VpcNetworkDomain string `json:"vpc_network_domain,omitempty" toml:"vpc_network_domain,omitempty"`

	AccessKeys []*AccessKeyPublic `json:"access_keys,omitempty" toml:"access_keys,omitempty"`
}

type AccessKeyPublic struct {
	AccessKey string `json:"access_key" toml:"access_key"`
}

var (
	Version = ""
	Release = ""

	Prefix = "."

	cfgFile = "instack.toml"

	Config ConfigCommon
)

func Setup(ver, rel string) error {

	Version = ver
	Release = rel

	inlog.Setup()

	if v, err := filepath.Abs(filepath.Dir(os.Args[0])); err == nil {
		Prefix = strings.TrimSuffix(v, "/bin")
	}

	if err := htoml.DecodeFromFile(Prefix+"/etc/"+cfgFile, &Config); err != nil {
		if !os.IsNotExist(err) {
			slog.Info(err.Error())
			return err
		}
	}

	Config.filepath = Prefix + "/etc/" + cfgFile

	{
		if Config.Server.HttpPort == 0 {
			Config.Server.HttpPort = 9532
		}
		if Config.Server.PeerPort == 0 {
			Config.Server.PeerPort = 9533
		}
	}

	// Auto-create default sysadmin access key if not configured
	if len(Config.Zonelet.AccessKeys) == 0 ||
		Config.Zonelet.AccessKeys[0].AccessKey == "" {
		ak := inauth.NewAccessKey()
		Config.Zonelet.AccessKeys = []*AccessKeyPublic{
			{
				AccessKey: ak.Export(),
			},
		}
	}

	{
		if Config.Hostlet.PodPath == "" {
			Config.Hostlet.PodPath = Prefix + "/pod"
		}

		if Config.Hostlet.HostId == "" {
			Config.Hostlet.HostId = inutil.SeqRandHexString(4, 8)
		}

		if Config.Hostlet.AccessKey == "" {
			ak := inauth.NewAccessKey()
			ak.Id = Config.Hostlet.HostId
			Config.Hostlet.AccessKey = ak.Export()
		}

		if Config.Hostlet.ak == nil {
			if ak, err := inauth.ParseAccessKey(Config.Hostlet.AccessKey); err != nil {
				return fmt.Errorf("hostlet access_key parse error: %w", err)
			} else {
				ak.Scopes = []string{
					inapi.AuthScope_Host_Write + ":" + Config.Hostlet.HostId,
					inapi.AuthScope_Package_Read,
				}
				Config.Hostlet.ak = ak
			}
		}

		if Config.Hostlet.LanAddr == "" {
			if ip, err := inutil.LookupPrivateIP(); err != nil {
				return err
			} else {
				Config.Hostlet.LanAddr = ip
			}
		} else {
			if _, err := netip.ParseAddr(Config.Hostlet.LanAddr); err != nil {
				return err
			}

			if !inutil.IsLocalIP(Config.Hostlet.LanAddr) &&
				Config.Hostlet.LanAddr != "127.0.0.1" {
				return errors.New("lan_addr " + Config.Hostlet.LanAddr +
					" not found in local network interfaces")
			}
		}
	}

	return Config.Flush()
}

func Flush() error {
	return Config.Flush()
}

func (cfg *ConfigCommon) Flush() error {
	if cfg.filepath != "" {
		err := htoml.EncodeToFile(Config, cfg.filepath, nil)
		if err != nil {
			return err
		}
		os.Chmod(cfg.filepath, 0600)
	}
	return nil
}

func (it *HostletConfig) AuthKey() *inauth.AccessKey {
	return it.ak
}
