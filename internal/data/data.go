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
	"encoding/json"
	"log/slog"

	"github.com/lynkdb/kvgo/v2/pkg/kvapi"
	"github.com/lynkdb/kvgo/v2/pkg/kvrep"
	"github.com/lynkdb/kvgo/v2/pkg/storage"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/config"
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
	}

	if c, err := kvrep.NewReplica(&storage.Options{
		DataDirectory: config.Prefix + "/var/zonelet_db",
	}); err != nil {
		return err
	} else {
		Zonelet = c
	}

	if c, err := kvrep.NewReplica(&storage.Options{
		DataDirectory: config.Prefix + "/var/package_db",
	}); err != nil {
		return err
	} else {
		Package = c
	}

	appInstanceMigrate()

	return nil
}

// appInstanceMigrate migrates legacy AppInstance records that store id/name as
// top-level JSON fields into the new Metadata-based format (meta.id, meta.name).
// This is a forward-compatible migration executed on zonelet startup.
func appInstanceMigrate() {

	var (
		offset = inapi.NsAppInstance(config.Config.Zonelet.ZoneName, "")
		rs     = Zonelet.NewRanger(offset, append(offset, 0xff)).
			SetLimit(10000).Exec()
	)

	for _, item := range rs.Items {

		// Parse raw JSON value to detect legacy format
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(item.Value, &raw); err != nil {
			continue
		}

		// Skip if "meta" field already present (new format)
		if _, hasMeta := raw["meta"]; hasMeta {
			continue
		}

		// Extract legacy id and name
		var (
			legacyID   string
			legacyName string
		)
		if v, ok := raw["id"]; ok {
			_ = json.Unmarshal(v, &legacyID)
		}
		if v, ok := raw["name"]; ok {
			_ = json.Unmarshal(v, &legacyName)
		}

		if legacyID == "" && legacyName == "" {
			continue
		}

		// Decode into AppInstance struct
		var instance inapi.AppInstance
		if err := item.JsonDecode(&instance); err != nil {
			continue
		}

		// Populate meta from legacy fields
		instance.Meta = &inapi.Metadata{
			Id:   legacyID,
			Name: legacyName,
		}

		// Write back with optimistic concurrency via prev version
		if rs := Zonelet.NewWriter(item.Key, &instance).
			SetPrevVersion(item.Meta.Version).Exec(); !rs.OK() {
			slog.Warn("app instance migration write failed",
				"key", string(item.Key),
				"err", rs.Error())
			continue
		}

		slog.Info("app instance migrated: legacy id/name -> meta",
			"instance_id", legacyID,
			"instance_name", legacyName)
	}
}

func Close() error {
	Hostlet.Close()
	Zonelet.Close()
	Package.Close()

	slog.Warn("database closed")
	return nil
}
