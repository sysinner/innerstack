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
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sysinner/incore/v2/inapi"
	"github.com/sysinner/incore/v2/internal/client"
)

const (
	metaDir      = ".ipk"
	metaFileName = "metadata.json"
)

func NewPkgPushCommand() *cobra.Command {

	var (
		addr      string
		chunkSize int
		overwrite bool
	)

	var runE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("package file is required")
		}

		packagePath := args[0]

		// Validate package file exists
		if _, err := os.Stat(packagePath); err != nil {
			return fmt.Errorf("package file not found: %s", packagePath)
		}

		// Read package metadata from archive
		pkg, err := readPackageMetadata(packagePath)
		if err != nil {
			return fmt.Errorf("failed to read package metadata: %w", err)
		}

		pkgId := inapi.PackageId(pkg)
		if pkgId == "" {
			return fmt.Errorf("invalid package metadata")
		}

		// Get file info
		fileInfo, err := os.Stat(packagePath)
		if err != nil {
			return fmt.Errorf("failed to get file info: %w", err)
		}
		totalSize := fileInfo.Size()

		// Calculate local SHA-256 checksum
		localChecksum, err := calculateFileChecksum(packagePath)
		if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
		}

		fmt.Printf("Package: %s\n", pkgId)
		fmt.Printf("Size: %d bytes (%.2f MB)\n", totalSize, float64(totalSize)/1024/1024)
		fmt.Printf("Checksum: %s\n", localChecksum)

		// Validate chunk size
		if chunkSize <= 0 || chunkSize > int(inapi.PackageChunkSizeDefault) {
			chunkSize = int(inapi.PackageChunkSizeDefault)
		}

		// Connect to server
		conn, err := client.Connect(addr, nil, false)
		if err != nil {
			return fmt.Errorf("failed to connect to server %s: %w", addr, err)
		}

		zc := inapi.NewZoneletClient(conn)

		// Open package file
		file, err := os.Open(packagePath)
		if err != nil {
		return fmt.Errorf("failed to open package file: %w", err)
		}
		defer file.Close()

		// Calculate total chunks
		totalChunks := int32((totalSize + int64(chunkSize) - 1) / int64(chunkSize))

		fmt.Printf("Uploading to %s (chunk size: %d bytes, total chunks: %d)\n\n",
			addr, chunkSize, totalChunks)

		// Upload chunks
		uploadedChunks := 0
		uploadedBytes := int64(0)
		buffer := make([]byte, chunkSize)

		for chunkIndex := int32(0); chunkIndex < totalChunks; chunkIndex++ {
			// Read chunk
			offset := int64(chunkIndex) * int64(chunkSize)
			_, err := file.Seek(offset, io.SeekStart)
			if err != nil {
				return fmt.Errorf("seek failed: %w", err)
			}

			chunkDataSize, err := file.Read(buffer)
			if err != nil && err != io.EOF {
				return fmt.Errorf("read failed: %w", err)
			}

			// Calculate CRC32
			crc32Val := crc32.ChecksumIEEE(buffer[:chunkDataSize])

			// Create chunk
			chunk := &inapi.PackageChunk{
				Index:  chunkIndex,
				Offset:  offset,
				Size:   int32(chunkDataSize),
				Crc32:  crc32Val,
				Data:   buffer[:chunkDataSize],
			}

			// Create request
			req := &inapi.PackagePushRequest{
				Id:         pkgId,
				Package:    pkg,
				TotalSize:  totalSize,
				Chunk:      chunk,
				Overwrite:  overwrite,
			}

			// Only send package on first chunk
			if chunkIndex > 0 {
				req.Package = nil
				req.TotalSize = 0
			}

			// Send chunk
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			resp, err := zc.PackagePush(ctx, req)
			cancel()

			if err != nil {
				return fmt.Errorf("upload failed at chunk %d: %w", chunkIndex, err)
			}

			uploadedChunks++
			uploadedBytes += int64(chunkDataSize)

			// Show progress
			progress := float64(uploadedBytes) / float64(totalSize) * 100
			fmt.Printf("\rUploading: %.1f%% (%d/%d chunks, %d/%d bytes)",
				progress, uploadedChunks, totalChunks, uploadedBytes, totalSize)

			// Check if complete
			if resp.Complete {
				fmt.Printf("\nUpload complete!\n")
				fmt.Printf("Server checksum: %s\n", resp.Checksum)

				// Verify checksum
				if resp.Checksum != localChecksum {
					fmt.Printf("WARNING: Checksum mismatch! Local: %s, Server: %s\n",
						localChecksum, resp.Checksum)
					return fmt.Errorf("checksum verification failed")
				}

				fmt.Printf("Checksum verified: OK\n")
				return nil
			}
		}

		return fmt.Errorf("upload incomplete")
	}

	cmd := &cobra.Command{
		Use:   "pkg-push <package-file>",
		Short: "Push a package to zonelet server",
		Long: `Upload a package built by pkg-build to the zonelet server.
Support chunked upload with progress tracking and checksum verification.

The package file should be a .txz or .tgz archive created by pkg-build.`,
		Args:  cobra.ExactArgs(1),
		RunE: runE,
		Example: `  # Push a package to local server
  cli pkg-push ./myapp_1.0.0_linux_amd64.txz

  # Push to remote server
  cli pkg-push ./myapp_1.0.0_linux_amd64.txz --addr 192.168.1.100:9533

  # Overwrite existing package
  cli pkg-push ./myapp_1.0.0_linux_amd64.txz --overwrite

  # Custom chunk size (1MB)
  cli pkg-push ./myapp_1.0.0_linux_amd64.txz --chunk-size 1048576`,
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", "127.0.0.1:9533", "Zonelet server address")
	cmd.Flags().IntVar(&chunkSize, "chunk-size", int(inapi.PackageChunkSizeDefault), "Chunk size in bytes")
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "o", false, "Overwrite existing package")

	return cmd
}

