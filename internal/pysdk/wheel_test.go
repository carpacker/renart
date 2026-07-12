package pysdk

import (
	"archive/zip"
	"strings"
	"testing"
)

func TestEnsureWheelProducesValidWheel(t *testing.T) {
	t.Setenv("RENART_PYSDK_WHEEL", "")

	path, err := EnsureWheel()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, WheelFilename()) {
		t.Fatalf("unexpected wheel path %q", path)
	}

	// Idempotent: a second call reuses the cached wheel.
	again, err := EnsureWheel()
	if err != nil || again != path {
		t.Fatalf("expected cached wheel %q, got %q (%v)", path, again, err)
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("wheel is not a readable zip: %v", err)
	}
	defer reader.Close()

	want := map[string]bool{
		"renart/__init__.py":                                 false,
		"renart/_client.py":                                  false,
		"renart/context.py":                                  false,
		"renart_sdk-" + Version + ".dist-info/METADATA":      false,
		"renart_sdk-" + Version + ".dist-info/WHEEL":         false,
		"renart_sdk-" + Version + ".dist-info/RECORD":        false,
		"renart_sdk-" + Version + ".dist-info/top_level.txt": false,
	}
	for _, file := range reader.File {
		if _, ok := want[file.Name]; ok {
			want[file.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("wheel is missing %s", name)
		}
	}
}

func TestEnsureWheelOverride(t *testing.T) {
	t.Setenv("RENART_PYSDK_WHEEL", "/custom/renart_sdk.whl")
	path, err := EnsureWheel()
	if err != nil || path != "/custom/renart_sdk.whl" {
		t.Fatalf("override must win, got %q (%v)", path, err)
	}
}
