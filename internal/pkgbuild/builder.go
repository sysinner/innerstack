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
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/sysinner/incore/v2/pkg/inapi"
)

const (
	metaDir      = ".ipk"
	metaFileName = "metadata.json"

	// PackageMagic is the magic number for .ipk files (4 bytes: "IPK1")
	PackageMagic = "\x49\x50\x4b\x31" // "IPK1"
)

// Config holds the build configuration
type Config struct {
	// Dir is the package source directory
	Dir string
	// OutputDir is the output directory for the package
	OutputDir string
	// SpecFile is the spec file path (optional, auto-detect if empty)
	SpecFile string
	// Version is the full version with optional pre-release and build metadata
	// If empty, uses Metadata.Version (core version only)
	// Example: "1.0.0-beta.1+build.123"
	Version string
	// Os is the operating system target (linux, freebsd, all)
	Os string
	// Arch is the architecture target (amd64, arm64, src)
	Arch string
	// Compress is the compression format (xz, gzip)
	Compress string
	// NoCompress skips final compression (for debugging)
	NoCompress bool
	// ShowBuild prints the build script before execution
	ShowBuild bool
	// BuildDir is the build temp directory (for debugging)
	BuildDir string
	// Quiet suppresses non-error output
	Quiet bool
}

// Builder handles package building
type Builder struct {
	config   Config
	spec     *inapi.PackageSpec
	specPath string
	buildDir string
	meta     *inapi.Package
	verbose  bool
}

// NewBuilder creates a new Builder instance
func NewBuilder(cfg Config) *Builder {
	return &Builder{
		config:  cfg,
		verbose: !cfg.Quiet,
	}
}

// Build executes the package build process
func (b *Builder) Build() error {

	// Step 1: Validate environment
	if err := b.validateEnv(); err != nil {
		return err
	}

	// Step 2: Setup working directory
	if err := b.setupWorkDir(); err != nil {
		return err
	}
	defer b.cleanup()

	// Step 3: Load and validate spec
	if err := b.loadSpec(); err != nil {
		return err
	}

	// Step 4: Update metadata.version if --version core is greater
	if err := b.updateSpecVersion(); err != nil {
		return err
	}

	// Step 5: Determine version info
	b.resolveVersion()

	// Step 6: Check if target already exists
	if err := b.checkExisting(); err != nil {
		return err
	}

	// Step 7: Print build info
	b.printBuildInfo()

	// Step 8: Process files (copy, minify, optimize)
	if err := b.processFiles(); err != nil {
		return err
	}

	// Step 9: Execute build script
	if err := b.runBuildScript(); err != nil {
		return err
	}

	// Step 10: Write metadata
	if err := b.writeMetadata(); err != nil {
		return err
	}

	// Step 11: Create archive
	if !b.config.NoCompress {
		if err := b.createArchive(); err != nil {
			return err
		}
	}

	return nil
}

func (b *Builder) validateEnv() error {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return fmt.Errorf("unsupported architecture: %s (requires amd64 or arm64)", runtime.GOARCH)
	}
	return nil
}

func (b *Builder) setupWorkDir() error {
	dir := b.config.Dir
	if dir == "" {
		dir = "."
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory: %w", err)
	}
	if _, err := os.Stat(absDir); err != nil {
		return fmt.Errorf("directory not found: %s", absDir)
	}
	b.config.Dir = absDir

	// Change to pack directory
	if err := os.Chdir(absDir); err != nil {
		return fmt.Errorf("failed to change directory: %w", err)
	}

	// Setup build directory
	buildDir := b.config.BuildDir
	if buildDir == "" {
		buildDir = filepath.Join(os.TempDir(), fmt.Sprintf("ipk-%d", time.Now().UnixNano()))
	}
	absBuildDir, err := filepath.Abs(buildDir)
	if err != nil {
		return fmt.Errorf("failed to resolve build directory: %w", err)
	}
	b.buildDir = absBuildDir

	// Create build directory
	if err := os.MkdirAll(b.buildDir, 0755); err != nil {
		return fmt.Errorf("failed to create build directory: %w", err)
	}

	return nil
}

