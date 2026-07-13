package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type countingUVEnsurer struct {
	path  string
	calls int
}

func (s *countingUVEnsurer) EnsureUvInstalled(context.Context) (string, error) {
	s.calls++
	return s.path, nil
}

func TestUVPathCacheSkipsRepeatedVersionChecks(t *testing.T) {
	uvPath := filepath.Join(t.TempDir(), "uv")
	if err := os.WriteFile(uvPath, []byte("uv"), 0o700); err != nil {
		t.Fatal(err)
	}
	checker := &countingUVEnsurer{path: uvPath}
	cache := &uvPathCache{}

	for range 2 {
		got, err := cache.ensure(context.Background(), checker)
		if err != nil {
			t.Fatal(err)
		}
		if got != uvPath {
			t.Fatalf("unexpected uv path %q", got)
		}
	}
	if checker.calls != 1 {
		t.Fatalf("expected one version check, got %d", checker.calls)
	}

	if err := os.Remove(uvPath); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.ensure(context.Background(), checker); err != nil {
		t.Fatal(err)
	}
	if checker.calls != 2 {
		t.Fatalf("expected a missing binary to invalidate the cache, got %d checks", checker.calls)
	}
}
