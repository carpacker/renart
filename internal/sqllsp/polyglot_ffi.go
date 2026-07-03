package sqllsp

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	polyglot "github.com/tobilg/polyglot/packages/go"
)

const polyglotReleaseBaseURL = "https://github.com/tobilg/polyglot/releases/download"

type PolyglotFFIOptions struct {
	CacheDir string
	Client   *http.Client
}

func OpenPolyglotClient(ctx context.Context, options PolyglotFFIOptions) (*polyglot.Client, string, error) {
	if override := strings.TrimSpace(os.Getenv(polyglot.LibraryPathEnv)); override != "" {
		client, err := polyglot.Open(override)
		return client, override, err
	}

	libraryPath, err := EnsurePolyglotFFI(ctx, options)
	if err != nil {
		return nil, "", err
	}
	client, err := polyglot.Open(libraryPath)
	if err != nil {
		return nil, libraryPath, err
	}
	return client, libraryPath, nil
}

func EnsurePolyglotFFI(ctx context.Context, options PolyglotFFIOptions) (string, error) {
	asset, libraryName, err := polyglotAssetForPlatform()
	if err != nil {
		return "", err
	}
	cacheDir := strings.TrimSpace(options.CacheDir)
	if cacheDir == "" {
		cacheDir, err = defaultPolyglotCacheDir()
		if err != nil {
			return "", err
		}
	}

	version := polyglot.Version()
	versionDir := filepath.Join(cacheDir, version, strings.TrimSuffix(strings.TrimSuffix(asset, ".tar.gz"), ".zip"))
	libraryPath := filepath.Join(versionDir, libraryName)
	if fileExists(libraryPath) {
		return libraryPath, nil
	}
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return "", err
	}

	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	archivePath := filepath.Join(cacheDir, version, asset)
	if !fileExists(archivePath) {
		if err := downloadFile(ctx, client, releaseURL(version, asset), archivePath); err != nil {
			return "", err
		}
	}
	if err := verifyReleaseChecksum(ctx, client, version, asset, archivePath); err != nil {
		_ = os.Remove(archivePath)
		return "", err
	}
	if strings.HasSuffix(asset, ".zip") {
		err = extractZipLibrary(archivePath, libraryName, versionDir)
	} else {
		err = extractTarGzLibrary(archivePath, libraryName, versionDir)
	}
	if err != nil {
		return "", err
	}
	if !fileExists(libraryPath) {
		return "", fmt.Errorf("polyglot ffi: extracted archive did not contain %s", libraryName)
	}
	return libraryPath, nil
}

func polyglotAssetForPlatform() (string, string, error) {
	var osName string
	switch runtime.GOOS {
	case "linux":
		osName = "linux"
	case "darwin":
		osName = "macos"
	case "windows":
		osName = "windows"
	default:
		return "", "", fmt.Errorf("polyglot ffi: unsupported OS %s", runtime.GOOS)
	}

	var archName string
	switch runtime.GOARCH {
	case "amd64":
		archName = "x86_64"
	case "arm64":
		archName = "aarch64"
	default:
		return "", "", fmt.Errorf("polyglot ffi: unsupported architecture %s", runtime.GOARCH)
	}
	if osName == "windows" && archName != "x86_64" {
		return "", "", errors.New("polyglot ffi: Windows arm64 release artifact is not available")
	}

	ext := ".tar.gz"
	libraryName := "libpolyglot_sql_ffi.so"
	if osName == "macos" {
		libraryName = "libpolyglot_sql_ffi.dylib"
	}
	if osName == "windows" {
		ext = ".zip"
		libraryName = "polyglot_sql_ffi.dll"
	}
	return fmt.Sprintf("polyglot-sql-ffi-%s-%s%s", osName, archName, ext), libraryName, nil
}

func defaultPolyglotCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "renart", "polyglot-sql-ffi"), nil
}

func releaseURL(version, asset string) string {
	return fmt.Sprintf("%s/v%s/%s", polyglotReleaseBaseURL, version, asset)
}

func downloadFile(ctx context.Context, client *http.Client, url, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}

	tmp := destination + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	return os.Rename(tmp, destination)
}

func verifyReleaseChecksum(ctx context.Context, client *http.Client, version, asset, archivePath string) error {
	expected, err := fetchChecksum(ctx, client, version, asset)
	if err != nil {
		return err
	}
	actual, err := sha256File(archivePath)
	if err != nil {
		return err
	}
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("polyglot ffi: checksum mismatch for %s", asset)
	}
	return nil
}

func fetchChecksum(ctx context.Context, client *http.Client, version, asset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseURL(version, "checksums.sha256"), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download checksums.sha256: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("polyglot ffi: checksum for %s not found", asset)
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func extractTarGzLibrary(archivePath, libraryName, destinationDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(header.Name) != libraryName {
			continue
		}
		return writeExtractedLibrary(reader, filepath.Join(destinationDir, libraryName))
	}
	return fmt.Errorf("polyglot ffi: %s not found in %s", libraryName, archivePath)
}

func extractZipLibrary(archivePath, libraryName, destinationDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if filepath.Base(file.Name) != libraryName {
			continue
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		defer src.Close()
		return writeExtractedLibrary(src, filepath.Join(destinationDir, libraryName))
	}
	return fmt.Errorf("polyglot ffi: %s not found in %s", libraryName, archivePath)
}

func writeExtractedLibrary(src io.Reader, destination string) error {
	tmp := destination + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destination)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
