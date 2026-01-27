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
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	hauth1 "github.com/hooto/hauth/go"
	"github.com/hooto/htoml4g/htoml"
	iamcfg "github.com/hooto/iam/config"
	"github.com/lessos/lessgo/crypto/idhash"

	// "github.com/lynkdb/kvgo"
	kvclient "github.com/lynkdb/kvgo/v2/pkg/client"

	"github.com/sysinner/incore/inapi"
)

type HostConfig struct {
	Id                 string `json:"id" toml:"id"`
	ZoneId             string `json:"zone_id" toml:"zone_id"`
	CellId             string `json:"cell_id" toml:"cell_id"`
	LanAddr            string `json:"lan_addr" toml:"lan_addr"`
	WanAddr            string `json:"wan_addr,omitempty" toml:"wan_addr,omitempty"`
	SecretKey          string `json:"secret_key" toml:"secret_key"`
	PprofHttpPort      uint16 `json:"pprof_http_port,omitempty" toml:"pprof_http_port,omitempty"`
	NetworkVpcBridge   string `json:"network_vpc_bridge,omitempty" toml:"network_vpc_bridge,omitempty"`
	NetworkVpcInstance string `json:"network_vpc_instance,omitempty" toml:"network_vpc_instance,omitempty"`
}

type HostJoinRequest struct {
	ZoneAddr      string `json:"zone_addr" toml:"zone_addr"`
	HostId        string `json:"host_id" toml:"host_id"`
	HostAddr      string `json:"host_addr" toml:"host_addr"`
	HostSecretKey string `json:"host_secret_key" toml:"host_secret_key"`
	CellId        string `json:"cell_id" toml:"cell_id"`
}

type ZoneInitRequest struct {
	HostAddr string `json:"host_addr" toml:"host_addr"`
	ZoneId   string `json:"zone_id" toml:"zone_id"`
	CellId   string `json:"cell_id" toml:"cell_id"`
	HttpPort uint16 `json:"http_port" toml:"http_port"`
	Password string `json:"password" toml:"password"`
	WanAddr  string `json:"wan_addr,omitempty" toml:"wan_addr,omitempty"`
}

type ZoneConfig struct {
	InstanceId            string                   `json:"instance_id,omitempty" toml:"instance_id,omitempty"`
	ZoneId                string                   `json:"zone_id" toml:"zone_id"`
	MainNodes             []string                 `json:"main_nodes" toml:"main_nodes"`
	HttpPort              uint16                   `json:"http_port" toml:"http_port"`
	PodHomeDir            string                   `json:"pod_home_dir" toml:"pod_home_dir"`
	ImageServices         []*inapi.ResImageService `json:"image_services,omitempty" toml:"image_services,omitempty"`
	InpackServiceUrl      string                   `json:"inpack_service_url,omitempty" toml:"inpack_service_url,omitempty"`
	InpanelServiceUrl     string                   `json:"inpanel_service_url,omitempty" toml:"inpanel_service_url,omitempty"`
	IamServiceUrlFrontend string                   `json:"iam_service_url_frontend,omitempty" toml:"iam_service_url_frontend,omitempty"`
	IamServiceUrlGlobal   string                   `json:"iam_service_url_global,omitempty" toml:"iam_service_url_global,omitempty"`
	NetworkDomainName     string                   `json:"network_domain_name,omitempty" toml:"network_domain_name,omitempty"`
}

type ZoneMainConfig struct {
	DataTableZone      string              `json:"data_table_zone" toml:"data_table_zone"`
	DataTableGlobal    string              `json:"data_table_global" toml:"data_table_global"`
	DataTableInpack    string              `json:"data_table_inpack" toml:"data_table_inpack"`
	MultiZoneEnable    bool                `json:"multi_zone_enable" toml:"multi_zone_enable"`
	MultiCellEnable    bool                `json:"multi_cell_enable" toml:"multi_cell_enable"`
	MultiHostEnable    bool                `json:"multi_host_enable" toml:"multi_host_enable"`
	MultiReplicaEnable bool                `json:"multi_replica_enable" toml:"multi_replica_enable"`
	SchedulerPlugin    string              `json:"scheduler_plugin,omitempty" toml:"scheduler_plugin,omitempty"`
	LocaleLang         string              `json:"locale_lang" toml:"locale_lang" desc:"locale language name. default: en"`
	IamAccessKey       *hauth1.AccessKey   `json:"iam_access_key,omitempty" toml:"iam_access_key,omitempty"`
	SysAccessKeys      []*hauth1.AccessKey `json:"sys_access_keys,omitempty" toml:"sys_access_keys,omitempty"`
}