func (b *Builder) loadSpec() error {
	specPath, spec, err := SpecFind(".", b.config.SpecFile)
	if err != nil {
		return fmt.Errorf("failed to find spec file: %w", err)
	}

	if err := SpecValidate(spec); err != nil {
		return fmt.Errorf("invalid spec: %w", err)
	}

	b.spec = spec
	b.specPath = specPath
	return nil
}

// metadataVersionRegexp matches a TOML version assignment line.
var metadataVersionRegexp = regexp.MustCompile(`^(\s*version\s*=\s*)"[^"]*"`)

// updateSpecVersion checks if the core version (X.Y.Z) extracted from the
// --version flag is strictly greater than metadata.version in ipk.toml.
// If so, it updates both the ipk.toml file on disk and the in-memory spec.
func (b *Builder) updateSpecVersion() error {

	if b.config.Version == "" {
		return nil
	}

	// Normalize --version for semver comparison (requires "v" prefix)
	cfgV := b.config.Version
	if !strings.HasPrefix(cfgV, "v") {
		cfgV = "v" + cfgV
	}
	if !semver.IsValid(cfgV) {
		return nil
	}

	// Extract the core version (X.Y.Z), stripping pre-release and build
	// metadata. Canonical() pads missing minor/patch and drops build metadata
	// but keeps the pre-release ("-..."), so we cut at the first "-" to obtain
	// the pure major.minor.patch used for comparison and metadata.version.
	coreVersion := semver.Canonical(cfgV)
	if i := strings.IndexByte(coreVersion, '-'); i >= 0 {
		coreVersion = coreVersion[:i]
	}

	// Normalize metadata.version for comparison
	metaV := b.spec.Metadata.Version
	if !strings.HasPrefix(metaV, "v") {
		metaV = "v" + metaV
	}

	// Only update when core version is strictly greater
	if semver.Compare(coreVersion, metaV) <= 0 {
		return nil
	}

	newVersion := strings.TrimPrefix(coreVersion, "v")

	// Read the original spec file
	content, err := os.ReadFile(b.specPath)
	if err != nil {
		return fmt.Errorf("[pkgbuild.updateSpecVersion] failed to read spec file: %w", err)
	}

	// Replace version in [metadata] section only
	updated, ok := replaceMetadataVersion(string(content), newVersion)
	if !ok {
		return fmt.Errorf("[pkgbuild.updateSpecVersion] metadata.version not found in %s", b.specPath)
	}

	// Write back
	if err := os.WriteFile(b.specPath, []byte(updated), 0644); err != nil {
		return fmt.Errorf("[pkgbuild.updateSpecVersion] failed to write spec file: %w", err)
	}

	// Update in-memory spec
	b.spec.Metadata.Version = newVersion

	if b.verbose {
		fmt.Printf("  Updated metadata.version: %s -> %s\n",
			strings.TrimPrefix(metaV, "v"), newVersion)
	}

	return nil
}

// replaceMetadataVersion replaces the version value within the [metadata]
// section of a TOML document, preserving all other content and formatting.
func replaceMetadataVersion(content, newVersion string) (string, bool) {
	lines := strings.Split(content, "\n")
	inMetadata := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track current TOML section
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inMetadata = trimmed == "[metadata]"
			continue
		}

		// Replace version only within [metadata] section
		if inMetadata {
			if m := metadataVersionRegexp.FindStringSubmatch(line); m != nil {
				lines[i] = m[1] + "\"" + newVersion + "\""
				return strings.Join(lines, "\n"), true
			}
		}
	}

	return content, false
}

