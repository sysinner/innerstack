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

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hooto/htoml4g/htoml"

	"github.com/sysinner/innerstack/v2/pkg/inauth"
)

// Config represents the CLI configuration file
type ConfigCommon struct {
	CurrentZone string        `toml:"current_zone"`
	Zones       []*ConfigZone `toml:"zones"`
}

// ConfigZone represents a single zone accessKey
type ConfigZone struct {
	Name string `toml:"name"`
	Addr string `toml:"addr"`
	AK   string `toml:"access_key"` // full access key, format: ak_<id>_<secret>
}

const AppName = "innerstack"

const configFileName = "innerstack_config.toml"

// Config is the loaded configuration
var Config ConfigCommon

// configPaths are the paths to search for config files (in order)
var configPaths = []string{
	configFileName,
}

var defaultConfigPath = configFileName

var loadedConfigPath string

// AccessKey parses the AK string (ak_{id}_{secret}) into an AccessKey
func (c *ConfigZone) AccessKey() (*inauth.AccessKey, error) {
	if c.AK == "" {
		return nil, errors.New("access_key not set")
	}
	return inauth.ParseAccessKey(c.AK)
}

func init() {
	if homeDir, err := os.UserHomeDir(); err == nil {
		defaultConfigPath = filepath.Join(homeDir, "."+configFileName)
		configPaths = append(configPaths, defaultConfigPath)
	}
}

// Setup loads the CLI configuration from file. A missing config file is not an
// error: Config stays empty and loadedConfigPath is set to the default path so
// that Flush can create it on the first `login add`.
func Setup() error {

	for _, path := range configPaths {
		if err := htoml.DecodeFromFile(path, &Config); err == nil {
			loadedConfigPath = path
			return os.Chmod(path, 0600)
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	// No config found; default to the home-dir path for the next Flush.
	loadedConfigPath = defaultConfigPath
	return nil
}

// Zone returns the zone config by name, or the current zone if name is empty.
func (it *ConfigCommon) Zone(name string) (*ConfigZone, error) {

	if name == "" {
		name = it.CurrentZone
	}

	if name == "" && len(it.Zones) == 0 {
		return nil, fmt.Errorf("no zone configured, run `%s login -n <zone> -a <host:port> -s <access-key>` first (see `%s login --help`)", AppName, AppName)
	}

	if name == "" && len(it.Zones) > 0 {
		return it.Zones[0], nil
	}

	for _, conn := range it.Zones {
		if conn.Name == name {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("zone (%s) not found in %s", name, loadedConfigPath)
}

// Flush writes the current configuration back to the config file. The file is
// recreated with 0600 permissions since it holds access keys.
func Flush() error {
	if err := htoml.EncodeToFile(Config, loadedConfigPath); err != nil {
		return err
	}
	return os.Chmod(loadedConfigPath, 0600)
}
