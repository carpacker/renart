package service

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	pypiSimpleURL   = "https://pypi.org/simple/"
	pypiIndexMaxAge = 7 * 24 * time.Hour
)

// pypiIndexClient has a generous timeout: the full simple index is tens of MB.
var pypiIndexClient = &http.Client{Timeout: 90 * time.Second}

// pypiIndexCache holds the full PyPI package-name index in memory, loaded once
// per process from an on-disk cache (downloaded from PyPI's simple index).
type pypiIndexCache struct {
	mu     sync.Mutex
	names  []string
	loaded bool
}

var pypiIndex = &pypiIndexCache{}

// WarmPyPIIndex loads the PyPI index in the background so the first dependency
// search does not pay the download cost.
func WarmPyPIIndex(ctx context.Context) {
	go func() {
		_, _ = pypiIndex.ensure(ctx)
	}()
}

func pypiIndexCachePath() string {
	base, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "renart", "pypi-simple-index.txt")
}

// ensure returns the package-name index, downloading and caching it when the
// on-disk cache is missing or stale. A stale cache is preferred over a failed
// refresh so search keeps working offline.
func (c *pypiIndexCache) ensure(ctx context.Context) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return c.names, nil
	}

	path := pypiIndexCachePath()
	if !pypiIndexFileStale(path) {
		if names, err := readPyPIIndexFile(path); err == nil && len(names) > 0 {
			c.names = names
			c.loaded = true
			return c.names, nil
		}
	}

	names, err := downloadPyPIIndex(ctx)
	if err != nil {
		if cached, cacheErr := readPyPIIndexFile(path); cacheErr == nil && len(cached) > 0 {
			c.names = cached
			c.loaded = true
			return c.names, nil
		}
		return nil, err
	}
	_ = writePyPIIndexFile(path, names)
	c.names = names
	c.loaded = true
	return c.names, nil
}

func pypiIndexFileStale(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > pypiIndexMaxAge
}

func downloadPyPIIndex(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pypiSimpleURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.pypi.simple.v1+json")
	req.Header.Set("User-Agent", "renart-notebook")
	resp, err := pypiIndexClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{status: resp.StatusCode}
	}
	var payload struct {
		Projects []struct {
			Name string `json:"name"`
		} `json:"projects"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(payload.Projects))
	for _, project := range payload.Projects {
		if name := strings.TrimSpace(project.Name); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func readPyPIIndexFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	names := make([]string, 0, 1<<19)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if name := strings.TrimSpace(scanner.Text()); name != "" {
			names = append(names, name)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

func writePyPIIndexFile(path string, names []string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)
	for _, name := range names {
		if _, err := writer.WriteString(name + "\n"); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := writer.Flush(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type httpStatusError struct{ status int }

func (e *httpStatusError) Error() string {
	return "unexpected HTTP status " + http.StatusText(e.status)
}
