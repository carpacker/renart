package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigureUVLinkModeSelectsCopyOnlyAcrossFilesystems(t *testing.T) {
	unsetEnvironmentForTest(t, "UV_LINK_MODE")
	projectRoot := filepath.Join(t.TempDir(), "snapshot")
	cacheDir := filepath.Join(t.TempDir(), "uv-cache")
	env := map[string]string{
		"UV_CACHE_DIR":   cacheDir,
		"UV_NO_CACHE":    "false",
		"UV_NO_CONFIG":   "true",
		"UV_CONFIG_FILE": "",
	}

	compared := false
	configureUVLinkModeWithComparator(env, projectRoot, projectRoot, func(first, second string) (bool, error) {
		compared = true
		if first != projectRoot || second != cacheDir {
			t.Fatalf("compared %q and %q, want %q and %q", first, second, projectRoot, cacheDir)
		}
		return false, nil
	})

	if !compared {
		t.Fatal("expected filesystem comparison")
	}
	if got := env["UV_LINK_MODE"]; got != "copy" {
		t.Fatalf("UV_LINK_MODE = %q, want copy", got)
	}

	delete(env, "UV_LINK_MODE")
	configureUVLinkModeWithComparator(env, projectRoot, projectRoot, func(_, _ string) (bool, error) {
		return true, nil
	})
	if _, ok := env["UV_LINK_MODE"]; ok {
		t.Fatal("same-filesystem projects must keep uv's default link mode")
	}

	configureUVLinkModeWithComparator(env, projectRoot, projectRoot, func(_, _ string) (bool, error) {
		return false, errors.New("stat failed")
	})
	if _, ok := env["UV_LINK_MODE"]; ok {
		t.Fatal("filesystem detection errors must not change uv's link mode")
	}
}

func TestConfigureUVLinkModePreservesExplicitPolicy(t *testing.T) {
	projectRoot := t.TempDir()
	cacheDir := filepath.Join(t.TempDir(), "uv-cache")
	pyproject := `[project]
name = "example"
version = "0.0.0"

[tool.uv]
link-mode = "hardlink"
`
	if err := os.WriteFile(filepath.Join(projectRoot, "pyproject.toml"), []byte(pyproject), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		env  map[string]string
	}{
		{
			name: "environment link mode",
			env: map[string]string{
				"UV_LINK_MODE": "clone",
			},
		},
		{
			name: "project link mode",
			env: map[string]string{
				"UV_CACHE_DIR":   cacheDir,
				"UV_NO_CACHE":    "false",
				"UV_NO_CONFIG":   "false",
				"UV_CONFIG_FILE": "",
			},
		},
		{
			name: "disabled cache",
			env: map[string]string{
				"UV_CACHE_DIR": cacheDir,
				"UV_NO_CACHE":  "true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsetEnvironmentForTest(t, "UV_LINK_MODE")
			called := false
			configureUVLinkModeWithComparator(tt.env, projectRoot, projectRoot, func(_, _ string) (bool, error) {
				called = true
				return false, nil
			})
			if called {
				t.Fatal("explicit uv policy should bypass filesystem detection")
			}
			if tt.name == "environment link mode" && tt.env["UV_LINK_MODE"] != "clone" {
				t.Fatalf("explicit link mode was replaced with %q", tt.env["UV_LINK_MODE"])
			}
			if tt.name != "environment link mode" {
				if _, ok := tt.env["UV_LINK_MODE"]; ok {
					t.Fatal("Renart added a link mode despite explicit uv policy")
				}
			}
		})
	}
}

func TestDefaultUVCacheDirectoryResolvesRelativeOverrideFromProject(t *testing.T) {
	projectRoot := t.TempDir()
	got := defaultUVCacheDirectory(map[string]string{"UV_CACHE_DIR": "relative-cache"}, projectRoot)
	want := filepath.Join(projectRoot, "relative-cache")
	if got != want {
		t.Fatalf("defaultUVCacheDirectory() = %q, want %q", got, want)
	}
}

func TestPathsSameFilesystemUsesExistingAncestors(t *testing.T) {
	root := t.TempDir()
	same, err := pathsSameFilesystem(
		filepath.Join(root, "missing-project"),
		filepath.Join(root, "missing-cache", "nested"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !same {
		t.Fatal("paths below the same existing directory should share a filesystem")
	}
}

func unsetEnvironmentForTest(t *testing.T, key string) {
	t.Helper()
	value, existed := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	})
}
