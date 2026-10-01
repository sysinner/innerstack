// Copyright 2026 Eryx <evorui at gmail dot com>, All rights reserved.
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
	"os"
	"path/filepath"
	"testing"

	"github.com/hooto/htoml4g/htoml"
)

type auditConfigFile struct {
	Audit *AuditConfig `toml:"audit"`
}

// TestAuditConfigDefaults verifies that an omitted [audit] section gets
// enabled/365d defaults while explicit edge values (disabled, retain
// forever) survive a TOML round trip.
func TestAuditConfigDefaults(t *testing.T) {

	fixtures := []struct {
		name string

		// body is the raw TOML file content ("" = file without [audit]).
		body string

		wantOn          bool
		wantRetentionMs int64

		// wantReload* are the expected values after Setup + re-encode +
		// re-decode (explicit settings must survive).
		wantReloadOn          bool
		wantReloadRetentionMs int64
	}{
		{
			name:                  "section_absent_defaults",
			body:                  "",
			wantOn:                true,
			wantRetentionMs:       365 * 86400000,
			wantReloadOn:          true,
			wantReloadRetentionMs: 365 * 86400000,
		},
		{
			name:                  "explicitly_disabled",
			body:                  "[audit]\nenabled = false\nretention_days = 30\n",
			wantOn:                false,
			wantRetentionMs:       30 * 86400000,
			wantReloadOn:          false,
			wantReloadRetentionMs: 30 * 86400000,
		},
		{
			name:                  "retain_forever_zero",
			body:                  "[audit]\nenabled = true\nretention_days = 0\n",
			wantOn:                true,
			wantRetentionMs:       0,
			wantReloadOn:          true,
			wantReloadRetentionMs: 0,
		},
	}

	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {

			var cfg AuditConfig

			if f.body != "" {
				path := filepath.Join(t.TempDir(), "audit.toml")
				if err := os.WriteFile(path, []byte(f.body), 0600); err != nil {
					t.Fatal(err)
				}
				var file auditConfigFile
				if err := htoml.DecodeFromFile(path, &file); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if file.Audit != nil {
					cfg = *file.Audit
				}
			}

			cfg.Setup()

			if cfg.On() != f.wantOn {
				t.Fatalf("On() = %v, want %v", cfg.On(), f.wantOn)
			}
			if ms := cfg.RetentionMs(); ms != f.wantRetentionMs {
				t.Fatalf("RetentionMs() = %d, want %d", ms, f.wantRetentionMs)
			}

			// Round trip through TOML: explicit values must persist.
			path := filepath.Join(t.TempDir(), "roundtrip.toml")
			if err := htoml.EncodeToFile(auditConfigFile{Audit: &cfg}, path, nil); err != nil {
				t.Fatalf("encode: %v", err)
			}

			var reloaded auditConfigFile
			if err := htoml.DecodeFromFile(path, &reloaded); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if reloaded.Audit == nil {
				t.Fatal("[audit] section lost in round trip")
			}

			if reloaded.Audit.On() != f.wantReloadOn {
				t.Fatalf("after round trip On() = %v, want %v",
					reloaded.Audit.On(), f.wantReloadOn)
			}
			if ms := reloaded.Audit.RetentionMs(); ms != f.wantReloadRetentionMs {
				t.Fatalf("after round trip RetentionMs() = %d, want %d",
					ms, f.wantReloadRetentionMs)
			}
		})
	}
}