type ConfigCommon struct {
	filepath string `json:"-" toml:"-"`

	Host       HostConfig           `json:"host" toml:"host"`
	Zone       ZoneConfig           `json:"zone" toml:"zone"`
	ZoneMain   *ZoneMainConfig      `json:"zone_main,omitempty" toml:"zone_main,omitempty"`
	IamService *iamcfg.ConfigCommon `json:"iam_service,omitempty" toml:"iam_service,omitempty"`
	// ZoneData   *kvgo.Config         `json:"zone_data,omitempty" toml:"zone_data,omitempty"`

	GlobDatabase *kvclient.Config `json:"glob_database,omitempty" toml:"glob_database,omitempty"`
	ZoneDatabase *kvclient.Config `json:"zone_database,omitempty" toml:"zone_database,omitempty"`
	PackDatabase *kvclient.Config `json:"pack_database,omitempty" toml:"pack_database,omitempty"`
}

type HostInfoReply struct {
	ConfigCommon
	Status struct {
		ZoneMainNode bool  `json:"zone_main_node,omitempty" toml:"zone_main_node,omitempty"`
		ZoneLeader   bool  `json:"zone_leader,omitempty" toml:"zone_leader,omitempty"`
		HostUptime   int64 `json:"host_uptime,omitempty" toml:"host_uptime,omitempty"`
	} `json:"status,omitempty" toml:"status,omitempty"`
	JobStatus interface{} `json:"job_status,omitempty" toml:"job_status,omitempty"`
}

func (cfg *ConfigCommon) Flush() error {
	if cfg.filepath != "" {
		return htoml.EncodeToFile(Config, cfg.filepath, nil)
	}
	return nil
}

var (
	Prefix = "/opt/sysinner/innerstack"
	Config ConfigCommon
	User   = &user.User{
		Uid:      "2048",
		Gid:      "2048",
		Username: "action",
		HomeDir:  "/home/action",
	}
	InitZoneId       = "z1"
	InitCellId       = "g1"
	SysConfigurators = []*inapi.SysConfigurator{}
	err              error
	DefaultUserID    = 2048
	DefaultGroupID   = 2048

	KeyMgr = hauth1.NewAccessKeyManager()
)

