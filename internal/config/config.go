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
	kvclient "github.com/lynkdb/kvgo/v2/pkg/client"

	"github.com/sysinner/innerstack/v2/internal/inutil"
	"github.com/sysinner/innerstack/v2/pkg/inapi"
	"github.com/sysinner/innerstack/v2/pkg/inauth"
	"github.com/sysinner/innerstack/v2/pkg/inlog"
)

// ConfigCommon is the root of etc/innerstack.toml. Naming conventions:
// boolean switches use the participle form - a section master switch is
// "enabled", a feature nested in a broader section is "<feature>_enabled";
// unset-safe numerics use zero-value-unset semantics with getter fallbacks.
type ConfigCommon struct {
	filepath string

	Server   ServerConfig   `json:"server"   toml:"server"`
	Zonelet  ZoneletConfig  `json:"zonelet"  toml:"zonelet"`
	Hostlet  HostletConfig  `json:"hostlet"  toml:"hostlet"`
	Audit    AuditConfig    `json:"audit"    toml:"audit"`
	Alerts   AlertsConfig   `json:"alerts"   toml:"alerts"`
	Monstore MonstoreConfig `json:"monstore" toml:"monstore"`
	Metrics  MetricsConfig  `json:"metrics"  toml:"metrics"`

	ZoneDatabase    *kvclient.Config `json:"zone_database,omitempty"    toml:"zone_database,omitempty"`
	PackageDatabase *kvclient.Config `json:"package_database,omitempty" toml:"package_database,omitempty"`
}

// AuditConfig configures the audit log. Pointer fields distinguish an
// omitted [audit] section (defaults: enabled, 365 days) from explicit
// values ("enabled = false", "retention_days = 0" = keep forever); Setup
// materializes the defaults and config.Setup flushes them back to the file.
type AuditConfig struct {
	Enabled       *bool `json:"enabled"        toml:"enabled"`
	RetentionDays *int  `json:"retention_days" toml:"retention_days"`
}

// Setup applies the defaults: enabled, 365-day retention (MLPS 2.0 requires
// at least 6 months).
func (it *AuditConfig) Setup() {
	if it.Enabled == nil {
		enabled := true
		it.Enabled = &enabled
	}
	if it.RetentionDays == nil {
		days := 365
		it.RetentionDays = &days
	}
}

// On reports whether audit recording is enabled.
func (it *AuditConfig) On() bool {
	return it.Enabled != nil && *it.Enabled
}

// RetentionMs returns the record TTL in milliseconds; 0 means retain
// forever.
func (it *AuditConfig) RetentionMs() int64 {
	if it.RetentionDays == nil || *it.RetentionDays <= 0 {
		return 0
	}
	return int64(*it.RetentionDays) * 86400000
}

// Alert rule default parameters, applied by the value getters when a field
// is unset (zero) or out of range, so a single source defines each default.
const (
	AlertHostDownSecondsDefault        = 30
	AlertDiskFreePercentDefault        = 10
	AlertCrashloopWindowSecondsDefault = 600
	AlertCrashloopRestartsDefault      = 3
)

// AlertsConfig configures the built-in alert rules evaluated by the zone
// leader. Value fields with zero-value-unset semantics: the getters fall
// back to the defaults above for zero or invalid values, and an empty
// webhook disables notification.
type AlertsConfig struct {
	HostDownSeconds        int    `json:"host_down_seconds,omitempty"        toml:"host_down_seconds,omitempty"`
	DiskFreePercent        int    `json:"disk_free_percent,omitempty"        toml:"disk_free_percent,omitempty"`
	CrashloopWindowSeconds int    `json:"crashloop_window_seconds,omitempty" toml:"crashloop_window_seconds,omitempty"`
	CrashloopRestarts      int    `json:"crashloop_restarts,omitempty"       toml:"crashloop_restarts,omitempty"`
	WebhookURL             string `json:"webhook_url,omitempty"              toml:"webhook_url,omitempty"`
}

// HostDownSec returns the freshness threshold in seconds.
func (it *AlertsConfig) HostDownSec() int64 {
	if it.HostDownSeconds <= 0 {
		return AlertHostDownSecondsDefault
	}
	return int64(it.HostDownSeconds)
}

