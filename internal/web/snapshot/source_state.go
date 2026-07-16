package snapshot

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SourceState is a cheap, content-free view of the files that participate in
// a pipeline manifest. It is a non-adversarial time-of-check guard, not a
// second source identity: a same-inode writer that deliberately restores all
// observed metadata cannot be distinguished without reading the content
// again. The canonical source identity remains the content-addressed manifest
// returned by CollectManifest.
type SourceState struct {
	files map[string]sourceFileState
}

type sourceFileState struct {
	size    int64
	mode    fs.FileMode
	modTime time.Time
	info    fs.FileInfo
}

// CollectSourceState walks the same file set as CollectManifest without
// reading file contents. Comparing a state captured before/after a content
// hash or render catches additions, removals, replacements, and ordinary
// in-place writes without hashing large source files a second time.
func CollectSourceState(pipelineDir string) (SourceState, error) {
	state := SourceState{files: make(map[string]sourceFileState)}
	walkErr := filepath.WalkDir(pipelineDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if _, skip := skipDirNames[entry.Name()]; skip && path != pipelineDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if _, skip := skipFileExtensions[strings.ToLower(filepath.Ext(path))]; skip {
			return nil
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		rel, relErr := filepath.Rel(pipelineDir, path)
		if relErr != nil {
			return relErr
		}
		state.files[filepath.ToSlash(rel)] = sourceFileState{
			size:    info.Size(),
			mode:    info.Mode(),
			modTime: info.ModTime(),
			info:    info,
		}
		return nil
	})
	if walkErr != nil {
		return SourceState{}, walkErr
	}
	return state, nil
}

// Equal reports whether two captures identify the same manifest file set and
// unchanged file metadata. os.SameFile also catches atomic replacements that
// preserve size and modification time.
func (s SourceState) Equal(other SourceState) bool {
	if len(s.files) != len(other.files) {
		return false
	}
	for path, before := range s.files {
		after, ok := other.files[path]
		if !ok || before.size != after.size || before.mode != after.mode || !before.modTime.Equal(after.modTime) {
			return false
		}
		if before.info == nil || after.info == nil || !os.SameFile(before.info, after.info) {
			return false
		}
	}
	return true
}
