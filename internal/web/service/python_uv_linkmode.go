package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type uvFilesystemComparator func(first, second string) (bool, error)

// configureUVLinkMode avoids making uv attempt an impossible cache hardlink
// when a project environment and its cache live on different filesystems. uv
// already falls back to a copy in that case; selecting copy up front removes
// the warning and failed-link attempt without changing the installed bytes.
// Explicit environment or discovered uv configuration always wins.
func configureUVLinkMode(envVariables map[string]string, projectRoot, repoRoot string) {
	configureUVLinkModeWithComparator(envVariables, projectRoot, repoRoot, pathsSameFilesystem)
}

func configureUVLinkModeWithComparator(envVariables map[string]string, projectRoot, repoRoot string, sameFilesystem uvFilesystemComparator) {
	if _, configured := effectiveEnvironmentValue(envVariables, "UV_LINK_MODE"); configured {
		return
	}
	if environmentFlagEnabled(envVariables, "UV_NO_CACHE") ||
		uvFilesystemPolicyConfigured(envVariables, projectRoot, repoRoot) {
		return
	}
	cacheDir := defaultUVCacheDirectory(envVariables, projectRoot)
	if cacheDir == "" {
		return
	}
	configureUVLinkModeForPaths(envVariables, projectRoot, cacheDir, sameFilesystem)
}

func configureUVLinkModeForPaths(envVariables map[string]string, projectRoot, cacheDir string, sameFilesystem uvFilesystemComparator) {
	same, err := sameFilesystem(projectRoot, cacheDir)
	if err == nil && !same {
		envVariables["UV_LINK_MODE"] = "copy"
	}
}

func effectiveEnvironmentValue(envVariables map[string]string, key string) (string, bool) {
	if value, ok := envVariables[key]; ok {
		return value, true
	}
	return os.LookupEnv(key)
}

func environmentFlagEnabled(envVariables map[string]string, key string) bool {
	value, ok := effectiveEnvironmentValue(envVariables, key)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func defaultUVCacheDirectory(envVariables map[string]string, projectRoot string) string {
	if cacheDir, ok := effectiveEnvironmentValue(envVariables, "UV_CACHE_DIR"); ok {
		return absoluteEnvironmentPath(cacheDir, projectRoot)
	}

	home, _ := effectiveEnvironmentValue(envVariables, "HOME")
	switch runtime.GOOS {
	case "windows":
		if localAppData, ok := effectiveEnvironmentValue(envVariables, "LOCALAPPDATA"); ok {
			return filepath.Join(absoluteEnvironmentPath(localAppData, projectRoot), "uv", "cache")
		}
	default:
		if xdg, ok := effectiveEnvironmentValue(envVariables, "XDG_CACHE_HOME"); ok {
			return filepath.Join(absoluteEnvironmentPath(xdg, projectRoot), "uv")
		}
		if home != "" {
			return filepath.Join(absoluteEnvironmentPath(home, projectRoot), ".cache", "uv")
		}
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cacheDir, "uv")
}

func absoluteEnvironmentPath(path, base string) string {
	path = strings.TrimSpace(path)
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if strings.TrimSpace(base) == "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return path
		}
		return abs
	}
	return filepath.Join(base, path)
}

// uvFilesystemPolicyConfigured conservatively preserves an explicit uv
// cache/link policy from any configuration scope uv would discover. Renart
// only supplies the automatic cross-filesystem fallback when uv is otherwise
// using its defaults.
func uvFilesystemPolicyConfigured(envVariables map[string]string, projectRoot, repoRoot string) bool {
	if environmentFlagEnabled(envVariables, "UV_NO_CONFIG") {
		return false
	}
	if configFile, ok := effectiveEnvironmentValue(envVariables, "UV_CONFIG_FILE"); ok && strings.TrimSpace(configFile) != "" {
		return uvConfigHasFilesystemPolicy(absoluteEnvironmentPath(configFile, projectRoot), false)
	}
	if projectUVFilesystemPolicyConfigured(projectRoot, repoRoot) {
		return true
	}
	for _, configPath := range userUVConfigPaths(envVariables, projectRoot) {
		if uvConfigHasFilesystemPolicy(configPath, false) {
			return true
		}
	}
	for _, configPath := range systemUVConfigPaths(envVariables, projectRoot) {
		if uvConfigHasFilesystemPolicy(configPath, false) {
			return true
		}
	}
	return false
}

