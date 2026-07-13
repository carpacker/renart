package pysdk

import (
	"archive/zip"
	"io/fs"
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
		"renart/__init__.pyi":                                false,
		"renart/_client.py":                                  false,
		"renart/_client.pyi":                                 false,
		"renart/context.py":                                  false,
		"renart/context.pyi":                                 false,
		"renart/py.typed":                                    false,
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

func TestQueryDefaultsToArrow(t *testing.T) {
	clientSource, err := fs.ReadFile(sdkSource, "src/renart/_client.py")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(clientSource), `format: str = "arrow"`) {
		t.Fatal("runtime query() must default to Arrow")
	}

	clientStub, err := fs.ReadFile(sdkSource, "src/renart/_client.pyi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(clientStub), `format: Literal["arrow", "pandas"] = "arrow"`) {
		t.Fatal("query() type stub must advertise the Arrow default")
	}
}

func TestTypeStubFiles(t *testing.T) {
	files := TypeStubFiles()
	if len(files) != 3 {
		t.Fatalf("expected three SDK stub files, got %d", len(files))
	}
	if files[0].Path != "renart/__init__.pyi" {
		t.Fatalf("stub files must be sorted and package-relative, got %q", files[0].Path)
	}
	for _, file := range files {
		if file.Content == "" {
			t.Fatalf("stub %s is empty", file.Path)
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
