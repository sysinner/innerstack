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

package pkgbuild

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sysinner/incore/v2/pkg/inapi"
)

func TestReplaceMetadataVersion(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		newVersion  string
		wantVersion string
		wantOk      bool
	}{
		{
			name: "standard metadata section",
			content: `[metadata]
name = "myapp"
version = "1.0.0"
description = "test app"

[build]
script = "make build"
`,
			newVersion:  "2.0.0",
			wantVersion: "2.0.0",
			wantOk:      true,
		},
		{
			name: "version with pre-release stripped",
			content: `[metadata]
name = "myapp"
version = "1.0.0"

[build]
script = "make build"
`,
			newVersion:  "2.0.0",
			wantVersion: "2.0.0",
			wantOk:      true,
		},
		{
			name: "indented version line",
			content: `[metadata]
  name = "myapp"
  version = "1.5.0"
`,
			newVersion:  "2.0.0",
			wantVersion: "2.0.0",
			wantOk:      true,
		},
		{
			name: "no metadata section",
			content: `[build]
version = "1.0.0"
`,
			newVersion: "2.0.0",
			wantOk:     false,
		},
		{
			name: "version only in release section not updated",
			content: `[metadata]
name = "myapp"
version = "1.0.0"

[release]
version = "3.0.0"
`,
			newVersion:  "2.0.0",
			wantVersion: "2.0.0",
			wantOk:      true,
		},
		{
			name:       "empty content",
			content:    ``,
			newVersion: "2.0.0",
			wantOk:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := replaceMetadataVersion(tt.content, tt.newVersion)

			if ok != tt.wantOk {
				t.Fatalf("replaceMetadataVersion() ok = %v, want %v", ok, tt.wantOk)
			}

			if !tt.wantOk {
				return
			}

			// Verify the result contains the new version in metadata section
			spec, err := SpecParseString(result)
			if err != nil {
				t.Fatalf("failed to parse result: %v", err)
			}

			if spec.Metadata.Version != tt.wantVersion {
				t.Errorf("metadata.version = %q, want %q", spec.Metadata.Version, tt.wantVersion)
			}
		})
	}
}

// SpecParseString parses a package spec from a TOML string.
func SpecParseString(tomlStr string) (*inapi.PackageSpec, error) {
	tmpFile, err := os.CreateTemp("", "ipk-test-*.toml")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(tomlStr); err != nil {
		tmpFile.Close()
		return nil, err
	}
	tmpFile.Close()

	return SpecParse(tmpFile.Name())
}

func TestUpdateSpecVersion(t *testing.T) {
	tests := []struct {
		name        string
		cfgVersion  string
		metaVersion string
		wantVersion string
		wantUpdated bool
		wantFileVer string
	}{
		{
			name:        "no version flag, no update",
			cfgVersion:  "",
			metaVersion: "1.0.0",
			wantVersion: "1.0.0",
			wantUpdated: false,
			wantFileVer: "1.0.0",
		},
		{
			name:        "lower version, no update",
			cfgVersion:  "1.0.0",
			metaVersion: "2.0.0",
			wantVersion: "2.0.0",
			wantUpdated: false,
			wantFileVer: "2.0.0",
		},
		{
			name:        "equal version, no update",
			cfgVersion:  "1.0.0",
			metaVersion: "1.0.0",
			wantVersion: "1.0.0",
			wantUpdated: false,
			wantFileVer: "1.0.0",
		},
		{
			name:        "higher version, update",
			cfgVersion:  "2.0.0",
			metaVersion: "1.0.0",
			wantVersion: "2.0.0",
			wantUpdated: true,
			wantFileVer: "2.0.0",
		},
		{
			name:        "pre-release higher core, update to core only",
			cfgVersion:  "2.0.0-beta.1",
			metaVersion: "1.0.0",
			wantVersion: "2.0.0",
			wantUpdated: true,
			wantFileVer: "2.0.0",
		},
		{
			name:        "pre-release lower core, no update",
			cfgVersion:  "1.0.0-beta.1",
			metaVersion: "1.0.0",
			wantVersion: "1.0.0",
			wantUpdated: false,
			wantFileVer: "1.0.0",
		},
		{
			// patch version must be preserved, not collapsed to X.Y.0
			name:        "higher patch version, update keeps patch",
			cfgVersion:  "1.2.3",
			metaVersion: "1.2.2",
			wantVersion: "1.2.3",
			wantUpdated: true,
			wantFileVer: "1.2.3",
		},
		{
			// pre-release with higher patch: core X.Y.Z must be kept intact
			name:        "pre-release higher patch, update to core patch",
			cfgVersion:  "1.2.4-rc1",
			metaVersion: "1.2.3",
			wantVersion: "1.2.4",
			wantUpdated: true,
			wantFileVer: "1.2.4",
		},
		{
			// two-part version (X.Y) is padded to X.Y.0 for comparison
			name:        "two-part version padded, lower patch no update",
			cfgVersion:  "1.2",
			metaVersion: "1.2.3",
			wantVersion: "1.2.3",
			wantUpdated: false,
			wantFileVer: "1.2.3",
		},
		{
			// two-part version higher minor updates and pads to X.Y.0
			name:        "two-part version higher minor updates",
			cfgVersion:  "1.3",
			metaVersion: "1.2.3",
			wantVersion: "1.3.0",
			wantUpdated: true,
			wantFileVer: "1.3.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory with ipk.toml
			tmpDir := t.TempDir()
			specPath := filepath.Join(tmpDir, "ipk.toml")
			content := `[metadata]
name = "testapp"
version = "` + tt.metaVersion + `"

[build]
script = "echo build"
`
			if err := os.WriteFile(specPath, []byte(content), 0644); err != nil {
				t.Fatalf("failed to write spec file: %v", err)
			}

			// Load spec
			_, spec, err := SpecFind(tmpDir, specPath)
			if err != nil {
				t.Fatalf("SpecFind failed: %v", err)
			}

			builder := &Builder{
				config: Config{
					Version: tt.cfgVersion,
					Quiet:   true, // suppress verbose output in tests
				},
				spec:     spec,
				specPath: specPath,
				verbose:  false,
			}

			if err := builder.updateSpecVersion(); err != nil {
				t.Fatalf("updateSpecVersion failed: %v", err)
			}

			// Check in-memory spec version
			if builder.spec.Metadata.Version != tt.wantVersion {
				t.Errorf("in-memory metadata.version = %q, want %q",
					builder.spec.Metadata.Version, tt.wantVersion)
			}

			// Check file on disk
			fileSpec, err := SpecParse(specPath)
			if err != nil {
				t.Fatalf("failed to re-parse spec file: %v", err)
			}
			if fileSpec.Metadata.Version != tt.wantFileVer {
				t.Errorf("file metadata.version = %q, want %q",
					fileSpec.Metadata.Version, tt.wantFileVer)
			}

			// Verify updated flag expectation
			wasUpdated := tt.wantFileVer != tt.metaVersion
			if wasUpdated != tt.wantUpdated {
				t.Errorf("updated = %v, want %v", wasUpdated, tt.wantUpdated)
			}
		})
	}
}
