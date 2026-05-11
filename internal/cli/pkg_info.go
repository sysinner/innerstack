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
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/internal/inutil"
	"github.com/sysinner/incore/v2/internal/pkgbuild"
)

// NewPkgInfoCommand creates the "pkg-info" command for inspecting .ipk files.
// Displays package metadata and can extract the data block from the archive.
func NewPkgInfoCommand() *cobra.Command {

	var (
		showJson bool
		extract  bool
	)

	runE := func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("no package file specified")
		}

		// Process each argument (supports glob patterns like *.ipk)
		var files []string
		for _, arg := range args {
			matches, err := filepath.Glob(arg)
			if err != nil {
				return fmt.Errorf("invalid path pattern %s: %w", arg, err)
			}
			if len(matches) == 0 {
				// If no glob match, treat as literal path
				files = append(files, arg)
			} else {
				files = append(files, matches...)
			}
		}

		// Filter to only .ipk files
		var ipkFiles []string
		for _, f := range files {
			if strings.HasSuffix(strings.ToLower(f), ".ipk") {
				ipkFiles = append(ipkFiles, f)
			}
		}

		if len(ipkFiles) == 0 {
			return fmt.Errorf("no .ipk files found")
		}

		// Process each .ipk file
		for i, file := range ipkFiles {
			if i > 0 {
				fmt.Println()
			}
			if extract {
				// Extract data block to current directory
				if err := extractPkgData(file); err != nil {
					return err
				}
			} else {
				// Display package information
				if err := showPkgInfo(file, showJson); err != nil {
					return err
				}
			}
		}

		return nil
	}

	cmd := &cobra.Command{
		Use:   "pkg-info <file.ipk>...",
		Short: "Display information about .ipk package files",
		Long: `Display metadata and release information from .ipk package files.
Supports glob patterns for batch processing.

IPK File Format:
  - Magic Number (4 bytes): "IPK1"
  - Header Length (4 bytes): uint32, little-endian
  - Header Block (JSON): Package metadata and release info
  - Data Block: Compressed tar archive (xz or gzip)`,
		RunE: runE,
		Example: `  # Show info for a single package
  cli pkg-info myapp_1.0.0_linux_amd64.ipk

  # Show info for multiple packages (supports glob)
  cli pkg-info *.ipk

  # Show raw JSON header
  cli pkg-info myapp_1.0.0_linux_amd64.ipk --json

  # Extract data block to current directory
  cli pkg-info myapp.ipk --extract`,
	}

	cmd.Flags().BoolVarP(&showJson, "json", "j", false, "Output raw JSON header")
	cmd.Flags().BoolVarP(&extract, "extract", "e", false, "Extract data block to current directory")

	return cmd
}

// showPkgInfo reads and displays the metadata from an .ipk file.
// If showJson is true, outputs the raw JSON header instead of formatted tables.
// Uses streaming I/O to read only the header portion, minimizing memory usage.
func showPkgInfo(file string, showJson bool) error {
	// Open file for streaming read
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", file, err)
	}
	defer f.Close()

	// Get file size for validation
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", file, err)
	}
	fileSize := fi.Size()

	// Validate minimum file size (magic + header length = 8 bytes minimum)
	if fileSize < 8 {
		return fmt.Errorf("invalid ipk file %s: file too small", file)
	}

	// Read magic number and header length (first 8 bytes)
	headerPrefix := make([]byte, 8)
	if _, err := io.ReadFull(f, headerPrefix); err != nil {
		return fmt.Errorf("failed to read header prefix: %w", err)
	}

	// Verify magic number
	if string(headerPrefix[0:4]) != pkgbuild.PackageMagic {
		return fmt.Errorf("invalid ipk file %s: bad magic number (expected IPK1)", file)
	}

	// Read header length from bytes 4-7
	headerLen := binary.LittleEndian.Uint32(headerPrefix[4:8])
	if int64(8+headerLen) > fileSize {
		return fmt.Errorf("invalid ipk file %s: header length exceeds file size", file)
	}

	// Read only the header portion into memory
	headerData := make([]byte, headerLen)
	if _, err := io.ReadFull(f, headerData); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Parse JSON header
	var pkg inapi.Package
	if err := json.Unmarshal(headerData, &pkg); err != nil {
		return fmt.Errorf("failed to parse header: %w", err)
	}

	// Output raw JSON if requested
	if showJson {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, headerData, "", "  "); err != nil {
			return fmt.Errorf("failed to format JSON: %w", err)
		}
		fmt.Printf("%s\n", pretty.String())
		return nil
	}

	// Validate required fields
	if pkg.Metadata == nil || pkg.Release == nil {
		return fmt.Errorf("invalid package: missing metadata or release info")
	}

	// Display file name
	fmt.Printf("File: %s\n", file)

	// Display metadata section
	var tbuf bytes.Buffer
	table := tablewriter.NewTable(&tbuf)
	table.Configure(func(config *tablewriter.Config) {
		config.Header.Alignment.Global = tw.AlignLeft
	})

	fmt.Println("\n[Metadata]")
	rows := [][]any{
		{"Name", pkg.Metadata.Name},
		{"Version", pkg.Metadata.Version},
	}
	// Add optional metadata fields
	if pkg.Metadata.Description != "" {
		rows = append(rows, []any{"Description", pkg.Metadata.Description})
	}
	if len(pkg.Metadata.Authors) > 0 {
		rows = append(rows, []any{"Authors", strings.Join(pkg.Metadata.Authors, ", ")})
	}
	if pkg.Metadata.License != "" {
		rows = append(rows, []any{"License", pkg.Metadata.License})
	}
	if pkg.Metadata.Homepage != "" {
		rows = append(rows, []any{"Homepage", pkg.Metadata.Homepage})
	}
	if pkg.Metadata.Repository != "" {
		rows = append(rows, []any{"Repository", pkg.Metadata.Repository})
	}
	if len(pkg.Metadata.Keywords) > 0 {
		rows = append(rows, []any{"Keywords", strings.Join(pkg.Metadata.Keywords, ", ")})
	}
	if len(pkg.Metadata.Categories) > 0 {
		rows = append(rows, []any{"Categories", strings.Join(pkg.Metadata.Categories, ", ")})
	}

	table.Header("Field", "Value")
	for _, row := range rows {
		table.Append(row...)
	}
	table.Render()
	fmt.Print(tbuf.String())

	// Display release section
	fmt.Println("\n[Release]")
	var rbuf bytes.Buffer
	table2 := tablewriter.NewTable(&rbuf)
	table2.Configure(func(config *tablewriter.Config) {
		config.Header.Alignment.Global = tw.AlignLeft
	})

	// Format build timestamp
	builtStr := "-"
	if pkg.Release.Built > 0 {
		builtStr = time.Unix(pkg.Release.Built, 0).Format("2006-01-02 15:04:05")
	}

	// Determine compression format
	compress := pkg.Release.Compress
	if compress == "" {
		compress = "none"
	}

	table2.Header("Field", "Value")
	table2.Append("Version", pkg.Release.Version)
	table2.Append("OS", pkg.Release.Os)
	table2.Append("Arch", pkg.Release.Arch)
	table2.Append("Size", inutil.PrettyBytes(pkg.Release.Size, 1024))
	table2.Append("Built", builtStr)
	table2.Append("Compression", compress)
	if pkg.Release.Checksum != "" {
		table2.Append("Checksum", pkg.Release.Checksum)
	}
	table2.Render()
	fmt.Print(rbuf.String())

	return nil
}

