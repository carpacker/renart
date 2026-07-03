package sqllsp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	polyglot "github.com/tobilg/polyglot/packages/go"
)

func TestPolyglotAssetForPlatformCurrentPlatform(t *testing.T) {
	asset, library, err := polyglotAssetForPlatform()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(asset, "polyglot-sql-ffi-") {
		t.Fatalf("unexpected asset name %q", asset)
	}
	if library == "" {
		t.Fatal("expected library name")
	}
}

func TestEnsurePolyglotFFIUsesCachedLibrary(t *testing.T) {
	asset, library, err := polyglotAssetForPlatform()
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := t.TempDir()
	versionDir := filepath.Join(cacheDir, polyglot.Version(), strings.TrimSuffix(strings.TrimSuffix(asset, ".tar.gz"), ".zip"))
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(versionDir, library)
	if err := os.WriteFile(expected, []byte("cached"), 0o755); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected network request to %s", r.URL.String())
	}))
	defer server.Close()

	got, err := EnsurePolyglotFFI(context.Background(), PolyglotFFIOptions{
		CacheDir: cacheDir,
		Client:   server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}
