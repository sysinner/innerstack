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
	"archive/tar"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// Extract reads an .ipk package file and writes its contents into targetDir.
//
// IPK file format (mirrors Builder.writeIPK):
//   - Magic Number (4 bytes): "IPK1"
//   - Header Length (4 bytes): uint32, little-endian
//   - Header Block (JSON): inapi.Package
//   - Data Block: compressed tarball (xz/gzip/none)
//
// The compression format is taken from the header's Release.Compress. Directory
// modes, regular files and symlinks are preserved. Entries that escape targetDir
// (path traversal) are rejected.
func Extract(ipkPath, targetDir string) error {
	f, err := os.Open(ipkPath)
	if err != nil {
		return fmt.Errorf("[pkgbuild.Extract] failed to open %s: %w", ipkPath, err)
	}
	defer f.Close()

	// Read and verify the magic number.
	magicBuf := make([]byte, 4)
	if _, err := io.ReadFull(f, magicBuf); err != nil {
		return fmt.Errorf("[pkgbuild.Extract] failed to read magic number: %w", err)
	}
	if string(magicBuf) != PackageMagic {
		return fmt.Errorf("[pkgbuild.Extract] invalid ipk file %s: bad magic number (expected IPK1)", ipkPath)
	}

	// Read the header length.
	var headerLen uint32
	if err := binary.Read(f, binary.LittleEndian, &headerLen); err != nil {
		return fmt.Errorf("[pkgbuild.Extract] failed to read header length: %w", err)
	}

	// Read the JSON header block.
	headerBuf := make([]byte, headerLen)
	if _, err := io.ReadFull(f, headerBuf); err != nil {
		return fmt.Errorf("[pkgbuild.Extract] failed to read header block: %w", err)
	}

	var pkg inapi.Package
	if err := json.Unmarshal(headerBuf, &pkg); err != nil {
		return fmt.Errorf("[pkgbuild.Extract] failed to parse header block: %w", err)
	}

	// The remainder of the file is the (compressed) tarball.
	compress := ""
	if pkg.Release != nil {
		compress = pkg.Release.Compress
	}

	var tr *tar.Reader
	switch compress {
	case "gzip":
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("[pkgbuild.Extract] failed to create gzip reader: %w", err)
		}
		defer gzr.Close()
		tr = tar.NewReader(gzr)

	case "xz":
		xzr, err := xz.NewReader(f)
		if err != nil {
			return fmt.Errorf("[pkgbuild.Extract] failed to create xz reader: %w", err)
		}
		tr = tar.NewReader(xzr)

	default:
		// Uncompressed tar.
		tr = tar.NewReader(f)
	}

	// Create the target directory.
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("[pkgbuild.Extract] failed to resolve target directory: %w", err)
	}
	if err := os.MkdirAll(absTarget, 0755); err != nil {
		return fmt.Errorf("[pkgbuild.Extract] failed to create target directory: %w", err)
	}
	sep := string(os.PathSeparator)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("[pkgbuild.Extract] failed to read tar header: %w", err)
		}

		// Security: prevent path traversal outside the target directory.
		targetPath := filepath.Join(absTarget, header.Name)
		if !strings.HasPrefix(targetPath, absTarget+sep) && targetPath != absTarget {
			return fmt.Errorf("[pkgbuild.Extract] path traversal detected: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("[pkgbuild.Extract] failed to create directory %s: %w", targetPath, err)
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("[pkgbuild.Extract] failed to create parent directory for %s: %w", targetPath, err)
			}
			outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("[pkgbuild.Extract] failed to create file %s: %w", targetPath, err)
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return fmt.Errorf("[pkgbuild.Extract] failed to write file %s: %w", targetPath, err)
			}
			outFile.Close()

		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return fmt.Errorf("[pkgbuild.Extract] failed to create parent directory for %s: %w", targetPath, err)
			}
			if err := os.Symlink(header.Linkname, targetPath); err != nil {
				return fmt.Errorf("[pkgbuild.Extract] failed to create symlink %s: %w", targetPath, err)
			}
		}
	}

	return nil
}
