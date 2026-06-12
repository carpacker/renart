// Package snapshot stores immutable deployed versions of pipeline source
// files. Scheduled runs execute snapshots; build mode keeps running the
// working tree. Files are content-addressed in renart_blobs and described
// by a per-snapshot manifest, so unchanged files cost nothing across
// versions.
package snapshot

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Snapshot is one deployed version of a pipeline.
type Snapshot struct {
	VersionID    string            `json:"version_id"`
	PipelineUUID string            `json:"pipeline_id"`
	MerkleRoot   string            `json:"merkle_root"`
	Manifest     map[string]string `json:"manifest"` // relpath -> blob hash
	GitSHA       string            `json:"git_sha,omitempty"`
	GitDirty     bool              `json:"git_dirty"`
	CreatedAt    time.Time         `json:"created_at"`
	CreatedBy    string            `json:"created_by,omitempty"`
}

// DriftReport compares the working tree against the latest deployed
// snapshot.
type DriftReport struct {
	HasSnapshot   bool      `json:"has_snapshot"`
	InSync        bool      `json:"in_sync"`
	VersionID     string    `json:"version_id,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	ChangedFiles  []string  `json:"changed_files,omitempty"`
	AddedFiles    []string  `json:"added_files,omitempty"`
	RemovedFiles  []string  `json:"removed_files,omitempty"`
	SnapshotCount int       `json:"snapshot_count"`
}

// skipDirNames are never snapshotted (VCS internals, caches, local state).
var skipDirNames = map[string]struct{}{
	".git": {}, ".github": {}, ".vscode": {}, "node_modules": {}, "__pycache__": {},
	".venv": {}, "venv": {}, ".renart": {}, "logs": {}, ".pytest_cache": {}, ".ruff_cache": {},
}

// skipFileExtensions excludes local database artifacts that live next to
// pipelines but are data, not source.
var skipFileExtensions = map[string]struct{}{
	".db": {}, ".duckdb": {}, ".sqlite": {}, ".sqlite3": {}, ".wal": {}, ".tmp": {},
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

const timeLayout = time.RFC3339Nano

func hashBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// merkleRoot derives the snapshot identity from the sorted manifest with
// length-prefixed parts (no concatenation ambiguity).
func merkleRoot(manifest map[string]string) string {
	paths := make([]string, 0, len(manifest))
	for path := range manifest {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	hasher := sha256.New()
	writePart := func(part string) {
		var lengthBuf [8]byte
		n := len(part)
		for i := 7; i >= 0; i-- {
			lengthBuf[i] = byte(n)
			n >>= 8
		}
		hasher.Write(lengthBuf[:])
		hasher.Write([]byte(part))
	}
	for _, path := range paths {
		writePart(path)
		writePart(manifest[path])
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// CollectManifest walks the pipeline directory and returns relpath -> blob
// hash plus the file contents keyed by hash.
func CollectManifest(pipelineDir string) (map[string]string, map[string][]byte, error) {
	manifest := make(map[string]string)
	contents := make(map[string][]byte)

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
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(pipelineDir, path)
		if relErr != nil {
			return relErr
		}
		hash := hashBytes(data)
		manifest[filepath.ToSlash(rel)] = hash
		contents[hash] = data
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}
	return manifest, contents, nil
}

// Deploy snapshots the pipeline directory. When the content is identical to
// the latest snapshot it is a no-op and returns that snapshot with
// created=false.
func (s *Store) Deploy(ctx context.Context, pipelineUUID, pipelineDir, createdBy string) (Snapshot, bool, error) {
	if strings.TrimSpace(pipelineUUID) == "" {
		return Snapshot{}, false, errors.New("snapshot: pipeline UUID is required")
	}
	manifest, contents, err := CollectManifest(pipelineDir)
	if err != nil {
		return Snapshot{}, false, err
	}
	if len(manifest) == 0 {
		return Snapshot{}, false, fmt.Errorf("snapshot: no files found under %s", pipelineDir)
	}
	root := merkleRoot(manifest)

	latest, err := s.Latest(ctx, pipelineUUID)
	if err != nil {
		return Snapshot{}, false, err
	}
	if latest != nil && latest.MerkleRoot == root {
		return *latest, false, nil
	}

	gitSHA, gitDirty := gitState(pipelineDir)
	snapshot := Snapshot{
		VersionID:    uuid.NewString(),
		PipelineUUID: pipelineUUID,
		MerkleRoot:   root,
		Manifest:     manifest,
		GitSHA:       gitSHA,
		GitDirty:     gitDirty,
		CreatedAt:    time.Now().UTC(),
		CreatedBy:    createdBy,
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return Snapshot{}, false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Snapshot{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	for hash, content := range contents {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO renart_blobs (hash, content) VALUES (?, ?) ON CONFLICT (hash) DO NOTHING`,
			hash, content); err != nil {
			return Snapshot{}, false, err
		}
	}

	dirty := 0
	if snapshot.GitDirty {
		dirty = 1
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO renart_snapshots (version_id, pipeline_id, merkle_root, manifest, git_sha, git_dirty, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.VersionID, snapshot.PipelineUUID, snapshot.MerkleRoot, string(manifestJSON),
		snapshot.GitSHA, dirty, snapshot.CreatedAt.Format(timeLayout), snapshot.CreatedBy); err != nil {
		return Snapshot{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Snapshot{}, false, err
	}
	return snapshot, true, nil
}

func (s *Store) Latest(ctx context.Context, pipelineUUID string) (*Snapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT version_id, pipeline_id, merkle_root, manifest, git_sha, git_dirty, created_at, created_by
		FROM renart_snapshots WHERE pipeline_id = ?
		ORDER BY created_at DESC LIMIT 1`, pipelineUUID)
	snapshot, err := scanSnapshot(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (s *Store) Get(ctx context.Context, versionID string) (Snapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT version_id, pipeline_id, merkle_root, manifest, git_sha, git_dirty, created_at, created_by
		FROM renart_snapshots WHERE version_id = ?`, versionID)
	return scanSnapshot(row)
}