func BasicSetup() error {

	if err := setupUser(); err != nil {
		return err
	}

	if v, err := filepath.Abs(filepath.Dir(os.Args[0]) + "/.."); err == nil {
		Prefix = v
	}

	if err := htoml.DecodeFromFile(Prefix+"/etc/config.toml", &Config); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}

	Config.filepath = Prefix + "/etc/config.toml"

	if len(Config.Host.Id) < 16 {
		Config.Host.Id = idhash.RandHexString(16)
	}

	// Private IPv4
	// 10.0.0.0 ~ 10.255.255.255
	// 172.16.0.0 ~ 172.31.255.255
	// 192.168.0.0 ~ 192.168.255.255
	lanAddr := inapi.HostNodeAddress(Config.Host.LanAddr)
	if !lanAddr.Valid() {

		// auto setup local area ip address
		addrs, _ := net.InterfaceAddrs()
		reg, _ := regexp.Compile(`^(.*)\.(.*)\.(.*)\.(.*)\/(.*)$`)
		for _, addr := range addrs {

			ips := reg.FindStringSubmatch(addr.String())
			if len(ips) != 6 || (ips[1] == "127" && ips[2] == "0") {
				continue
			}

			ipa, _ := strconv.Atoi(ips[1])
			ipb, _ := strconv.Atoi(ips[2])

			if ipa == 10 ||
				(ipa == 172 && ipb >= 16 && ipb <= 31) ||
				(ipa == 192 && ipb == 168) {

				lanAddr.SetIP(
					fmt.Sprintf("%s.%s.%s.%s", ips[1], ips[2], ips[3], ips[4]),
				)
				break
			}
		}

		if len(lanAddr) < 8 {
			lanAddr.SetIP("127.0.0.1")
		}
	}

	if lanAddr.Port() < 1 {
		lanAddr.SetPort(9529)
	}

	Config.Host.LanAddr = lanAddr.String()

	if Config.Zone.HttpPort < 1 {
		Config.Zone.HttpPort = 9530
	}

	if len(Config.Host.SecretKey) < 32 {
		Config.Host.SecretKey = idhash.RandBase64String(40)
	}

	if IsZoneMaster() && Config.ZoneMain == nil {
		Config.ZoneMain = &ZoneMainConfig{}
	}

	if !inapi.ResNetworkDomainNameRE.MatchString(Config.Zone.NetworkDomainName) {
		Config.Zone.NetworkDomainName = "local"
	}

	/**
	{
		if len(Config.Zone.ImageServices) == 0 && len(Config.ImageServices) > 0 {
			Config.Zone.ImageServices = Config.ImageServices
		}

		if len(Config.Zone.MainNodes) == 0 && len(Config.Masters) > 0 {
			Config.Zone.MainNodes = Config.Masters
		}

		if Config.Zone.InpackServiceUrl == "" && Config.InpackServiceUrl != "" {
			Config.Zone.InpackServiceUrl = Config.InpackServiceUrl
		}

		if Config.Zone.InpanelServiceUrl == "" && Config.InpanelServiceUrl != "" {
			Config.Zone.InpanelServiceUrl = Config.InpanelServiceUrl
		}

		if Config.Zone.PodHomeDir == "" && Config.PodHomeDir != "" {
			Config.Zone.PodHomeDir = Config.PodHomeDir
		}

		if Config.Zone.ZoneId == "" && Config.Host.ZoneId != "" {
			Config.Zone.ZoneId = Config.Host.ZoneId
		}

		if Config.ZoneMain.IamAccessKey == nil && Config.DelZoneIamAccessKey != nil {
			Config.ZoneMain.IamAccessKey = Config.DelZoneIamAccessKey
		}
	}
	*/

	if Config.ZoneMain != nil {
		for _, ak := range Config.ZoneMain.SysAccessKeys {
			KeyMgr.KeySet(ak)
		}
		if KeyMgr.Count() == 0 {
			KeyMgr.KeySet(hauth1.NewAccessKey())
		}
	}

	return Config.Flush()
}

func Setup() error {

	//
	if Config.Zone.PodHomeDir == "" {
		os.MkdirAll("/opt/sysinner/pods", 0755)
		Config.Zone.PodHomeDir = "/opt/sysinner/pods"
	}

	if Config.Zone.IamServiceUrlFrontend != "" &&
		!strings.HasPrefix(Config.Zone.IamServiceUrlFrontend, "http") {
		return fmt.Errorf("Invalid iam_service_url_frontend")
	}

	if Config.Zone.InpanelServiceUrl == "" {
		Config.Zone.InpanelServiceUrl = fmt.Sprintf("http://%s/in", Config.Host.LanAddr)
	}

	if Config.ZoneMain != nil && Config.ZoneMain.LocaleLang == "" {
		Config.ZoneMain.LocaleLang = "en"
	}

	return Config.Flush()
}

func setupUser() error {

	if runtime.GOOS == "linux" {

		if u, err := user.Current(); err != nil || u.Uid != "0" {
			return fmt.Errorf("Access Denied : must be run as root")
		}

		if _, err := user.Lookup(User.Username); err != nil {

			nologin, err := exec.LookPath("nologin")
			if err != nil {
				nologin = "/sbin/nologin"
			}

			if _, err = exec.Command(
				"/usr/sbin/useradd",
				"-d", User.HomeDir,
				"-s", nologin,
				"-u", User.Uid, User.Username,
			).Output(); err != nil {
				return err
			}
		}
	} else {
		DefaultUserID = os.Getuid()
		DefaultGroupID = os.Getgid()
	}

	return nil
}

func IsZoneMaster() bool {
	if Config.Zone.ZoneId == "" || len(Config.Zone.MainNodes) == 0 {
		return false
	}
	for _, v := range Config.Zone.MainNodes {
		if v == Config.Host.LanAddr {
			return true
		}
	}
	return false
}
