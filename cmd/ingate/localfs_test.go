package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalfsServe verifies localfsServe serves files under the route root
// but never resolves a path outside it: a symlink planted inside the root
// (rootDir may be writable by other processes) must not turn ingate into an
// arbitrary file reader.
func TestLocalfsServe(t *testing.T) {
	// A file outside rootDir: the payload an escape attempt would leak.
	outsideDir := t.TempDir()
	secretPath := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("top secret"), 0644); err != nil {
		t.Fatal(err)
	}

	rootDir := t.TempDir()
	os.WriteFile(filepath.Join(rootDir, "hello.txt"), []byte("hello world"), 0644)
	os.MkdirAll(filepath.Join(rootDir, "sub"), 0755)
	os.WriteFile(filepath.Join(rootDir, "sub", "nested.txt"), []byte("nested"), 0644)
	// A relative symlink staying inside the root.
	os.MkdirAll(filepath.Join(rootDir, "alias"), 0755)
	os.WriteFile(filepath.Join(rootDir, "real.txt"), []byte("aliased"), 0644)
	os.Symlink(filepath.Join("..", "real.txt"), filepath.Join(rootDir, "alias", "link.txt"))
	// Symlinks whose targets escape the root: direct file, via directory,
	// and an absolute-target chain.
	os.Symlink(secretPath, filepath.Join(rootDir, "leak.txt"))
	os.Symlink(outsideDir, filepath.Join(rootDir, "escape-dir"))
	os.Symlink(string(filepath.Separator), filepath.Join(rootDir, "root-link"))

	stubDomainRoute(t, &DomainEntryRoute{
		Type: "localfs",
		Path: "/static",
		Urls: []*url.URL{{Path: rootDir}},
	})

	// Cases are fed to localfsServe directly with the raw (uncleaned) path
	// so the lexical guard is exercised; rootHandler cleans first.
	tests := []struct {
		name     string
		urlPath  string
		wantCode int
		wantBody string
	}{
		{"plain file", "/static/hello.txt", http.StatusOK, "hello world"},
		{"nested file", "/static/sub/nested.txt", http.StatusOK, "nested"},
		{"missing file", "/static/nope.txt", http.StatusNotFound, ""},
		{"root itself is a dir", "/static", http.StatusForbidden, ""},
		{"trailing slash is a dir", "/static/", http.StatusForbidden, ""},
		{"subdir is a dir", "/static/sub", http.StatusForbidden, ""},
		// Uncleaned traversal still resolving to the route (lookup's parent
		// walk cleans "/static/../static" back to "/static"): the lexical
		// guard must reject it before os.Root is consulted.
		{"lexical traversal", "/static/../static/hello.txt", http.StatusForbidden, ""},
		{"absolute path", "/static//etc/passwd", http.StatusForbidden, ""},
		// os.Root resolves a relative symlink that stays inside the root,
		// but refuses absolute targets outright, even when they point back
		// inside the root.
		{"relative symlink within root", "/static/alias/link.txt", http.StatusOK, "aliased"},
		{"symlink escaping root (file)", "/static/leak.txt", http.StatusNotFound, ""},
		{"symlink escaping root (dir)", "/static/escape-dir/secret.txt", http.StatusNotFound, ""},
		{"symlink to filesystem root", "/static/root-link/etc/passwd", http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://example.com"+tt.urlPath, nil)
			rec := httptest.NewRecorder()

			if !localfsServe(rec, r, tt.urlPath) {
				t.Fatal("localfsServe returned false, want the localfs route to handle the request")
			}
			if rec.Code != tt.wantCode {
				t.Fatalf("code = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}
