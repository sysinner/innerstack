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
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/pkg/inapi"
	"github.com/sysinner/incore/v2/internal/client"
	"github.com/sysinner/incore/v2/internal/pkgbuild"
)

// NewPkgPushCommand creates the "pkg-push" command for uploading packages.
// Supports chunked upload with progress tracking and CRC32 checksum verification.
func NewPkgPushCommand() *cobra.Command {

	var (
		addr      string
		overwrite bool
	)

	var runE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("package file is required")
		}

		packagePath := args[0]

		// Check if package file exists
		fileInfo, err := os.Stat(packagePath)
		if err != nil {
			return fmt.Errorf("package file not found: %s", packagePath)
		}

		// Read package metadata from .ipk file header
		pkg, err := readIPKPackage(packagePath)
		if err != nil {
			return fmt.Errorf("failed to read package: %w", err)
		}

		pkgId := inapi.PackageId(pkg)
		if pkgId == "" {
			return fmt.Errorf("invalid package metadata")
		}

		// Get total file size (entire IPK file)
		totalSize := fileInfo.Size()

		// Set file size for server-side chunk tracking
		pkg.File = &inapi.PackageFile{
			Size: totalSize,
		}

		fmt.Printf("Package: %s\n", pkgId)
		fmt.Printf("Compress: %s\n", pkg.Release.Compress)
		fmt.Printf("Data Size: %d bytes (%.2f MB)\n", pkg.Release.Size, float64(pkg.Release.Size)/1024/1024)
		fmt.Printf("Total Size: %d bytes (%.2f MB)\n", totalSize, float64(totalSize)/1024/1024)

		chunkSize := inapi.PackageFileChunkSizeDefault

		zone, err := Config.Zone(addr)
		if err != nil {
			return err
		}

		// Connect to zonelet server
		ak, err := zone.AccessKey()
		if err != nil {
			return fmt.Errorf("invalid access key: %w", err)
		}

		conn, err := client.Connect(zone.Addr, ak, false)
		if err != nil {
			return fmt.Errorf("failed to connect to server %s: %w", zone.Addr, err)
		}

		zc := inapi.NewZoneServiceClient(conn)

		// Open package file for reading
		file, err := os.Open(packagePath)
		if err != nil {
			return fmt.Errorf("failed to open package file: %w", err)
		}
		defer file.Close()

		// Calculate total number of chunks
		totalChunks := (totalSize + chunkSize - 1) / chunkSize

		fmt.Printf("Uploading to %s (chunk size: %d bytes, total chunks: %d)\n\n",
			zone.Addr, chunkSize, totalChunks)

		// Upload chunks sequentially
		uploadedChunks := 0
		uploadedBytes := int64(0)
		buffer := make([]byte, chunkSize)

		for chunkIndex := int64(0); chunkIndex < totalChunks; chunkIndex++ {
			// Calculate file offset for this chunk
			offset := chunkIndex * chunkSize

			// Seek to the correct position
			_, err := file.Seek(offset, io.SeekStart)
			if err != nil {
				return fmt.Errorf("seek failed: %w", err)
			}

			// Read chunk data
			chunkDataSize, err := file.Read(buffer)
			if err != nil && err != io.EOF {
				return fmt.Errorf("read failed: %w", err)
			}

			// Calculate CRC32 checksum for integrity verification
			crc32Val := crc32.ChecksumIEEE(buffer[:chunkDataSize])

			// Build chunk message
			chunk := &inapi.PackageFileChunk{
				Index: chunkIndex,
				Crc32: crc32Val,
				Data:  buffer[:chunkDataSize],
			}

			// Build upload request
			req := &inapi.PackagePushRequest{
				Id:        pkgId,
				Package:   pkg,
				TotalSize: totalSize,
				ChunkSize: chunkSize,
				Chunk:     chunk,
				Overwrite: overwrite,
			}

			// Only include package metadata in first chunk
			if chunkIndex > 0 {
				req.Package = nil
			}

			// Send chunk to server
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			resp, err := zc.PackagePush(ctx, req)
			cancel()

			if err != nil {
				return fmt.Errorf("upload failed at chunk %d: %w", chunkIndex, err)
			}

			uploadedChunks++
			uploadedBytes += int64(chunkDataSize)

			// Display progress
			progress := float64(uploadedBytes) / float64(totalSize) * 100
			fmt.Printf("\rUploading: %.1f%% (%d/%d chunks, %d/%d bytes)",
				progress, uploadedChunks, totalChunks, uploadedBytes, totalSize)

			// Check if upload is complete
			if resp.File != nil && resp.File.State == inapi.PackageFileStateComplete {
				fmt.Printf("\nUpload complete!\n")
				return nil
			}
		}

		return fmt.Errorf("upload incomplete")
	}

	cmd := &cobra.Command{
		Use:   "pkg-push <package-file>",
		Short: "Upload a package to zonelet server",
		Long: `Upload a .ipk package (built by pkg-build) to the zonelet server.

Supports chunked upload with progress display and CRC32 checksum verification.

File Format: .ipk archive file (generated by pkg-build command)`,
		Args: cobra.ExactArgs(1),
		RunE: runE,
		Example: `  # Upload to local server
  cli pkg-push ./myapp_1.0.0_linux_amd64.ipk

  # Upload to remote server
  cli pkg-push ./myapp_1.0.0_linux_amd64.ipk --addr 192.168.1.100:9533

  # Overwrite existing package
  cli pkg-push ./myapp_1.0.0_linux_amd64.ipk --overwrite`,
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", "", "Zonelet server address")
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "o", false, "Overwrite existing package")

	return cmd
}

// readIPKPackage reads and parses the header from an IPK file.
// Returns the Package struct containing metadata and release information.
func readIPKPackage(path string) (*inapi.Package, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Read magic number (4 bytes) - should be "IPK1"
	magic := make([]byte, 4)
	if _, err := io.ReadFull(file, magic); err != nil {
		return nil, fmt.Errorf("failed to read magic: %w", err)
	}

	if string(magic) != pkgbuild.PackageMagic {
		return nil, fmt.Errorf("invalid ipk file: magic mismatch (expected %q, got %q)",
			pkgbuild.PackageMagic, string(magic))
	}

	// Read header length (4 bytes, little-endian)
	var headerLen uint32
	if err := binary.Read(file, binary.LittleEndian, &headerLen); err != nil {
		return nil, fmt.Errorf("failed to read header length: %w", err)
	}

	// Read header block (JSON format)
	headerBytes := make([]byte, headerLen)
	if _, err := io.ReadFull(file, headerBytes); err != nil {
		return nil, fmt.Errorf("failed to read header: %w", err)
	}

	// Parse JSON header into Package struct
	var pkg inapi.Package
	if err := json.Unmarshal(headerBytes, &pkg); err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	return &pkg, nil
}
