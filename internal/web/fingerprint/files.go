package fingerprint

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

// lockfileNames are checked nearest-first from the asset's directory up to
// the pipeline root; the first hit wins (mirrors how Bruin resolves Python
// dependencies).
var lockfileNames = []string{"uv.lock", "requirements.txt"}

// sharedDirName is the designated shared-code directory under the pipeline
// root. One coarse hash covers it: any change there invalidates every Python
// asset in the pipeline (safety net until Phase 7 import resolution).
const sharedDirName = "shared"

func (e *Engine) pythonLockfileHash(asset *pipeline.Asset, pipelineDir string) (string, error) {
	startDir := filepath.Dir(asset.ExecutableFile.Path)
	if asset.ExecutableFile.Path == "" {
		startDir = pipelineDir
	}
	dir := startDir
	for {
		for _, name := range lockfileNames {
			candidate := filepath.Join(dir, name)
			hash, ok, err := e.hashFileIfExists(candidate)
			if err != nil {
				return "", err
			}
			if ok {
				return hashHex("lockfile", name, hash), nil
			}
		}
		if dir == pipelineDir || !strings.HasPrefix(dir, pipelineDir) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return hashHex("lockfile", "none"), nil
}

func (e *Engine) sharedDirHash(pipelineDir string) (string, error) {
	return e.hashDir(filepath.Join(pipelineDir, sharedDirName))
}

// dependsOnFilesHash hashes files pinned via the meta.depends_on_files
// escape hatch: comma-separated globs relative to the pipeline directory.
func (e *Engine) dependsOnFilesHash(asset *pipeline.Asset, pipelineDir string) (string, error) {
	raw := strings.TrimSpace(asset.Meta["depends_on_files"])
	if raw == "" {
		return hashHex("deps", "none"), nil
	}

	matched := make([]string, 0)
	for _, glob := range strings.Split(raw, ",") {
		glob = strings.TrimSpace(glob)
		if glob == "" {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(pipelineDir, filepath.FromSlash(glob)))
		if err != nil {
			return "", err
		}
		matched = append(matched, matches...)
	}
	sort.Strings(matched)

	parts := []string{"deps"}
	for _, path := range matched {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		hash, ok, err := e.hashFileIfExists(path)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		rel, relErr := filepath.Rel(pipelineDir, path)
		if relErr != nil {
			rel = path
		}
		parts = append(parts, filepath.ToSlash(rel), hash)
	}
	return hashHex(parts...), nil
}

// hashDir hashes a directory tree as sorted (relative path, content hash)
// pairs. A missing directory hashes to a stable "absent" value.
func (e *Engine) hashDir(dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return hashHex("dir", "absent"), nil //nolint:nilerr // absent is a valid state
	}

	parts := []string{"dir"}
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		hash, ok, hashErr := e.hashFileIfExists(path)
		if hashErr != nil {
			return hashErr
		}
		if !ok {
			return nil
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			rel = path
		}
		parts = append(parts, filepath.ToSlash(rel), hash)
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	// WalkDir is lexical, but normalize separators before relying on order.
	return hashHex(parts...), nil
}

// hashFileIfExists hashes one file's contents through the stat-validated
// cache. ok is false when the file does not exist.
func (e *Engine) hashFileIfExists(path string) (hash string, ok bool, err error) {
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "", false, nil
		}
		return "", false, statErr
	}
	if info.IsDir() {
		return "", false, nil
	}

	e.mu.Lock()
	entry, cached := e.fileHashes[path]
	e.mu.Unlock()
	if cached && entry.modTime.Equal(info.ModTime()) && entry.size == info.Size() {
		return entry.hash, true, nil
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return "", false, readErr
	}
	hash = hashHex("file", string(data))

	e.mu.Lock()
	e.fileHashes[path] = fileHashEntry{modTime: info.ModTime(), size: info.Size(), hash: hash}
	e.mu.Unlock()
	return hash, true, nil
}
