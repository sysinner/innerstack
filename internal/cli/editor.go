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
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

// Edit opens the user's preferred editor on a temp file pre-filled with seed
// and returns the edited content verbatim. A single trailing newline is trimmed
// to match the historical multi-line prompt behavior; everything else (inner
// newlines, \r\n, unicode, lines that equal "...") is preserved byte-for-byte.
//
// Editor selection order: $INNERSTACK_EDITOR, $EDITOR, $VISUAL, then "vi".
// The chosen variable may contain arguments (e.g. "code --wait"), which are
// interpreted via "sh -c" so multi-token editors work.
//
// suffix sets the temp file extension so editors apply syntax highlighting.
func Edit(seed string, suffix string) (string, error) {
	editor := resolveEditor()
	if editor == "" {
		return "", fmt.Errorf("no editor found (set $EDITOR or $VISUAL)")
	}

	dir, err := os.MkdirTemp("", "innerstack-edit-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "config"+suffix)
	if err := os.WriteFile(path, []byte(seed), 0600); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}

	// "sh -c" lets $EDITOR carry arguments (e.g. "code --wait", "vim -f").
	cmd := exec.Command("sh", "-c", editor+" "+shellQuote(path))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read temp file: %w", err)
	}

	// Trim exactly one trailing newline; editors commonly append one on save.
	return strings.TrimSuffix(string(data), "\n"), nil
}

// resolveEditor returns the editor command string (program + args) from the
// environment, or "vi" as a last resort.
func resolveEditor() string {
	for _, key := range []string{"INNERSTACK_EDITOR", "EDITOR", "VISUAL"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return "vi"
}

// shellQuote single-quotes a path so spaces/special chars are safe in "sh -c".
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// suffixFor maps an AppSpecConfigItem type to a temp file extension so the
// editor applies the right syntax highlighting.
func suffixFor(t string) string {
	switch t {
	case inapi.SpecFieldTypeTextJSON:
		return ".json"
	case inapi.SpecFieldTypeTextTOML:
		return ".toml"
	case inapi.SpecFieldTypeTextYAML:
		return ".yaml"
	case inapi.SpecFieldTypeTextINI:
		return ".ini"
	case inapi.SpecFieldTypeTextJavaProp:
		return ".properties"
	case inapi.SpecFieldTypeTextMarkdown:
		return ".md"
	default:
		return ".txt"
	}
}
