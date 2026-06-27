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

package data

import (
	"log/slog"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
	"github.com/lynkdb/kvgo/v2/pkg/kvrep"
	"github.com/lynkdb/kvgo/v2/pkg/storage"

	"github.com/sysinner/innerstack/v2/internal/config"
)

var (
	Hostlet kvapi.Client

	Zonelet kvapi.Client

	Package kvapi.Client
)

func Setup() error {

	if c, err := kvrep.NewReplica(&storage.Options{
		DataDirectory: config.Prefix + "/var/hostlet_db",
	}); err != nil {
		return err
	} else {
		Hostlet = c
		slog.Info("database connected", "name", "hostlet", "type", "local-replica")
	}

	if config.Config.ZoneDatabase != nil &&
		config.Config.ZoneDatabase.Database != "" {
		c, err := config.Config.ZoneDatabase.NewClient()
		if err != nil {
			return err
		}
		Zonelet = c
		slog.Info("database connected", "name", "zonelet", "type", "remote")
	} else {
		if c, err := kvrep.NewReplica(&storage.Options{
			DataDirectory: config.Prefix + "/var/zonelet_db",
		}); err != nil {
			return err
		} else {
			Zonelet = c
			slog.Info("database connected", "name", "zonelet", "type", "local-replica")
		}
	}

	if config.Config.PackageDatabase != nil &&
		config.Config.PackageDatabase.Database != "" {
		c, err := config.Config.PackageDatabase.NewClient()
		if err != nil {
			return err
		}
		Package = c
		slog.Info("database connected", "name", "package", "type", "remote")
	} else {
		if c, err := kvrep.NewReplica(&storage.Options{
			DataDirectory: config.Prefix + "/var/package_db",
		}); err != nil {
			return err
		} else {
			Package = c
			slog.Info("database connected", "name", "package", "type", "local-replica")
		}
	}

	return nil
}

func Close() error {
	Hostlet.Close()
	Zonelet.Close()
	Package.Close()

	slog.Warn("database closed")
	return nil
}
