package pysdk

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
		"renart/__init__.py":                                false,
		"renart/__init__.pyi":                               false,
		"renart/_client.py":                                 false,
		"renart/_client.pyi":                                false,
		"renart/context.py":                                 false,
		"renart/context.pyi":                                false,
		"renart/py.typed":                                   false,
		"renart-" + Version + ".dist-info/METADATA":         false,
		"renart-" + Version + ".dist-info/WHEEL":            false,
		"renart-" + Version + ".dist-info/RECORD":           false,
		"renart-" + Version + ".dist-info/top_level.txt":    false,
		"renart-" + Version + ".dist-info/licenses/LICENSE": false,
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
	if !strings.Contains(string(clientSource), `format: Literal["arrow", "pandas"] = "arrow"`) {
		t.Fatal("runtime query() must default to Arrow")
	}

	clientStub, err := fs.ReadFile(sdkSource, "src/renart/_client.pyi")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(clientStub), `format: Literal["arrow"] = "arrow"`) ||
		!strings.Contains(string(clientStub), `) -> Table: ...`) ||
		!strings.Contains(string(clientStub), `) -> DataFrame: ...`) {
		t.Fatal("query() type stub must advertise the Arrow default")
	}
}

func TestTypeStubFiles(t *testing.T) {
	files := TypeStubFiles()
	if len(files) != 5 {
		t.Fatalf("expected five SDK and fallback stub files, got %d", len(files))
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	wantPaths := []string{
		"pandas/__init__.pyi",
		"pyarrow/__init__.pyi",
		"renart/__init__.pyi",
		"renart/_client.pyi",
		"renart/context.pyi",
	}
	if strings.Join(paths, "\n") != strings.Join(wantPaths, "\n") {
		t.Fatalf("unexpected stub paths: got %q, want %q", paths, wantPaths)
	}
	for _, file := range files {
		if file.Content == "" {
			t.Fatalf("stub %s is empty", file.Path)
		}
	}
}

func TestEnsureWheelOverride(t *testing.T) {
	t.Setenv("RENART_PYSDK_WHEEL", "/custom/renart.whl")
	path, err := EnsureWheel()
	if err != nil || path != "/custom/renart.whl" {
		t.Fatalf("override must win, got %q (%v)", path, err)
	}
}

func TestBuildWheelUsesConfiguredVersionAndIsDeterministic(t *testing.T) {
	originalVersion := Version
	Version = "1.2.3"
	t.Cleanup(func() { Version = originalVersion })

	dir := t.TempDir()
	path, err := BuildWheel(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := filepath.Base(path), "renart-1.2.3-py3-none-any.whl"; got != want {
		t.Fatalf("unexpected wheel filename %q, want %q", got, want)
	}

	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	metadata := readWheelFile(t, path, "renart-1.2.3.dist-info/METADATA")
	if !strings.Contains(metadata, "Name: renart\nVersion: 1.2.3\n") {
		t.Fatalf("wheel metadata does not contain the configured version:\n%s", metadata)
	}
	for _, field := range []string{
		"Metadata-Version: 2.4\n",
		"License-Expression: Apache-2.0\n",
		"License-File: LICENSE\n",
		"Classifier: Development Status :: 3 - Alpha\n",
		"Classifier: Typing :: Typed\n",
		"Requires-Dist: pandas>=1.5\n",
	} {
		if !strings.Contains(metadata, field) {
			t.Errorf("wheel metadata is missing %q:\n%s", field, metadata)
		}
	}
	license := readWheelFile(t, path, "renart-1.2.3.dist-info/licenses/LICENSE")
	if !strings.Contains(license, "Apache License") || !strings.Contains(license, "Version 2.0") {
		t.Fatal("wheel does not contain the Apache-2.0 license")
	}

	path, err = BuildWheel(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("building the same SDK version twice must produce identical wheel bytes")
	}
}

func TestBuildWheelLicenseMetadataMatchesArchive(t *testing.T) {
	wheelPath, err := BuildWheel(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	distInfo := distribution + "-" + Version + ".dist-info"
	metadata := readWheelFile(t, wheelPath, distInfo+"/METADATA")
	licenseFiles := make([]string, 0, 1)
	for _, line := range strings.Split(metadata, "\n") {
		if value, ok := strings.CutPrefix(line, "License-File: "); ok {
			licenseFiles = append(licenseFiles, strings.TrimSpace(value))
		}
	}
	if len(licenseFiles) == 0 {
		t.Fatal("wheel metadata declares no License-File")
	}

	reader, err := zip.OpenReader(wheelPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	members := make(map[string]struct{}, len(reader.File))
	for _, file := range reader.File {
		members[file.Name] = struct{}{}
	}
	for _, licenseFile := range licenseFiles {
		if !fs.ValidPath(licenseFile) {
			t.Errorf("License-File %q is not a valid relative path", licenseFile)
			continue
		}
		member := path.Join(distInfo, "licenses", licenseFile)
		if _, ok := members[member]; !ok {
			t.Errorf("License-File %q requires missing wheel member %q", licenseFile, member)
		}
	}
}

func TestBuildWheelUsesDescriptorFreeLocalHeaders(t *testing.T) {
	wheelPath, err := BuildWheel(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wheelBytes, err := os.ReadFile(wheelPath)
	if err != nil {
		t.Fatal(err)
	}
	localHeaders := readDescriptorFreeLocalZIPHeaders(t, wheelBytes)

	reader, err := zip.OpenReader(wheelPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(localHeaders) != len(reader.File) {
		t.Fatalf("read %d local headers for %d wheel members", len(localHeaders), len(reader.File))
	}
	for _, file := range reader.File {
		local, ok := localHeaders[file.Name]
		if !ok {
			t.Errorf("wheel member %s has no local header", file.Name)
			continue
		}
		if local.crc32 != file.CRC32 ||
			uint64(local.compressedSize) != file.CompressedSize64 ||
			uint64(local.uncompressedSize) != file.UncompressedSize64 {
			t.Errorf(
				"wheel member %s local header does not contain its final CRC and sizes",
				file.Name,
			)
		}
	}
}

type localZIPHeader struct {
	crc32            uint32
	compressedSize   uint32
	uncompressedSize uint32
}

func readDescriptorFreeLocalZIPHeaders(t *testing.T, archive []byte) map[string]localZIPHeader {
	t.Helper()
	const (
		localFileHeaderSignature  = 0x04034b50
		centralDirectorySignature = 0x02014b50
		localFileHeaderSize       = 30
		dataDescriptorFlag        = 1 << 3
	)

	headers := make(map[string]localZIPHeader)
	for offset := 0; ; {
		if len(archive)-offset < 4 {
			t.Fatalf("ZIP ended before its central directory at offset %d", offset)
		}
		signature := binary.LittleEndian.Uint32(archive[offset:])
		if signature == centralDirectorySignature {
			return headers
		}
		if signature != localFileHeaderSignature {
			t.Fatalf("unexpected ZIP signature 0x%08x at offset %d", signature, offset)
		}
		if len(archive)-offset < localFileHeaderSize {
			t.Fatalf("truncated local ZIP header at offset %d", offset)
		}

		flags := binary.LittleEndian.Uint16(archive[offset+6:])
		nameLength := int(binary.LittleEndian.Uint16(archive[offset+26:]))
		extraLength := int(binary.LittleEndian.Uint16(archive[offset+28:]))
		nameStart := offset + localFileHeaderSize
		dataStart := nameStart + nameLength + extraLength
		if dataStart > len(archive) {
			t.Fatalf("truncated local ZIP header name at offset %d", offset)
		}
		name := string(archive[nameStart : nameStart+nameLength])
		if flags&dataDescriptorFlag != 0 {
			t.Fatalf("wheel member %s uses a ZIP data descriptor (flags 0x%04x)", name, flags)
		}

		header := localZIPHeader{
			crc32:            binary.LittleEndian.Uint32(archive[offset+14:]),
			compressedSize:   binary.LittleEndian.Uint32(archive[offset+18:]),
			uncompressedSize: binary.LittleEndian.Uint32(archive[offset+22:]),
		}
		if _, exists := headers[name]; exists {
			t.Fatalf("wheel contains duplicate local header for %s", name)
		}
		headers[name] = header
		offset = dataStart + int(header.compressedSize)
		if offset > len(archive) {
			t.Fatalf("compressed data for %s extends past the ZIP archive", name)
		}
	}
}

func readWheelFile(t *testing.T, wheelPath, name string) string {
	t.Helper()
	reader, err := zip.OpenReader(wheelPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		contents, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(contents)
		closeErr := contents.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		return string(data)
	}
	t.Fatalf("wheel is missing %s", name)
	return ""
}
