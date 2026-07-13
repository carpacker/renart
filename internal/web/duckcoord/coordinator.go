// Package duckcoord serializes access to local DuckDB database files.
//
// DuckDB supports multiple writer threads inside one process, but a local
// database file may only be opened for writing by one process at a time. Renart
// executes some assets in-process and others through child processes such as
// Sling and ingestr, so coordination has to cover both goroutines and process
// boundaries.
package duckcoord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

const defaultRetryDelay = 25 * time.Millisecond

// Owner describes the operation waiting for a database. OnWait is called at
// most once per database, and only when the lease cannot be acquired
// immediately.
type Owner struct {
	Operation string
	Pipeline  string
	Asset     string
	RunID     string
	OnWait    func(databasePath string)
}

// Options configures a Coordinator. Empty values select safe defaults shared
// by every Renart process for the current user.
type Options struct {
	LockDir    string
	RetryDelay time.Duration
}

// Coordinator grants exclusive leases for canonical local DuckDB paths.
type Coordinator struct {
	lockDir    string
	retryDelay time.Duration
}

// Lease owns one or more database locks. Release is idempotent.
type Lease struct {
	once     sync.Once
	acquired []acquiredLock
}

type acquiredLock struct {
	file  *flock.Flock
	local *localLease
}

var processLocks = localRegistry{entries: make(map[string]*localEntry)}

type localRegistry struct {
	mu      sync.Mutex
	entries map[string]*localEntry
}

type localEntry struct {
	token chan struct{}
	refs  int
}

type localLease struct {
	registry *localRegistry
	key      string
	entry    *localEntry
	once     sync.Once
}

// New creates a coordinator. Coordinators share a process-local registry even
// when separate projects are open, while the file lock covers other Renart
// processes and child processes whose lifetime is enclosed by the lease.
func New(options Options) *Coordinator {
	lockDir := strings.TrimSpace(options.LockDir)
	if lockDir == "" {
		if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
			lockDir = filepath.Join(cacheDir, "renart", "locks", "duckdb")
		} else {
			lockDir = filepath.Join(os.TempDir(), "renart-duckdb-locks")
		}
	}
	retryDelay := options.RetryDelay
	if retryDelay <= 0 {
		retryDelay = defaultRetryDelay
	}
	return &Coordinator{lockDir: lockDir, retryDelay: retryDelay}
}

// CanonicalPath resolves a DuckDB path to the stable absolute filesystem path
// used as its lock identity. It returns an empty path for in-memory and
// MotherDuck databases, which do not use a local DuckDB file.
func CanonicalPath(baseDir, rawPath string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", nil
	}
	lower := strings.ToLower(path)
	if lower == ":memory:" || strings.HasPrefix(lower, "md:") || strings.HasPrefix(lower, "motherduck:") {
		return "", nil
	}

	if queryIndex := strings.IndexByte(path, '?'); queryIndex >= 0 {
		path = path[:queryIndex]
	}
	lower = strings.ToLower(path)
	switch {
	case strings.HasPrefix(lower, "duckdb://"):
		path = path[len("duckdb://"):]
		// duckdb:///tmp/db leaves /tmp/db; duckdb://relative/db leaves
		// relative/db. A doubled leading slash is a valid UNC path and is
		// intentionally preserved by filepath.Clean below.
	case strings.HasPrefix(lower, "file://"):
		path = path[len("file://"):]
	}
	if path == "" || strings.EqualFold(path, ":memory:") {
		return "", nil
	}

	if !filepath.IsAbs(path) {
		if strings.TrimSpace(baseDir) == "" {
			baseDir = "."
		}
		path = filepath.Join(baseDir, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve DuckDB path %q: %w", rawPath, err)
	}
	absPath = filepath.Clean(absPath)

	resolved, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("resolve DuckDB path %q: %w", rawPath, err)
	}

	// The database file may not exist yet. Resolve symlinks in its nearest
	// existing ancestor so a path through a symlink and its physical path still
	// coordinate on first creation.
	ancestor := absPath
	missing := make([]string, 0, 2)
	for {
		if _, statErr := os.Lstat(ancestor); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("resolve DuckDB path %q: %w", rawPath, statErr)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		missing = append(missing, filepath.Base(ancestor))
		ancestor = parent
	}
	resolvedAncestor, resolveErr := filepath.EvalSymlinks(ancestor)
	if resolveErr != nil {
		return "", fmt.Errorf("resolve DuckDB path %q: %w", rawPath, resolveErr)
	}
	for index := len(missing) - 1; index >= 0; index-- {
		resolvedAncestor = filepath.Join(resolvedAncestor, missing[index])
	}
	return filepath.Clean(resolvedAncestor), nil
}

