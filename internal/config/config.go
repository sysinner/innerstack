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
	"net/netip"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/hooto/htoml4g/htoml"

	"github.com/sysinner/incore/v2/internal/inutil"
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
	HostId    string `json:"host_id" toml:"host_id"`
	LanAddr   string `json:"lan_addr" toml:"lan_addr"`
	SecretKey string `json:"secret_key" toml:"secret_key"`
	PodPath   string `json:"pod_path" toml:"pod_path"`
}

type ZoneletConfig struct {
	ZoneId string `json:"zone_id" toml:"zone_id"`
	// ZoneName string `json:"zone_name" toml:"zone_name"`
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

	{
		Config.Hostlet.PodPath = Prefix + "/pod"

		if len(Config.Hostlet.HostId) < 12 {
			Config.Hostlet.HostId = inutil.SeqRandHexString(4, 8)
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

		if Config.Hostlet.SecretKey == "" {
			Config.Hostlet.SecretKey, _ = inutil.GenerateSecretKeyBase62(32)
		}
	}

	return Config.Flush()
}

func Flush() error {
	return Config.Flush()
}

func (cfg *ConfigCommon) Flush() error {
	if cfg.filepath != "" {
		return htoml.EncodeToFile(Config, cfg.filepath, nil)
	}
	return nil
}
