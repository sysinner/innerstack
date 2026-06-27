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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sysinner/innerstack/v2/pkg/inapi"
)

func TestSuffixFor(t *testing.T) {
	cases := map[string]string{
		inapi.SpecFieldTypeTextJSON:     ".json",
		inapi.SpecFieldTypeTextTOML:     ".toml",
		inapi.SpecFieldTypeTextYAML:     ".yaml",
		inapi.SpecFieldTypeTextINI:      ".ini",
		inapi.SpecFieldTypeTextJavaProp: ".properties",
		inapi.SpecFieldTypeTextMarkdown: ".md",
		inapi.SpecFieldTypeText:         ".txt",
		"unknown":                       ".txt",
		"":                              ".txt",
	}
	for in, want := range cases {
		if got := suffixFor(in); got != want {
			t.Errorf("suffixFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveEditor(t *testing.T) {
	keys := []string{"INNERSTACK_EDITOR", "EDITOR", "VISUAL"}
	save := map[string]string{}
	for _, k := range keys {
		save[k] = os.Getenv(k)
		os.Unsetenv(k)
	}
	defer func() {
		for k, v := range save {
			os.Setenv(k, v)
		}
	}()

	if got := resolveEditor(); got != "vi" {
		t.Errorf("default editor = %q, want vi", got)
	}

	os.Setenv("EDITOR", "nano")
	if got := resolveEditor(); got != "nano" {
		t.Errorf("EDITOR=nano -> %q, want nano", got)
	}

	// VISUAL must not override EDITOR.
	os.Setenv("VISUAL", "emacs")
	if got := resolveEditor(); got != "nano" {
		t.Errorf("VISUAL should not override EDITOR -> %q, want nano", got)
	}

	os.Unsetenv("EDITOR")
	if got := resolveEditor(); got != "emacs" {
		t.Errorf("VISUAL fallback -> %q, want emacs", got)
	}

	os.Setenv("INNERSTACK_EDITOR", "code --wait")
	if got := resolveEditor(); got != "code --wait" {
		t.Errorf("INNERSTACK_EDITOR priority -> %q, want 'code --wait'", got)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/a b/c"); got != "'/a b/c'" {
		t.Errorf("shellQuote(/a b/c) = %q", got)
	}
	// Embedded single quote must be escaped and result stays a single shell token.
	got := shellQuote("it's")
	if !strings.HasPrefix(got, "'") || !strings.HasSuffix(got, "'") || !strings.Contains(got, `'\''`) {
		t.Errorf("shellQuote(it's) = %q, expected escaped single quotes", got)
	}
}

// fakeEditor writes a small script that overwrites $1 with fixed content,
// simulating a user editing the temp file. Verifies the read-back path
// preserves inner newlines, a line equal to "...", and trims one trailing \n.
func TestEditRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based editor test")
	}
	script := writeFakeEditor(t, `#!/bin/sh
printf 'line1\nline2\n... is kept\ntrailing-cr\r\n' > "$1"
`)

	withEditor(t, script)
	want := "line1\nline2\n... is kept\ntrailing-cr\r" // one trailing \n trimmed

	got, err := Edit("ignored-seed", ".yaml")
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if got != want {
		t.Errorf("Edit round-trip mismatch:\n got  = %q\n want = %q", got, want)
	}
}

// A no-op editor must leave the seed content untouched (apart from the trailing
// newline trim, which is a no-op when seed has none).
func TestEditNoOpPreservesSeed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based editor test")
	}
	script := writeFakeEditor(t, "#!/bin/sh\nexit 0\n")
	withEditor(t, script)

	seed := "alpha\nbeta\ngamma"
	got, err := Edit(seed, ".toml")
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if got != seed {
		t.Errorf("no-op edit changed content:\n got  = %q\n want = %q", got, seed)
	}
}

// An editor that exits non-zero must surface as an error.
func TestEditEditorFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based editor test")
	}
	script := writeFakeEditor(t, "#!/bin/sh\nexit 3\n")
	withEditor(t, script)

	if _, err := Edit("seed", ".txt"); err == nil {
		t.Fatal("expected error from failing editor, got nil")
	}
}

func writeFakeEditor(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fakeeditor")
	if err := os.WriteFile(path, []byte(body), 0755); err != nil {
		t.Fatalf("write fake editor: %v", err)
	}
	return path
}

func withEditor(t *testing.T, editor string) {
	t.Helper()
	prev := os.Getenv("EDITOR")
	os.Setenv("EDITOR", editor)
	// INNERSTACK_EDITOR/VISUAL would shadow EDITOR; clear them for the test.
	prevIE := os.Getenv("INNERSTACK_EDITOR")
	prevV := os.Getenv("VISUAL")
	os.Unsetenv("INNERSTACK_EDITOR")
	os.Unsetenv("VISUAL")
	t.Cleanup(func() {
		os.Setenv("EDITOR", prev)
		os.Setenv("INNERSTACK_EDITOR", prevIE)
		os.Setenv("VISUAL", prevV)
	})
}