func (b *Builder) resolveVersion() {
	// Determine the full version for PackageRelease.Version
	// If --version is specified, use it (can include pre-release and build metadata)
	// Otherwise, use Metadata.Version (core version only)
	version := b.spec.Metadata.Version
	if b.config.Version != "" {
		version = b.config.Version
	}

	// Determine operating system (default: linux)
	os := b.config.Os
	if os == "" {
		os = "linux"
	}

	// Determine architecture (default: amd64)
	arch := b.config.Arch
	if arch == "" {
		arch = "amd64"
	}

	// Create metadata
	// Note: Metadata.Version keeps the core version from spec
	// Release.Version contains the full version (with optional pre-release/build metadata)
	// Built timestamp will be set in createArchive() when the build is complete
	b.meta = &inapi.Package{
		Metadata: b.spec.Metadata,
		Release: &inapi.PackageRelease{
			Version: version,
			Os:      os,
			Arch:    arch,
		},
	}
}

func (b *Builder) checkExisting() error {
	if b.config.NoCompress {
		return nil
	}

	archivePath := b.targetPath() + ".ipk"

	if _, err := os.Stat(archivePath); err == nil {
		return fmt.Errorf("target package already exists: %s", archivePath)
	}

	return nil
}

func (b *Builder) printBuildInfo() {
	if !b.verbose {
		return
	}

	fmt.Printf(`
Building
  name:        %s
  version:     %s
  os:          %s
  arch:        %s
`,
		b.meta.Metadata.Name,
		b.meta.Release.Version,
		b.meta.Release.Os,
		b.meta.Release.Arch,
	)
}

func (b *Builder) processFiles() error {
	build := b.spec.Build

	// Process JavaScript files (minify)
	if len(build.MinifyJs) > 0 {
		if err := b.processFilesByPattern(build.MinifyJs, ".js", MinifyJS); err != nil {
			return fmt.Errorf("js minification failed: %w", err)
		}
	}

	// Process CSS files (minify)
	if len(build.MinifyCss) > 0 {
		if err := b.processFilesByPattern(build.MinifyCss, ".css", MinifyCSS); err != nil {
			return fmt.Errorf("css minification failed: %w", err)
		}
	}

	// Process HTML files (minify)
	if len(build.MinifyHtml) > 0 {
		if err := b.processFilesByPattern(build.MinifyHtml, ".html", MinifyHTML); err != nil {
			return fmt.Errorf("html minification failed: %w", err)
		}
		// Also process .tpl files
		if err := b.processFilesByPattern(build.MinifyHtml, ".tpl", MinifyHTML); err != nil {
			return fmt.Errorf("html minification failed: %w", err)
		}
	}

	// Process PNG files (optimize)
	if len(build.OptimizePng) > 0 {
		if err := b.processFilesByPattern(build.OptimizePng, ".png", OptimizePNG); err != nil {
			return fmt.Errorf("png optimization failed: %w", err)
		}
	}

	// Copy include files
	if len(build.Include) > 0 {
		if err := b.copyFiles(build.Include); err != nil {
			return fmt.Errorf("file copy failed: %w", err)
		}
	}

	return nil
}

// ProcessFunc is a function that processes a file
type ProcessFunc func(src, dst string) error

func (b *Builder) processFilesByPattern(patterns []string, ext string, fn ProcessFunc) error {
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}

		for _, src := range matches {
			if shouldIgnore(src) {
				continue
			}

			info, err := os.Stat(src)
			if err != nil || info.IsDir() {
				continue
			}

			if ext != "" && !strings.HasSuffix(src, ext) {
				continue
			}

			dst := filepath.Join(b.buildDir, src)
			if _, err := os.Stat(dst); err == nil {
				continue // Already processed
			}

			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				return err
			}

			if err := fn(src, dst); err != nil {
				// Fallback to copy on error
				if err := copyFile(src, dst); err != nil {
					return err
				}
				if b.verbose {
					fmt.Printf("  FILE FALLBACK %s\n", src)
				}
			} else if b.verbose {
				fmt.Printf("  FILE OK %s\n", src)
			}
		}
	}
	return nil
}

