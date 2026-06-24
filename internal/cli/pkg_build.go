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
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/internal/pkgbuild"
)

// NewPkgBuildCommand creates the "pkg-build" command for building .ipk packages.
// An .ipk package is a distributable archive containing application binaries
// and metadata, ready for deployment to zonelet servers.
func NewPkgBuildCommand() *cobra.Command {

	var (
		dir        string
		output     string
		spec       string
		version    string
		os         string
		arch       string
		compress   string
		noCompress bool
		showBuild  bool
		buildDir   string
		quiet      bool
	)

	var runE = func(cmd *cobra.Command, args []string) error {
		// Validate OS: linux, freebsd, or all (for cross-platform packages)
		if os != "" && os != "linux" && os != "freebsd" && os != "all" {
			return fmt.Errorf("invalid --os: must be 'linux', 'freebsd' or 'all'")
		}

		// Validate architecture: amd64, arm64, or src (source package)
		if arch != "" && arch != "amd64" && arch != "arm64" && arch != "src" {
			return fmt.Errorf("invalid --arch: must be 'amd64', 'arm64' or 'src'")
		}

		// Validate compression format
		if compress != "" && compress != "xz" && compress != "gzip" {
			return fmt.Errorf("invalid --compress: must be 'xz' or 'gzip'")
		}

		cfg := pkgbuild.Config{
			Dir:        dir,
			OutputDir:  output,
			SpecFile:   spec,
			Version:    version,
			Os:         os,
			Arch:       arch,
			Compress:   compress,
			NoCompress: noCompress,
			ShowBuild:  showBuild,
			BuildDir:   buildDir,
			Quiet:      quiet,
		}

		builder := pkgbuild.NewBuilder(cfg)
		return builder.Build()
	}

	cmd := &cobra.Command{
		Use:   "pkg-build",
		Short: "Build an incore binary package",
		Long: `Build a distributable .ipk package from a local project.

Packages the project into a deployable archive based on ipk.toml specification.

Package Naming Format:
  {name}_{version}_{os}_{arch}.ipk

  Examples:
    myapp_1.0.0_linux_amd64.ipk
    myapp_1.0.0-beta.1_linux_arm64.ipk

IPK File Format:
  - Magic Number (4 bytes): "IPK1"
  - Header Length (4 bytes): uint32, little-endian
  - Header Block (JSON): Package metadata
  - Data Block: Compressed tar archive (xz or gzip)

Spec File Search Order:
  - ./ipk.toml
  - ./.ipk/ipk.toml
  - ./misc/ipk/ipk.toml

Version Handling:
  - Without --version: uses Metadata.Version from ipk.toml
  - With --version: supports full semver (e.g., 1.0.0-beta.1+build.123)
  - With --version: if core version (X.Y.Z) is greater than metadata.version
    in ipk.toml, the file is automatically updated and saved

Build Script Template Variables:
  ${ipk_dir}             - Package source directory
  ${ipk_build_dir}       - Build temp directory
  ${ipk_name}            - Package name
  ${ipk_version}         - Version string
  ${ipk_release_version} - Release version string
  ${ipk_release_os}      - Operating system (linux, freebsd, all)
  ${ipk_release_arch}    - Architecture (amd64, arm64, src)
  ${ipk_prefix}          - Install prefix (e.g., /opt/packagename)

Environment Variables:
  IPK_DIR             - Package source directory
  IPK_BUILD_DIR       - Build temp directory
  IPK_NAME            - Package name
  IPK_VERSION         - Version string
  IPK_RELEASE_VERSION - Release version string
  IPK_RELEASE_OS      - Operating system
  IPK_RELEASE_ARCH    - Architecture
  IPK_PREFIX          - Install prefix`,
		RunE: runE,
		Example: `  # Build from current directory (uses version from ipk.toml)
  cli pkg-build

  # Build with pre-release version
  cli pkg-build --version 1.0.0-beta.1

  # Build with full semver including build metadata
  cli pkg-build --version 1.0.0-beta.1+build.123

  # Build for specific architecture
  cli pkg-build --arch arm64 --os linux

  # Build with gzip compression
  cli pkg-build --compress gzip

  # Build source package
  cli pkg-build --arch src

  # Build with custom output directory
  cli pkg-build --output /tmp/packages

  # Show build script before execution
  cli pkg-build --show-build

  # Build without compression (for debugging)
  cli pkg-build --no-compress --build-dir ./debug-build`,
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Package source directory (default: current directory)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output directory for the package")
	cmd.Flags().StringVar(&spec, "spec", "", "Spec file path (default: auto-detect)")
	cmd.Flags().StringVar(&version, "version", "", "Full version with optional pre-release/build metadata")
	cmd.Flags().StringVar(&os, "os", "linux", "Operating system (linux, freebsd, all)")
	cmd.Flags().StringVar(&arch, "arch", "amd64", "Architecture (amd64, arm64, src)")
	cmd.Flags().StringVar(&compress, "compress", "xz", "Compression format (xz or gzip)")
	cmd.Flags().BoolVar(&noCompress, "no-compress", false, "Skip compression (for debugging)")
	cmd.Flags().BoolVar(&showBuild, "show-build", false, "Show build script before execution")
	cmd.Flags().StringVar(&buildDir, "build-dir", "", "Build temp directory (for debugging)")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "Quiet mode (show errors only)")

	return cmd
}
