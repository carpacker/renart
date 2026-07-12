// Package pysdk embeds the renart Python SDK and packages it as a wheel on
// demand. The runner injects the wheel into every Python asset invocation via
// uv's --with flag, so the SDK version is locked to the renart binary — no
// PyPI round-trip and no version skew.
package pysdk

import (
	"archive/zip"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed src/renart/*.py
var sdkSource embed.FS

// Version is the SDK's wheel version. Bump it together with user-visible SDK
// changes; the content hash in the cache path handles dev-time invalidation.
const Version = "0.1.0"

const (
	distribution = "renart_sdk"
	wheelTag     = "py3-none-any"
)

// metadata is the wheel's METADATA file. pyarrow decodes the broker's Arrow
// responses; pandas backs query()'s default return type (a hard dependency by
// decision — data code wants DataFrames).
var metadata = strings.TrimLeft(`
Metadata-Version: 2.1
Name: renart-sdk
Version: `+Version+`
Summary: Renart SDK for Python assets: query project data through the renart runner.
Requires-Python: >=3.9
Requires-Dist: pyarrow>=15.0.0
Requires-Dist: pandas>=1.5
`, "\n")

var wheelMetadata = strings.TrimLeft(`
Wheel-Version: 1.0
Generator: renart
Root-Is-Purelib: true
Tag: `+wheelTag+`
`, "\n")

// WheelFilename is the canonical wheel file name.
func WheelFilename() string {
	return fmt.Sprintf("%s-%s-%s.whl", distribution, Version, wheelTag)
}

type wheelEntry struct {
	name    string
	content []byte
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
	entries = append(entries,
		wheelEntry{name: distInfo + "/METADATA", content: []byte(metadata)},
		wheelEntry{name: distInfo + "/WHEEL", content: []byte(wheelMetadata)},
		wheelEntry{name: distInfo + "/top_level.txt", content: []byte("renart\n")},
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

func buildWheel(target string, entries []wheelEntry) error {
	tmp, err := os.CreateTemp(filepath.Dir(target), ".renart-sdk-*.whl")
	if err != nil {
		return err
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	writer := zip.NewWriter(tmp)
	for _, entry := range entries {
		// A fixed header (no timestamps) keeps the wheel byte-reproducible.
		fileWriter, err := writer.CreateHeader(&zip.FileHeader{Name: entry.name, Method: zip.Deflate})
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