func (s *Store) List(ctx context.Context, pipelineUUID string) ([]Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT version_id, pipeline_id, merkle_root, manifest, git_sha, git_dirty, created_at, created_by
		FROM renart_snapshots WHERE pipeline_id = ?
		ORDER BY created_at DESC`, pipelineUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshots := make([]Snapshot, 0)
	for rows.Next() {
		snapshot, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSnapshot(row rowScanner) (Snapshot, error) {
	var snapshot Snapshot
	var manifestJSON, createdAt string
	var gitSHA, createdBy sql.NullString
	var gitDirty sql.NullInt64
	if err := row.Scan(&snapshot.VersionID, &snapshot.PipelineUUID, &snapshot.MerkleRoot,
		&manifestJSON, &gitSHA, &gitDirty, &createdAt, &createdBy); err != nil {
		return Snapshot{}, err
	}
	if err := json.Unmarshal([]byte(manifestJSON), &snapshot.Manifest); err != nil {
		return Snapshot{}, err
	}
	snapshot.GitSHA = gitSHA.String
	snapshot.GitDirty = gitDirty.Int64 != 0
	snapshot.CreatedBy = createdBy.String
	if parsed, err := time.Parse(timeLayout, createdAt); err == nil {
		snapshot.CreatedAt = parsed
	}
	return snapshot, nil
}

// BlobContent returns the content of one stored file.
func (s *Store) BlobContent(ctx context.Context, hash string) ([]byte, error) {
	var content []byte
	err := s.db.QueryRowContext(ctx, `SELECT content FROM renart_blobs WHERE hash = ?`, hash).Scan(&content)
	return content, err
}

// Materialize writes a snapshot's files into destDir. Simplest correct v1
// of the executor seam: files are KB-scale, extraction is microseconds; a
// virtual fs.FS over the blob table is a clean later swap.
func (s *Store) Materialize(ctx context.Context, versionID, destDir string) error {
	snapshot, err := s.Get(ctx, versionID)
	if err != nil {
		return err
	}
	for relPath, hash := range snapshot.Manifest {
		content, err := s.BlobContent(ctx, hash)
		if err != nil {
			return fmt.Errorf("snapshot %s: blob %s for %s: %w", versionID, hash, relPath, err)
		}
		target := filepath.Join(destDir, filepath.FromSlash(relPath))
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(destDir)+string(filepath.Separator)) {
			return fmt.Errorf("snapshot %s: manifest path %q escapes destination", versionID, relPath)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// MaterializeForExecution writes a snapshot into destDir and prepares the
// directory for the executor: Bruin's ingestr and Python operators locate
// the repo root by walking up to a `.git` entry (existence is all that is
// checked — no git commands run against it), and the temp directory lives
// outside the workspace repository, so a dummy `.git` directory is created
// at the root. Without it, ingestr assets fail with "no git repository
// found".
func (s *Store) MaterializeForExecution(ctx context.Context, versionID, destDir string) error {
	if err := s.Materialize(ctx, versionID, destDir); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(destDir, ".git"), 0o755)
}

// Drift compares the working tree against the latest deployed snapshot.
func (s *Store) Drift(ctx context.Context, pipelineUUID, pipelineDir string) (DriftReport, error) {
	snapshots, err := s.List(ctx, pipelineUUID)
	if err != nil {
		return DriftReport{}, err
	}
	report := DriftReport{SnapshotCount: len(snapshots)}
	if len(snapshots) == 0 {
		return report, nil
	}
	latest := snapshots[0]
	report.HasSnapshot = true
	report.VersionID = latest.VersionID
	report.CreatedAt = latest.CreatedAt

	manifest, _, err := CollectManifest(pipelineDir)
	if err != nil {
		return DriftReport{}, err
	}
	if merkleRoot(manifest) == latest.MerkleRoot {
		report.InSync = true
		return report, nil
	}

	for path, hash := range manifest {
		deployedHash, ok := latest.Manifest[path]
		switch {
		case !ok:
			report.AddedFiles = append(report.AddedFiles, path)
		case deployedHash != hash:
			report.ChangedFiles = append(report.ChangedFiles, path)
		}
	}
	for path := range latest.Manifest {
		if _, ok := manifest[path]; !ok {
			report.RemovedFiles = append(report.RemovedFiles, path)
		}
	}
	sort.Strings(report.AddedFiles)
	sort.Strings(report.ChangedFiles)
	sort.Strings(report.RemovedFiles)
	return report, nil
}

// gitState best-effort records the commit the deploy was cut from.
func gitState(dir string) (sha string, dirty bool) {
	revParse := exec.Command("git", "rev-parse", "HEAD")
	revParse.Dir = dir
	if out, err := revParse.Output(); err == nil {
		sha = strings.TrimSpace(string(out))
	}
	status := exec.Command("git", "status", "--porcelain")
	status.Dir = dir
	if out, err := status.Output(); err == nil {
		dirty = strings.TrimSpace(string(out)) != ""
	}
	return sha, dirty
}