// extractPkgData extracts the data block from an .ipk file to current directory.
// Creates two files:
//   - {name}_{version}_{os}_{arch}.{ext} - the compressed tar archive
//   - {name}_{version}_{os}_{arch}.json - the package metadata in JSON format
func extractPkgData(file string) error {
	// Open file for streaming read
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", file, err)
	}
	defer f.Close()

	// Get file size for validation
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", file, err)
	}
	fileSize := fi.Size()

	// Validate minimum file size
	if fileSize < 8 {
		return fmt.Errorf("invalid ipk file %s: file too small", file)
	}

	// Read magic number and header length (first 8 bytes)
	headerPrefix := make([]byte, 8)
	if _, err := io.ReadFull(f, headerPrefix); err != nil {
		return fmt.Errorf("failed to read header prefix: %w", err)
	}

	// Verify magic number
	if string(headerPrefix[0:4]) != pkgbuild.PackageMagic {
		return fmt.Errorf("invalid ipk file %s: bad magic number (expected IPK1)", file)
	}

	// Read header length
	headerLen := binary.LittleEndian.Uint32(headerPrefix[4:8])
	if int64(8+headerLen) > fileSize {
		return fmt.Errorf("invalid ipk file %s: header length exceeds file size", file)
	}

	// Read only the header portion into memory
	headerData := make([]byte, headerLen)
	if _, err := io.ReadFull(f, headerData); err != nil {
		return fmt.Errorf("failed to read header: %w", err)
	}

	// Parse header to get package info
	var pkg inapi.Package
	if err := json.Unmarshal(headerData, &pkg); err != nil {
		return fmt.Errorf("failed to parse header: %w", err)
	}

	if pkg.Metadata == nil || pkg.Release == nil {
		return fmt.Errorf("invalid package: missing metadata or release info")
	}

	// Determine file extension based on compression format
	ext := "tar"
	switch pkg.Release.Compress {
	case "xz":
		ext = "tar.xz"
	case "gzip":
		ext = "tar.gz"
	}

	// Generate base filename: {name}_{version}_{os}_{arch}
	baseName := fmt.Sprintf("%s_%s_%s_%s",
		pkg.Metadata.Name,
		pkg.Release.Version,
		pkg.Release.Os,
		pkg.Release.Arch,
	)

	// Create output file for data block
	dataPath := fmt.Sprintf("%s.%s", baseName, ext)
	outFile, err := os.Create(dataPath)
	if err != nil {
		return fmt.Errorf("failed to create data file: %w", err)
	}

	// Stream copy data block from input to output file
	// Current position is already at the start of data block (8 + headerLen)
	dataSize, err := io.Copy(outFile, f)
	outFile.Close()

	if err != nil {
		os.Remove(dataPath)
		return fmt.Errorf("failed to write data file: %w", err)
	}
	fmt.Printf("Extracted: %s (%d bytes)\n", dataPath, dataSize)

	// Write JSON metadata file
	jsonPath := fmt.Sprintf("%s.json", baseName)
	jsonData, err := json.MarshalIndent(&pkg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal json: %w", err)
	}
	if err := os.WriteFile(jsonPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write json file: %w", err)
	}
	fmt.Printf("Extracted: %s (%d bytes)\n", jsonPath, len(jsonData))

	return nil
}
