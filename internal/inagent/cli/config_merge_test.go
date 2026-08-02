// Copyright 2015 Eryx <evorily at gmail dot com>, All rights reserved.
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
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/ini.v1"
)

func TestMergeINI(t *testing.T) {
	type check struct {
		section, key, want string
	}
	type tc struct {
		name     string
		base     string // initial file content; "" means file is not created
		override string
		checks   []check
		// wantMissing asserts a key is absent from the merged file.
		wantMissing []check
	}

	const odooBase = `[options]
db_host = 10.0.0.5
db_port = 5432
http_port = 8069
data_dir = /opt/odoo/data
`

	tcases := []tc{
		{
			name: "override_updates_and_adds_keys_keeps_base",
			base: odooBase,
			override: `[options]
db_host = 10.1.2.3
workers = 8
`,
			checks: []check{
				{"options", "db_host", "10.1.2.3"}, // override wins
				{"options", "db_port", "5432"},     // base preserved
				{"options", "http_port", "8069"},   // base preserved
				{"options", "data_dir", "/opt/odoo/data"},
				{"options", "workers", "8"}, // new key added
			},
		},
		{
			name: "comment_only_override_keeps_base_intact",
			// The default odoo_conf field is just a commented-out template; the
			// merge must not wipe the base config rendered by config-render.
			base:     odooBase,
			override: "[options]\n;workers = 0\n;proxy_mode = True\n;log_level = info\n",
			checks: []check{
				{"options", "db_host", "10.0.0.5"},
				{"options", "db_port", "5432"},
				{"options", "http_port", "8069"},
			},
			wantMissing: []check{
				{"options", "workers", ""},
				{"options", "proxy_mode", ""},
			},
		},
		{
			name: "override_adds_new_section",
			base: odooBase,
			override: `[misc]
foo = bar
`,
			checks: []check{
				{"options", "db_host", "10.0.0.5"},
				{"misc", "foo", "bar"},
			},
		},
		{
			name:     "missing_base_file_becomes_override",
			base:     "", // file not created
			override: "[options]\ndb_host = 10.9.9.9\n",
			checks: []check{
				{"options", "db_host", "10.9.9.9"},
			},
		},
	}

	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "odoo.conf")
			if tc.base != "" {
				if err := os.WriteFile(path, []byte(tc.base), 0640); err != nil {
					t.Fatal(err)
				}
			}

			if err := mergeINI(path, tc.override); err != nil {
				t.Fatalf("mergeINI: %v", err)
			}

			got, err := ini.Load(path)
			if err != nil {
				t.Fatalf("reload merged file: %v", err)
			}
			for _, c := range tc.checks {
				sec, err := got.GetSection(c.section)
				if err != nil {
					t.Errorf("section %q missing: %v", c.section, err)
					continue
				}
				if v := sec.Key(c.key).Value(); v != c.want {
					t.Errorf("[%s] %s = %q, want %q", c.section, c.key, v, c.want)
				}
			}
			for _, c := range tc.wantMissing {
				sec, _ := got.GetSection(c.section)
				if sec != nil && sec.Key(c.key).Value() != "" {
					t.Errorf("[%s] %s unexpectedly present = %q", c.section, c.key, sec.Key(c.key).Value())
				}
			}
		})
	}
}