// Acquire waits for exclusive access to all supplied database files. Paths are
// canonicalized, deduplicated, and sorted before acquisition, preventing
// deadlocks when a load reads one DuckDB database and writes another.
func (c *Coordinator) Acquire(ctx context.Context, databasePaths []string, owner Owner) (*Lease, error) {
	if c == nil {
		return &Lease{}, nil
	}
	paths, err := canonicalSortedPaths(databasePaths)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return &Lease{}, nil
	}
	if err := os.MkdirAll(c.lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("create DuckDB lock directory: %w", err)
	}

	lease := &Lease{acquired: make([]acquiredLock, 0, len(paths))}
	for _, path := range paths {
		notified := false
		notify := func() {
			if notified || owner.OnWait == nil {
				return
			}
			notified = true
			owner.OnWait(path)
		}

		local, err := processLocks.acquire(ctx, path, notify)
		if err != nil {
			lease.Release()
			return nil, fmt.Errorf("wait for DuckDB database %q: %w", path, err)
		}

		fileLock := flock.New(c.lockPath(path))
		locked, err := fileLock.TryLock()
		if err == nil && !locked {
			notify()
			locked, err = fileLock.TryLockContext(ctx, c.retryDelay)
		}
		if err != nil || !locked {
			local.Release()
			lease.Release()
			if err == nil {
				err = ctx.Err()
			}
			if err == nil {
				err = fmt.Errorf("lock was not acquired")
			}
			return nil, fmt.Errorf("wait for DuckDB database %q: %w", path, err)
		}
		lease.acquired = append(lease.acquired, acquiredLock{file: fileLock, local: local})
	}

	return lease, nil
}

// Release relinquishes every database lock in reverse acquisition order.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		for index := len(l.acquired) - 1; index >= 0; index-- {
			_ = l.acquired[index].file.Unlock()
			l.acquired[index].local.Release()
		}
		l.acquired = nil
	})
}

func (c *Coordinator) lockPath(databasePath string) string {
	digest := sha256.Sum256([]byte(databasePath))
	return filepath.Join(c.lockDir, hex.EncodeToString(digest[:])+".lock")
}

func canonicalSortedPaths(paths []string) ([]string, error) {
	set := make(map[string]struct{}, len(paths))
	for _, rawPath := range paths {
		path, err := CanonicalPath("", rawPath)
		if err != nil {
			return nil, err
		}
		if path != "" {
			set[path] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for path := range set {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func (r *localRegistry) acquire(ctx context.Context, key string, onWait func()) (*localLease, error) {
	r.mu.Lock()
	entry := r.entries[key]
	if entry == nil {
		entry = &localEntry{token: make(chan struct{}, 1)}
		r.entries[key] = entry
	}
	entry.refs++
	r.mu.Unlock()

	select {
	case entry.token <- struct{}{}:
	default:
		if onWait != nil {
			onWait()
		}
		select {
		case entry.token <- struct{}{}:
		case <-ctx.Done():
			r.dropReference(key, entry)
			return nil, ctx.Err()
		}
	}
	return &localLease{registry: r, key: key, entry: entry}, nil
}

func (l *localLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		<-l.entry.token
		l.registry.dropReference(l.key, l.entry)
	})
}

func (r *localRegistry) dropReference(key string, entry *localEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry.refs--
	if entry.refs == 0 && r.entries[key] == entry {
		delete(r.entries, key)
	}
}
