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
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// tarEntry describes a single entry written into a test tarball.
type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	content  string // for tar.TypeReg
	link     string // for tar.TypeSymlink
}

// buildTar builds an uncompressed tar stream from the given entries, in order.
func buildTar(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     e.mode,
			Typeflag: e.typeflag,
		}
		switch e.typeflag {
		case tar.TypeReg:
			hdr.Size = int64(len(e.content))
		case tar.TypeSymlink:
			hdr.Linkname = e.link
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%s): %v", e.name, err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.content)); err != nil {
				t.Fatalf("Write(%s): %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return buf.Bytes()
}

// writeIpk writes an .ipk file at path wrapping tarBytes with the given
// compression recorded in the JSON header.
func writeIpk(t *testing.T, path, compress string, tarBytes []byte) {
	t.Helper()

	var data bytes.Buffer
	if compress == "gzip" {
		gw := gzip.NewWriter(&data)
		if _, err := gw.Write(tarBytes); err != nil {
			t.Fatalf("gzip write: %v", err)
		}
		if err := gw.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
	} else {
		data.Write(tarBytes)
	}

	header, err := json.MarshalIndent(&inapi.Package{
		Metadata: &inapi.PackageMetadata{Name: "testapp", Version: "1.0.0"},
		Release: &inapi.PackageRelease{
			Version: "1.0.0", Os: "linux", Arch: "amd64", Compress: compress,
		},
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create ipk: %v", err)
	}
	defer f.Close()
	if _, err := f.Write([]byte(PackageMagic)); err != nil {
		t.Fatalf("write magic: %v", err)
	}
	if err := binary.Write(f, binary.LittleEndian, uint32(len(header))); err != nil {
		t.Fatalf("write header len: %v", err)
	}
	if _, err := f.Write(header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := f.Write(data.Bytes()); err != nil {
		t.Fatalf("write data: %v", err)
	}
}

func TestExtract(t *testing.T) {
	cases := []struct {
		name     string
		compress string
		entries  []tarEntry
		wantErr  string
		check    func(t *testing.T, dir string)
	}{
		{
			name:     "gzip nested dirs and files with modes",
			compress: "gzip",
			entries: []tarEntry{
				{name: "bin/", typeflag: tar.TypeDir, mode: 0755},
				{name: "bin/app", typeflag: tar.TypeReg, mode: 0755, content: "hello world"},
				{name: "etc/config.conf", typeflag: tar.TypeReg, mode: 0644, content: "key=value"},
			},
			check: func(t *testing.T, dir string) {
				got, err := os.ReadFile(filepath.Join(dir, "bin/app"))
				if err != nil {
					t.Fatalf("read bin/app: %v", err)
				}
				if string(got) != "hello world" {
					t.Errorf("bin/app = %q, want %q", got, "hello world")
				}
				if got, _ := os.ReadFile(filepath.Join(dir, "etc/config.conf")); string(got) != "key=value" {
					t.Errorf("etc/config.conf = %q, want %q", got, "key=value")
				}
				fi, err := os.Stat(filepath.Join(dir, "bin/app"))
				if err != nil {
					t.Fatalf("stat bin/app: %v", err)
				}
				if fi.Mode()&0100 == 0 {
					t.Errorf("bin/app lost executable bit: %v", fi.Mode())
				}
			},
		},
		{
			name:     "uncompressed tar",
			compress: "",
			entries: []tarEntry{
				{name: "a.txt", typeflag: tar.TypeReg, mode: 0644, content: "abc"},
			},
			check: func(t *testing.T, dir string) {
				got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
				if err != nil {
					t.Fatalf("read a.txt: %v", err)
				}
				if string(got) != "abc" {
					t.Errorf("a.txt = %q, want %q", got, "abc")
				}
			},
		},
		{
			name:     "symlink preserved",
			compress: "gzip",
			entries: []tarEntry{
				{name: "target.txt", typeflag: tar.TypeReg, mode: 0644, content: "T"},
				{name: "link.txt", typeflag: tar.TypeSymlink, mode: 0777, link: "target.txt"},
			},
			check: func(t *testing.T, dir string) {
				fi, err := os.Lstat(filepath.Join(dir, "link.txt"))
				if err != nil {
					t.Fatalf("lstat link.txt: %v", err)
				}
				if fi.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("link.txt is not a symlink: %v", fi.Mode())
				}
				dest, err := os.Readlink(filepath.Join(dir, "link.txt"))
				if err != nil {
					t.Fatalf("readlink: %v", err)
				}
				if dest != "target.txt" {
					t.Errorf("link dest = %q, want %q", dest, "target.txt")
				}
			},
		},
		{
			name:     "path traversal rejected",
			compress: "gzip",
			entries: []tarEntry{
				{name: "../escape.txt", typeflag: tar.TypeReg, mode: 0644, content: "evil"},
			},
			wantErr: "path traversal",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ipkPath := filepath.Join(t.TempDir(), "test.ipk")
			writeIpk(t, ipkPath, c.compress, buildTar(t, c.entries))

			dir := filepath.Join(t.TempDir(), "out")
			err := Extract(ipkPath, dir)

			if c.wantErr != "" {
				if err == nil {
					t.Fatalf("Extract() expected error containing %q, got nil", c.wantErr)
				}
				if !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("Extract() error = %q, want substring %q", err.Error(), c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Extract() unexpected error: %v", err)
			}
			if c.check != nil {
				c.check(t, dir)
			}
		})
	}
}

func TestExtractBadMagic(t *testing.T) {
	ipkPath := filepath.Join(t.TempDir(), "bad.ipk")
	if err := os.WriteFile(ipkPath, []byte("XXXXjunk"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	err := Extract(ipkPath, filepath.Join(t.TempDir(), "out"))
	if err == nil || !strings.Contains(err.Error(), "bad magic number") {
		t.Fatalf("Extract() error = %v, want substring %q", err, "bad magic number")
	}
}
