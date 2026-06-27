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
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hooto/htoml4g/htoml"
	"golang.org/x/mod/semver"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// ValidOS defines the supported operating systems
var ValidOS = map[string]bool{
	"linux":   true,
	"freebsd": true,
	"darwin":  true,
	"all":     true,
}

// ValidArch defines the supported CPU architectures
var ValidArch = map[string]bool{
	"amd64": true,
	"arm64": true,
	"src":   true,
}

const (
	SpecFileName = "ipk.toml"
)

var (
	// NameRegex validates package names (lowercase, alphanumeric, hyphens, underscores)
	NameRegex = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

// SpecFileSearchOrder defines the order to search for spec files
var SpecFileSearchOrder = []string{
	SpecFileName,
	filepath.Join(".ipk", SpecFileName),
	filepath.Join("misc", "ipk", SpecFileName),
}

// SpecParse parses a package spec from a TOML file
func SpecParse(path string) (*inapi.PackageSpec, error) {
	var spec inapi.PackageSpec

	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("spec file not found: %s", path)
	}

	if err := htoml.DecodeFromFile(path, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse spec file %s: %w", path, err)
	}

	return &spec, nil
}

// SpecFind locates a spec file in the search order
func SpecFind(packDir string, specOverride string) (string, *inapi.PackageSpec, error) {
	var searchPaths []string

	if specOverride != "" {
		searchPaths = []string{specOverride}
	}
	for _, p := range SpecFileSearchOrder {
		searchPaths = append(searchPaths, p)
	}

	for _, relPath := range searchPaths {
		fullPath := relPath
		if !filepath.IsAbs(relPath) {
			fullPath = filepath.Join(packDir, relPath)
		}

		spec, err := SpecParse(fullPath)
		if err == nil {
			return fullPath, spec, nil
		}
		if !os.IsNotExist(err) {
			return "", nil, err
		}
	}

	return "", nil, fmt.Errorf("no spec file found in %s (searched: %s)", packDir, strings.Join(searchPaths, ", "))
}

// MetadataValidate validates package metadata
func MetadataValidate(meta *inapi.PackageMetadata) error {
	if meta == nil {
		return fmt.Errorf("metadata is required")
	}

	if meta.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}

	if !NameRegex.MatchString(meta.Name) {
		return fmt.Errorf("invalid metadata.name '%s': must be lowercase, start with a letter, and contain only alphanumeric, hyphens, or underscores", meta.Name)
	}

	if meta.Version == "" {
		return fmt.Errorf("metadata.version is required")
	}

	// Validate version using semver (requires "v" prefix)
	// Metadata.Version should be core version only (MAJOR.MINOR.PATCH)
	v := meta.Version
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return fmt.Errorf("invalid metadata.version '%s': must be semantic version core format (e.g., 1.0.0, v2.1.0)", meta.Version)
	}

	return nil
}

// SpecValidate validates a package spec
func SpecValidate(spec *inapi.PackageSpec) error {
	if spec.Metadata == nil {
		return fmt.Errorf("[metadata] section is required")
	}

	if spec.Build == nil {
		return fmt.Errorf("[build] section is required")
	}

	if err := MetadataValidate(spec.Metadata); err != nil {
		return err
	}

	return nil
}

// ReleaseValidate validates package release info
func ReleaseValidate(release *inapi.PackageRelease) error {
	if release == nil {
		return fmt.Errorf("release is required")
	}

	if release.Version == "" {
		return fmt.Errorf("release.version is required")
	}

	// Validate version using semver (requires "v" prefix)
	// Release.Version can include pre-release and build metadata
	v := release.Version
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return fmt.Errorf("invalid release.version '%s': must be semantic version format (e.g., 1.0.0, 1.0.0-beta.1)", release.Version)
	}

	if release.Os == "" {
		return fmt.Errorf("release.os is required")
	}
	if !ValidOS[release.Os] {
		return fmt.Errorf("invalid release.os '%s': must be one of linux, freebsd, darwin, all", release.Os)
	}

	if release.Arch == "" {
		return fmt.Errorf("release.arch is required")
	}
	if !ValidArch[release.Arch] {
		return fmt.Errorf("invalid release.arch '%s': must be one of amd64, arm64, src", release.Arch)
	}

	return nil
}