func userUVConfigPaths(envVariables map[string]string, projectRoot string) []string {
	if runtime.GOOS == "windows" {
		if appData, ok := effectiveEnvironmentValue(envVariables, "APPDATA"); ok && strings.TrimSpace(appData) != "" {
			return []string{filepath.Join(absoluteEnvironmentPath(appData, projectRoot), "uv", "uv.toml")}
		}
	} else {
		if xdg, ok := effectiveEnvironmentValue(envVariables, "XDG_CONFIG_HOME"); ok && strings.TrimSpace(xdg) != "" {
			return []string{filepath.Join(absoluteEnvironmentPath(xdg, projectRoot), "uv", "uv.toml")}
		}
		if home, ok := effectiveEnvironmentValue(envVariables, "HOME"); ok && strings.TrimSpace(home) != "" {
			return []string{filepath.Join(absoluteEnvironmentPath(home, projectRoot), ".config", "uv", "uv.toml")}
		}
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(configDir, "uv", "uv.toml")}
}

func systemUVConfigPaths(envVariables map[string]string, projectRoot string) []string {
	if runtime.GOOS == "windows" {
		if programData, ok := effectiveEnvironmentValue(envVariables, "PROGRAMDATA"); ok && strings.TrimSpace(programData) != "" {
			return []string{filepath.Join(absoluteEnvironmentPath(programData, projectRoot), "uv", "uv.toml")}
		}
		return nil
	}

	paths := make([]string, 0, 3)
	if dirs, ok := effectiveEnvironmentValue(envVariables, "XDG_CONFIG_DIRS"); ok {
		for _, dir := range filepath.SplitList(dirs) {
			if strings.TrimSpace(dir) != "" {
				paths = append(paths, filepath.Join(absoluteEnvironmentPath(dir, projectRoot), "uv", "uv.toml"))
			}
		}
	} else {
		paths = append(paths, filepath.Join(string(filepath.Separator), "etc", "xdg", "uv", "uv.toml"))
	}
	return append(paths, filepath.Join(string(filepath.Separator), "etc", "uv", "uv.toml"))
}

// projectUVFilesystemPolicyConfigured detects project-local cache/link policy.
// When present, Renart leaves it untouched rather than guessing how uv will
// resolve a custom cache directory.
func projectUVFilesystemPolicyConfigured(projectRoot, repoRoot string) bool {
	current := filepath.Clean(projectRoot)
	stop := filepath.Clean(repoRoot)
	for {
		if uvConfigHasFilesystemPolicy(filepath.Join(current, "uv.toml"), false) ||
			uvConfigHasFilesystemPolicy(filepath.Join(current, "pyproject.toml"), true) {
			return true
		}
		if current == stop {
			return false
		}
		parent := filepath.Dir(current)
		if parent == current || !pathWithinRoot(stop, parent) {
			return false
		}
		current = parent
	}
}

func pathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func uvConfigHasFilesystemPolicy(path string, pyproject bool) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc struct {
		CacheDir string `toml:"cache-dir"`
		LinkMode string `toml:"link-mode"`
		Tool     struct {
			UV struct {
				CacheDir string `toml:"cache-dir"`
				LinkMode string `toml:"link-mode"`
			} `toml:"uv"`
		} `toml:"tool"`
	}
	if err := toml.Unmarshal(raw, &doc); err != nil {
		return false
	}
	if pyproject {
		return strings.TrimSpace(doc.Tool.UV.CacheDir) != "" || strings.TrimSpace(doc.Tool.UV.LinkMode) != ""
	}
	return strings.TrimSpace(doc.CacheDir) != "" || strings.TrimSpace(doc.LinkMode) != ""
}
