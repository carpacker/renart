// Package pysdk embeds the renart Python SDK and packages it as a wheel on
// demand. The runner injects the wheel into every Python asset invocation via
// uv's --with flag, so the SDK version is locked to the renart binary — no
// PyPI round-trip and no version skew.
package pysdk

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed LICENSE src/renart/* stubs/*/*.pyi
var sdkSource embed.FS

// Version is the SDK's wheel version. Release builds inject the Renart release
// version with -X so the wheel embedded in the binary and the wheel published
// to PyPI always have the same version. Development builds deliberately use a
// PEP 440 development version so their cache key can never masquerade as a
// published release.
var Version = "0.0.0.dev0"

const (
	distribution       = "renart"
	wheelTag           = "py3-none-any"
	wheelDeflateLevel  = 5
	wheelArchiveFormat = "raw-deflate-v1"
	sdkLicenseFile     = "LICENSE"
)

// wheelMetadataFile is the wheel's WHEEL file.
const wheelMetadataFile = `Wheel-Version: 1.0
Generator: renart
Root-Is-Purelib: true
Tag: ` + wheelTag + `
`

// packageMetadata returns the wheel's METADATA file. pyarrow decodes the
// broker's default Arrow results; pandas remains available for explicit
// format="pandas" calls and pyarrow.Table.to_pandas().
func packageMetadata() string {
	return strings.TrimLeft(`
Metadata-Version: 2.4
Name: renart
Version: `+Version+`
Summary: Renart SDK for Python assets: query project data through the renart runner.
Home-page: https://getrenart.com
Project-URL: Documentation, https://getrenart.com/docs/asset-types/python-assets/
Project-URL: Source, https://github.com/renart-data/renart
License-Expression: Apache-2.0
License-File: `+sdkLicenseFile+`
Requires-Python: >=3.9
Requires-Dist: pyarrow>=15.0.0
Requires-Dist: pandas>=1.5
Classifier: Development Status :: 3 - Alpha
Classifier: Programming Language :: Python :: 3
Classifier: Typing :: Typed
Description-Content-Type: text/markdown

# Renart Python SDK

renart provides the Python package used by Python assets and Python
notebook cells in Renart. Renart injects its matching SDK wheel automatically
at runtime; the PyPI distribution is available for type checking, editors, and
CI environments that inspect project code outside the Renart process.

query() connects to a token-scoped broker started by the Renart runner. It
does not contain database credentials and is not a standalone database client.
`, "\n")
}

// WheelFilename is the canonical wheel file name.
func WheelFilename() string {
	return fmt.Sprintf("%s-%s-%s.whl", distribution, Version, wheelTag)
}

type wheelEntry struct {
	name    string
	content []byte
}

type compressedWheelEntry struct {
	name             string
	content          []byte
	crc32            uint32
	uncompressedSize uint64
}

// SourceFile is one embedded SDK source file. TypeStubFiles exposes the .pyi
// subset to the embedded Python language server, so editor support matches the
// exact SDK version injected into runs.
type SourceFile struct {
	Path    string
	Content string
}