// readPackageMetadata extracts package metadata from the archive
func readPackageMetadata(packagePath string) (*inapi.Package, error) {
	// Open the archive
	file, err := os.Open(packagePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var tarReader *tar.Reader
	var gzipReader *gzip.Reader

	// Detect compression type
	ext := filepath.Ext(packagePath)
	switch ext {
	case ".tgz":
		gzipReader, err = gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		tarReader = tar.NewReader(gzipReader)
	case ".txz":
		// For xz, use xz command to decompress
		return readPackageMetadataXz(packagePath)
	default:
		tarReader = tar.NewReader(file)
	}

	return readMetadataFromTar(tarReader)
}

// readPackageMetadataXz reads metadata from xz-compressed archive
func readPackageMetadataXz(packagePath string) (*inapi.Package, error) {
	// Use xz command to decompress to stdout
	// First, list the contents to find metadata.json
	cmd := exec.Command("tar", "-tf", packagePath)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list archive contents: %w", err)
	}

	// Find metadata.json path
	var metaPath string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "/"+metaFileName) {
			parts := strings.Fields(line)
			if len(parts) >= 1 {
				metaPath = parts[len(parts)-1]
				break
			}
		}
	}

	if metaPath == "" {
		return nil, fmt.Errorf("metadata.json not found in archive")
	}

	// Extract metadata.json
	cmd = exec.Command("tar", "-xOf", packagePath, metaPath)
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	var pkg inapi.Package
	if err := json.Unmarshal(output, &pkg); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	return &pkg, nil
}

// readMetadataFromTar reads metadata from tar reader
func readMetadataFromTar(tarReader *tar.Reader) (*inapi.Package, error) {
	for {
		header, err := tarReader.Next()
		if err != nil {
			return nil, err
		}
		if header == nil {
			break
		}

		if strings.HasSuffix(header.Name, metaFileName) {
			// Read file content
			content, err := io.ReadAll(tarReader)
			if err != nil {
				return nil, fmt.Errorf("failed to read metadata: %w", err)
			}

			var pkg inapi.Package
			if err := json.Unmarshal(content, &pkg); err != nil {
				return nil, fmt.Errorf("failed to parse metadata: %w", err)
			}

			return &pkg, nil
		}
	}

	return nil, fmt.Errorf("metadata.json not found in archive")
}

// calculateFileChecksum calculates SHA-256 checksum of a file
func calculateFileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
