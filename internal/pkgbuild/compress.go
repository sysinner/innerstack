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
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
)

// MinifyJS minifies a JavaScript file
func MinifyJS(src, dst string) error {
	return minifyFile(src, dst, "text/javascript", func(m *minify.M, w io.Writer, r io.Reader) error {
		return js.Minify(m, w, r, nil)
	})
}

// MinifyCSS minifies a CSS file
func MinifyCSS(src, dst string) error {
	return minifyFile(src, dst, "text/css", func(m *minify.M, w io.Writer, r io.Reader) error {
		return css.Minify(m, w, r, nil)
	})
}

// MinifyHTML minifies an HTML file
func MinifyHTML(src, dst string) error {
	return minifyFile(src, dst, "text/html", func(m *minify.M, w io.Writer, r io.Reader) error {
		return html.Minify(m, w, r, nil)
	})
}

type minifyFunc func(m *minify.M, w io.Writer, r io.Reader) error

func minifyFile(src, dst, mediatype string, fn minifyFunc) error {
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	m := minify.New()
	var buf bytes.Buffer

	if err := fn(m, &buf, bytes.NewReader(content)); err != nil {
		return err
	}

	// Get original file mode
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, buf.Bytes(), info.Mode())
}

// OptimizePNG optimizes a PNG file using optipng
func OptimizePNG(src, dst string) error {
	// Check if optipng is available
	if _, err := exec.LookPath("optipng"); err != nil {
		// Fallback to copy if optipng not available
		return copyFile(src, dst)
	}

	cmd := exec.Command("optipng", "-o7", "-quiet", "-out", dst, src)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("optipng failed: %w, output: %s", err, string(output))
	}

	// Preserve original file mode
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}