func (b *Builder) copyFiles(patterns []string) error {
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return err
		}

		for _, src := range matches {
			if shouldIgnore(src) {
				continue
			}

			info, err := os.Stat(src)
			if err != nil {
				continue
			}

			dst := filepath.Join(b.buildDir, src)
			if _, err := os.Stat(dst); err == nil {
				continue // Already exists
			}

			if info.IsDir() {
				// Copy directory recursively
				if err := b.copyDir(src, dst); err != nil {
					return err
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
					return err
				}
				if err := copyFile(src, dst); err != nil {
					return err
				}
				if b.verbose {
					fmt.Printf("  FILE OK %s\n", src)
				}
			}
		}
	}
	return nil
}

func (b *Builder) copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if shouldIgnore(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			return err
		}

		if err := copyFile(path, dstPath); err != nil {
			return err
		}

		if b.verbose {
			fmt.Printf("  FILE OK %s\n", path)
		}
		return nil
	})
}

func shouldIgnore(path string) bool {
	base := filepath.Base(path)
	ignored := []string{".git", ".gitignore", ".gitmodules", ".DS_Store", ".build_tempdir", "ipk.toml"}
	for _, i := range ignored {
		if base == i {
			return true
		}
	}
	return strings.Contains(path, "/.git/")
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func (b *Builder) runBuildScript() error {
	if b.spec.Build.Script == "" {
		return nil
	}

	// Template variables (snake_case naming with ipk_ prefix)
	vars := map[string]string{
		"ipk_dir":             b.config.Dir,
		"ipk_build_dir":       b.buildDir,
		"ipk_name":            b.meta.Metadata.Name,
		"ipk_version":         b.meta.Metadata.Version,
		"ipk_release_os":      b.meta.Release.Os,
		"ipk_release_arch":    b.meta.Release.Arch,
		"ipk_release_version": b.meta.Release.Version,
		"ipk_prefix":          "/opt/" + b.meta.Metadata.Name,
	}

	// Replace ${var} placeholders with values
	buildScript := b.spec.Build.Script
	for name, value := range vars {
		buildScript = strings.ReplaceAll(buildScript, "${"+name+"}", value)
	}

	if b.config.ShowBuild {
		fmt.Printf("\nBuild Script:\n%s\n\n", buildScript)
	}

	// Execute script
	cmd := exec.Command("sh", "-c", buildScript)
	cmd.Dir = b.config.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set environment variables
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("IPK_DIR=%s", b.config.Dir),
		fmt.Sprintf("IPK_BUILD_DIR=%s", b.buildDir),
		fmt.Sprintf("IPK_NAME=%s", b.meta.Metadata.Name),
		fmt.Sprintf("IPK_VERSION=%s", b.meta.Metadata.Version),
		fmt.Sprintf("IPK_RELEASE_OS=%s", b.meta.Release.Os),
		fmt.Sprintf("IPK_RELEASE_ARCH=%s", b.meta.Release.Arch),
		fmt.Sprintf("IPK_RELEASE_VERSION=%s", b.meta.Release.Version),
		fmt.Sprintf("IPK_PREFIX=%s", "/opt/"+b.meta.Metadata.Name),
	)

	// Add custom env from spec
	for k, v := range b.spec.Build.Env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build script failed: %w", err)
	}

	return nil
}

func (b *Builder) writeMetadata() error {
	metaDir := filepath.Join(b.buildDir, metaDir)
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		return err
	}

	metaPath := filepath.Join(metaDir, metaFileName)
	data, err := json.MarshalIndent(b.meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metaPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	return nil
}