// TypeStubFiles returns fresh copies of the SDK's embedded .pyi files under
// their site-packages-relative paths. Small dependency fallbacks keep the
// embedded language server useful when the runtime-only SDK dependencies have
// not also been installed into the workspace's editor environment.
func TypeStubFiles() []SourceFile {
	files := make([]SourceFile, 0, 5)
	for _, root := range []string{"src/renart", "stubs"} {
		_ = fs.WalkDir(sdkSource, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".pyi") {
				return err
			}
			content, readErr := sdkSource.ReadFile(p)
			if readErr != nil {
				return readErr
			}
			stubPath := strings.TrimPrefix(p, "stubs/")
			if root == "src/renart" {
				stubPath = path.Join("renart", strings.TrimPrefix(p, "src/renart/"))
			}
			files = append(files, SourceFile{Path: stubPath, Content: string(content)})
			return nil
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func wheelEntries() ([]wheelEntry, error) {
	entries := make([]wheelEntry, 0, 8)
	err := fs.WalkDir(sdkSource, "src/renart", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, readErr := sdkSource.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		entries = append(entries, wheelEntry{
			name:    path.Join("renart", strings.TrimPrefix(p, "src/renart/")),
			content: content,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	distInfo := distribution + "-" + Version + ".dist-info"
	license, err := sdkSource.ReadFile(sdkLicenseFile)
	if err != nil {
		return nil, fmt.Errorf("read SDK license: %w", err)
	}
	entries = append(entries,
		wheelEntry{name: distInfo + "/METADATA", content: []byte(packageMetadata())},
		wheelEntry{name: distInfo + "/WHEEL", content: []byte(wheelMetadataFile)},
		wheelEntry{name: distInfo + "/top_level.txt", content: []byte("renart\n")},
		wheelEntry{name: path.Join(distInfo, "licenses", sdkLicenseFile), content: license},
	)

	record := &strings.Builder{}
	for _, entry := range entries {
		digest := sha256.Sum256(entry.content)
		encoded := base64.RawURLEncoding.EncodeToString(digest[:])
		fmt.Fprintf(record, "%s,sha256=%s,%d\n", entry.name, encoded, len(entry.content))
	}
	fmt.Fprintf(record, "%s/RECORD,,\n", distInfo)
	entries = append(entries, wheelEntry{name: distInfo + "/RECORD", content: []byte(record.String())})
	return entries, nil
}

func compressWheelEntries(entries []wheelEntry) ([]compressedWheelEntry, error) {
	compressed := make([]compressedWheelEntry, 0, len(entries))
	for _, entry := range entries {
		var body bytes.Buffer
		// archive/zip uses Deflate level 5 by default. Keeping the level explicit
		// preserves the existing compact, deterministic wheel representation while
		// allowing us to calculate the local-header sizes before ZIP assembly.
		compressor, err := flate.NewWriter(&body, wheelDeflateLevel)
		if err != nil {
			return nil, fmt.Errorf("create compressor for %s: %w", entry.name, err)
		}
		if _, err := compressor.Write(entry.content); err != nil {
			_ = compressor.Close()
			return nil, fmt.Errorf("compress %s: %w", entry.name, err)
		}
		if err := compressor.Close(); err != nil {
			return nil, fmt.Errorf("finish compressing %s: %w", entry.name, err)
		}
		compressed = append(compressed, compressedWheelEntry{
			name:             entry.name,
			content:          body.Bytes(),
			crc32:            crc32.ChecksumIEEE(entry.content),
			uncompressedSize: uint64(len(entry.content)),
		})
	}
	return compressed, nil
}

func buildWheel(target string, entries []wheelEntry) error {
	compressedEntries, err := compressWheelEntries(entries)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".renart-*.whl")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	writer := zip.NewWriter(tmp)
	for _, entry := range compressedEntries {
		// CreateRaw writes the precomputed CRC and sizes into the local header.
		// In particular, flag 0x0008 remains clear and no trailing data descriptor
		// is emitted; Warehouse rejects descriptor-bearing wheel members.
		fileWriter, err := writer.CreateRaw(&zip.FileHeader{
			Name:               entry.name,
			Method:             zip.Deflate,
			CRC32:              entry.crc32,
			CompressedSize64:   uint64(len(entry.content)),
			UncompressedSize64: entry.uncompressedSize,
		})
		if err != nil {
			return err
		}
		if _, err := fileWriter.Write(entry.content); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// BuildWheel writes the configured SDK version into outputDir and returns the
// resulting path. Release automation uses this same assembler as EnsureWheel,
// keeping the PyPI artifact byte-identical to the SDK carried by a release
// binary built with the same Version.
func BuildWheel(outputDir string) (string, error) {
	entries, err := wheelEntries()
	if err != nil {
		return "", fmt.Errorf("failed to assemble the renart SDK wheel: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create the SDK wheel output directory: %w", err)
	}
	target := filepath.Join(outputDir, WheelFilename())
	if err := buildWheel(target, entries); err != nil {
		return "", fmt.Errorf("failed to write the renart SDK wheel: %w", err)
	}
	return target, nil
}

// EnsureWheel writes the SDK wheel into the user cache (keyed by content
// hash, so a changed SDK never reuses a stale wheel) and returns its path.
// RENART_PYSDK_WHEEL overrides the path entirely, for development and tests.
func EnsureWheel() (string, error) {
	if override := strings.TrimSpace(os.Getenv("RENART_PYSDK_WHEEL")); override != "" {
		return override, nil
	}

	entries, err := wheelEntries()
	if err != nil {
		return "", fmt.Errorf("failed to assemble the renart SDK wheel: %w", err)
	}

	hasher := sha256.New()
	// Encoding changes must invalidate previously cached wheels even when the
	// embedded SDK files are unchanged.
	hasher.Write([]byte(wheelArchiveFormat))
	for _, entry := range entries {
		hasher.Write([]byte(entry.name))
		hasher.Write(entry.content)
	}
	contentHash := hex.EncodeToString(hasher.Sum(nil))[:12]

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		cacheRoot = os.TempDir()
	}
	dir := filepath.Join(cacheRoot, "renart", "pysdk", contentHash)
	target := filepath.Join(dir, WheelFilename())
	if _, statErr := os.Stat(target); statErr == nil {
		return target, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create the SDK wheel cache: %w", err)
	}
	if err := buildWheel(target, entries); err != nil {
		return "", fmt.Errorf("failed to write the renart SDK wheel: %w", err)
	}
	return target, nil
}
