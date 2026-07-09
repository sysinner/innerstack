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
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sysinner/innerstack/v2/internal/pkgbuild"
)

// NewPkgExportCommand creates the "pkg-export" command for extracting .ipk packages.
// It decompresses (xz/gzip) and extracts the package file tree into a directory,
// the inverse of pkg-build.
func NewPkgExportCommand() *cobra.Command {

	var (
		output string
		force  bool
	)

	runE := func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("no package file specified")
		}

		// Expand arguments, supporting glob patterns like *.ipk (same as pkg-info).
		var files []string
		for _, arg := range args {
			matches, err := filepath.Glob(arg)
			if err != nil {
				return fmt.Errorf("invalid path pattern %s: %w", arg, err)
			}
			if len(matches) == 0 {
				// No glob match: treat as a literal path.
				files = append(files, arg)
			} else {
				files = append(files, matches...)
			}
		}

		// Keep only .ipk files.
		var ipkFiles []string
		for _, f := range files {
			if strings.HasSuffix(strings.ToLower(f), ".ipk") {
				ipkFiles = append(ipkFiles, f)
			}
		}
		if len(ipkFiles) == 0 {
			return fmt.Errorf("no .ipk files found")
		}

		// An explicit --output only makes sense for a single input file.
		if output != "" && len(ipkFiles) > 1 {
			return fmt.Errorf("--output cannot be used with multiple input files")
		}

		for _, file := range ipkFiles {
			targetDir := output
			if targetDir == "" {
				// Default: the file name with the .ipk suffix stripped.
				targetDir = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
			}

			// Refuse to clobber an existing directory unless --force is set.
			if _, err := os.Stat(targetDir); err == nil {
				if !force {
					return fmt.Errorf("target directory already exists: %s (use --force to overwrite)", targetDir)
				}
				if err := os.RemoveAll(targetDir); err != nil {
					return fmt.Errorf("failed to remove existing directory %s: %w", targetDir, err)
				}
			}

			if err := pkgbuild.Extract(file, targetDir); err != nil {
				return err
			}
			fmt.Printf("Extracted: %s -> %s\n", file, targetDir)
		}

		return nil
	}

	cmd := &cobra.Command{
		Use:   "pkg-export <file.ipk>...",
		Short: "Extract a .ipk package into a directory",
		Long: `Extract the contents of one or more .ipk package files into directories.

This is the inverse of pkg-build: it decompresses (xz/gzip) and extracts the
package file tree. By default each package is extracted into a directory named
after the file (the .ipk suffix stripped).

Supports glob patterns for batch processing.

IPK File Format:
  - Magic Number (4 bytes): "IPK1"
  - Header Length (4 bytes): uint32, little-endian
  - Header Block (JSON): Package metadata
  - Data Block: Compressed tar archive (xz or gzip)`,
		RunE: runE,
		Example: `  # Extract a package into ./hooto-press_0.10.7-r2_linux_amd64
  cli pkg-export hooto-press_0.10.7-r2_linux_amd64.ipk

  # Extract into a specific directory
  cli pkg-export hooto-press_0.10.7-r2_linux_amd64.ipk -o /tmp/hooto-press

  # Overwrite an existing target directory
  cli pkg-export hooto-press_0.10.7-r2_linux_amd64.ipk --force

  # Extract multiple packages (each into its own directory)
  cli pkg-export *.ipk`,
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Target directory (default: file name without .ipk)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite the target directory if it exists")

	return cmd
}