// DiskFreePercentV returns the free-disk threshold in whole percent (1..99).
func (it *AlertsConfig) DiskFreePercentV() int64 {
	if it.DiskFreePercent <= 0 || it.DiskFreePercent >= 100 {
		return AlertDiskFreePercentDefault
	}
	return int64(it.DiskFreePercent)
}

// CrashloopWindowSec returns the crashloop sliding window in seconds.
func (it *AlertsConfig) CrashloopWindowSec() int64 {
	if it.CrashloopWindowSeconds <= 0 {
		return AlertCrashloopWindowSecondsDefault
	}
	return int64(it.CrashloopWindowSeconds)
}

// CrashloopRestartsV returns the restart count that fires replica_crashloop
// within the window.
func (it *AlertsConfig) CrashloopRestartsV() int32 {
	if it.CrashloopRestarts <= 0 {
		return AlertCrashloopRestartsDefault
	}
	return int32(it.CrashloopRestarts)
}

// Webhook returns the alert webhook URL; empty disables notification.
func (it *AlertsConfig) Webhook() string {
	return it.WebhookURL
}

// MonstoreConfig configures the built-in host metrics history blocks.
// retention_days = 0 disables history storage entirely (the system keeps
// only the latest per-host snapshot, the pre-monstore behavior); a zero
// sample interval falls back to the 30-second default.
type MonstoreConfig struct {
	RetentionDays      int `json:"retention_days,omitempty"          toml:"retention_days,omitempty"`
	SampleIntervalSecs int `json:"sample_interval_seconds,omitempty" toml:"sample_interval_seconds,omitempty"`
}

// RetentionMs returns the block TTL in milliseconds; 0 disables monstore.
func (it *MonstoreConfig) RetentionMs() int64 {
	if it.RetentionDays <= 0 {
		return 0
	}
	return int64(it.RetentionDays) * 86400000
}

// SampleIntervalSec returns the sampling cadence in seconds, clamped to
// [10, 3600]; zero means the 30-second default.
func (it *MonstoreConfig) SampleIntervalSec() int64 {
	if it.SampleIntervalSecs <= 0 {
		return 30
	}
	return min(max(int64(it.SampleIntervalSecs), 10), 3600)
}

// MetricsConfig configures the optional /metrics Prometheus export.
// Disabled by default: the export is an opt-in extension for deployments
// with an external scraper (Prometheus / VictoriaMetrics); the built-in
// alerts and metrics history work without it.
type MetricsConfig struct {
	Enabled bool   `json:"enabled,omitempty" toml:"enabled,omitempty"`
	Token   string `json:"token,omitempty"   toml:"token,omitempty"`
}

// On reports whether the /metrics endpoints answer.
func (it *MetricsConfig) On() bool {
	return it.Enabled
}

// TokenV returns the optional Bearer token; empty disables authentication.
func (it *MetricsConfig) TokenV() string {
	return it.Token
}

type ServerConfig struct {
	HttpPort  int      `json:"http_port"  toml:"http_port"`
	PeerPort  int      `json:"peer_port"  toml:"peer_port"`
	ZoneHosts []string `json:"zone_hosts" toml:"zone_hosts"`

	PublicApiEnabled bool `json:"public_api_enabled" toml:"public_api_enabled"`
}

type HostletConfig struct {
	HostId  string `json:"host_id"  toml:"host_id"`
	LanAddr string `json:"lan_addr" toml:"lan_addr"`

	AccessKey string `json:"access_key" toml:"access_key"`
	ak        *inauth.AccessKey

	AppPath string `json:"app_path" toml:"app_path"`

	LxcFsEnabled bool `json:"lxc_fs_enabled" toml:"lxc_fs_enabled"`

	// InagentSlimEnabled selects the C++ inagent-slim binary as the container
	// inagent when true. Defaults to false, using the Go inagent build.
	InagentSlimEnabled bool `json:"inagent_slim_enabled,omitempty" toml:"inagent_slim_enabled,omitempty"`

	VpcBridgeIP     string `json:"vpc_bridge_ip,omitempty"     toml:"vpc_bridge_ip,omitempty"`
	VpcInstanceCIDR string `json:"vpc_instance_cidr,omitempty" toml:"vpc_instance_cidr,omitempty"`

	VpcNetworkDomain string   `json:"vpc_network_domain,omitempty" toml:"vpc_network_domain,omitempty"`
	DnsServers       []string `json:"dns_servers,omitempty"        toml:"dns_servers,omitempty"`
}