func (b *Builder) createArchive() error {
	targetPath := b.targetPath()
	archivePath := targetPath + ".ipk"

	if b.verbose {
		fmt.Printf("\n  Creating archive: %s\n", archivePath)
	}

	// Create tar archive
	tarPath := filepath.Join(os.TempDir(), fmt.Sprintf("ipk-tar-%d", time.Now().UnixNano()))
	if err := b.createTar(tarPath); err != nil {
		return err
	}
	defer os.Remove(tarPath)

	// Compress tar
	compressedPath := filepath.Join(os.TempDir(), fmt.Sprintf("ipk-compressed-%d", time.Now().UnixNano()))
	var compressAlgo string
	switch b.config.Compress {
	case "gzip":
		if err := b.compressGzip(tarPath, compressedPath); err != nil {
			return err
		}
		compressAlgo = "gzip"
	default:
		// Use xz by default
		if err := b.compressXz(tarPath, compressedPath); err != nil {
			return err
		}
		compressAlgo = "xz"
	}
	defer os.Remove(compressedPath)

	// Read compressed data
	dataBytes, err := os.ReadFile(compressedPath)
	if err != nil {
		return fmt.Errorf("failed to read compressed data: %w", err)
	}

	// Calculate SHA-256 checksum of compressed data (format: algorithm:hash)
	checksum := "sha256:" + fmt.Sprintf("%x", sha256.Sum256(dataBytes))

	// Prepare header JSON (inapi.Package)
	pkg := &inapi.Package{
		Metadata: b.meta.Metadata,
		Release:  b.meta.Release,
	}
	pkg.Release.Size = int64(len(dataBytes))
	pkg.Release.Checksum = checksum
	pkg.Release.Compress = compressAlgo
	pkg.Release.Built = time.Now().Unix()

	headerBytes, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal header: %w", err)
	}

	// Create IPK file with new format
	if err := b.writeIPK(archivePath, headerBytes, dataBytes); err != nil {
		return err
	}

	// Get file size for info
	if info, err := os.Stat(archivePath); err == nil {
		if b.verbose {
			fmt.Printf("  OK: %s (%.2f MB)\n", archivePath, float64(info.Size())/1024/1024)
			fmt.Printf("  Checksum: %s\n", checksum)
		}
	}

	return nil
}

// writeIPK writes the IPK file with Header + Metadata + Data format
// Format:
//   - Magic Number (4 bytes): "IPK1"
//   - Header Length (4 bytes): uint32, little-endian
//   - Header Block (JSON): inapi.Package
//   - Data Block: compressed tar archive
func (b *Builder) writeIPK(path string, header, data []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Write magic number (4 bytes)
	if _, err := file.Write([]byte(PackageMagic)); err != nil {
		return fmt.Errorf("failed to write magic: %w", err)
	}

	// Write header length (4 bytes, little-endian)
	if err := binary.Write(file, binary.LittleEndian, uint32(len(header))); err != nil {
		return fmt.Errorf("failed to write header length: %w", err)
	}

	// Write header block (JSON)
	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write data block
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

func (b *Builder) createTar(tarPath string) error {
	tarFile, err := os.Create(tarPath)
	if err != nil {
		return err
	}
	defer tarFile.Close()

	tw := tar.NewWriter(tarFile)
	defer tw.Close()

	return filepath.WalkDir(b.buildDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(b.buildDir, path)
		if err != nil {
			return err
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !d.Type().IsRegular() {
			return nil
		}

		// Copy file content
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tw, file)
		return err
	})
}

func (b *Builder) compressGzip(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	gw, err := gzip.NewWriterLevel(dstFile, gzip.BestCompression)
	if err != nil {
		return err
	}
	defer gw.Close()

	_, err = io.Copy(gw, srcFile)
	return err
}

func (b *Builder) compressXz(src, dst string) error {
	// Use xz command (not available in Go stdlib)
	cmd := exec.Command("xz", "-z", "-e", "-9", "-f", "--threads=0", src)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("xz compression failed: %w", err)
	}

	// xz creates .xz file, rename to our target
	xzPath := src + ".xz"
	return os.Rename(xzPath, dst)
}

// targetPath returns the target package path without extension.
// Format: {name}_{version}_{os}_{arch}
// Example: myapp_1.0.0_linux_amd64
func (b *Builder) targetPath() string {
	name := fmt.Sprintf("%s_%s_%s_%s",
		b.meta.Metadata.Name,
		b.meta.Release.Version,
		b.meta.Release.Os,
		b.meta.Release.Arch,
	)

	outputDir := b.config.OutputDir
	if outputDir != "" {
		return filepath.Join(outputDir, name)
	}
	return name
}

func (b *Builder) cleanup() {
	if b.config.NoCompress || b.config.BuildDir != "" {
		return // Keep build dir for debugging
	}
	os.RemoveAll(b.buildDir)
}