type ZoneletConfig struct {
	ZoneName string `json:"zone_name" toml:"zone_name"`

	VpcBridgeCidr    string `json:"vpc_bridge_cidr,omitempty"    toml:"vpc_bridge_cidr,omitempty"`
	VpcInstanceCidr  string `json:"vpc_instance_cidr,omitempty"  toml:"vpc_instance_cidr,omitempty"`
	VpcNetworkDomain string `json:"vpc_network_domain,omitempty" toml:"vpc_network_domain,omitempty"`

	AccessKeys []*AccessKeyPublic `json:"access_keys,omitempty" toml:"access_keys,omitempty"`
}

type AccessKeyPublic struct {
	AccessKey   string `json:"access_key"            toml:"access_key"`
	Description string `json:"description,omitempty" toml:"description,omitempty"`
	// User labels the key owner (e.g. "sysadmin"); empty for unlabeled
	// infrastructure keys.
	User string `json:"user,omitempty" toml:"user,omitempty"`
	// Type is User or App; inferred as User when a user is set and type is
	// empty.
	Type string `json:"type,omitempty" toml:"type,omitempty"`
}

// ApplyTo copies the declared identity onto a parsed access key: the TOML
// export string carries only id and secret.
func (it *AccessKeyPublic) ApplyTo(ak *inauth.AccessKey) {
	ak.User = it.User
	ak.Description = it.Description
	if it.Type != "" {
		ak.Type = it.Type
	} else if it.User != "" {
		ak.Type = "User"
	}
}

var (
	Version = ""

	Prefix = "."

	cfgFile = "innerstack.toml"

	Config ConfigCommon

	DefaultUserID  = 2048
	DefaultGroupID = 2048

	User = &user.User{
		Uid:      "2048",
		Gid:      "2048",
		Username: "action",
		HomeDir:  "/home/action",
	}
)

func Setup(version string) error {

	Version = version

	slog.Info("version " + version)

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

	Config.Audit.Setup()

	// Auto-create default sysadmin access key if not configured
	if len(Config.Zonelet.AccessKeys) == 0 ||
		Config.Zonelet.AccessKeys[0].AccessKey == "" {
		ak := inauth.NewUserAccessKey()
		ak.User = "sysadmin"
		Config.Zonelet.AccessKeys = []*AccessKeyPublic{
			{
				AccessKey:   ak.Export(),
				Description: "for sysadmin",
				User:        ak.User,
				Type:        ak.Type,
			},
		}
	}
	// Label the admin key of pre-existing configs, but only on evidence
	// this Setup auto-created it: the owner label flows into the
	// tamper-evident audit chain, so it is never guessed from slot
	// position. Persisted back to the TOML by the final Flush.
	if Config.Zonelet.AccessKeys[0].User == "" &&
		Config.Zonelet.AccessKeys[0].Description == "for sysadmin" {
		Config.Zonelet.AccessKeys[0].User = "sysadmin"
	}
	if len(Config.Zonelet.AccessKeys) == 1 {
		ak := inauth.NewAppAccessKey()
		ak.User = "ingate"
		Config.Zonelet.AccessKeys = append(Config.Zonelet.AccessKeys, &AccessKeyPublic{
			AccessKey:   ak.Export(),
			Description: "for ingate daemon",
			User:        ak.User,
			Type:        ak.Type,
		})
	}
	// Label the ingate key of pre-existing multi-key configs (created
	// before owner labels existed), mirroring the sysadmin restore above:
	// only on this Setup's own auto-generated description, never guessed
	// from slot position. The label flows into the tamper-evident audit
	// chain, so it is persisted back to the TOML by the final Flush.
	if len(Config.Zonelet.AccessKeys) > 1 &&
		Config.Zonelet.AccessKeys[1].User == "" &&
		Config.Zonelet.AccessKeys[1].Description == "for ingate daemon" {
		Config.Zonelet.AccessKeys[1].User = "ingate"
		Config.Zonelet.AccessKeys[1].Type = "App"
	}

	{
		if Config.Hostlet.AppPath == "" {
			Config.Hostlet.AppPath = Prefix + "/apps"
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
				ak.Type = inauth.AccessKey_Type_Host
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
