package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindWorkspaceRootWalkUp(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".bruin.yml"), "environments: {}\n")
	nested := filepath.Join(root, "pipelines", "marts")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findWorkspaceRoot("", nested)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("findWorkspaceRoot = %q, want %q", got, root)
	}
}

func TestFindWorkspaceRootExplicitWins(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	mustWrite(t, filepath.Join(root, ".bruin.yml"), "environments: {}\n")

	got, err := findWorkspaceRoot(other, root)
	if err != nil {
		t.Fatal(err)
	}
	if got != other {
		t.Errorf("explicit workspace ignored: got %q, want %q", got, other)
	}
}

func TestFindWorkspaceRootRenartFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".renart"), 0o755); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(root, "somewhere", "deep")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findWorkspaceRoot("", inner)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("findWorkspaceRoot = %q, want %q", got, root)
	}
}

func TestResolvePipelineDirByName(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".bruin.yml"), "environments: {}\n")
	martsDir := filepath.Join(root, "pipelines", "marts")
	mustWrite(t, filepath.Join(martsDir, "pipeline.yml"), "name: marts\n")
	rawDir := filepath.Join(root, "pipelines", "raw")
	mustWrite(t, filepath.Join(rawDir, "pipeline.yml"), "name: raw_zone\n")

	got, err := resolvePipelineDir(root, "marts", root)
	if err != nil {
		t.Fatal(err)
	}
	if got != martsDir {
		t.Errorf("by name = %q, want %q", got, martsDir)
	}

	// Directory basename also matches when the manifest name differs.
	got, err = resolvePipelineDir(root, "raw", root)
	if err != nil {
		t.Fatal(err)
	}
	if got != rawDir {
		t.Errorf("by basename = %q, want %q", got, rawDir)
	}

	if _, err := resolvePipelineDir(root, "nope", root); err == nil {
		t.Error("expected an error for an unknown pipeline name")
	}
}

func TestResolvePipelineDirFromCwd(t *testing.T) {
	root := t.TempDir()
	pipelineDir := filepath.Join(root, "marts")
	mustWrite(t, filepath.Join(pipelineDir, "pipeline.yml"), "name: marts\n")
	assetsDir := filepath.Join(pipelineDir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolvePipelineDir(root, "", assetsDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != pipelineDir {
		t.Errorf("from cwd = %q, want %q", got, pipelineDir)
	}
}

func TestResolvePipelineDirPath(t *testing.T) {
	root := t.TempDir()
	pipelineDir := filepath.Join(root, "marts")
	mustWrite(t, filepath.Join(pipelineDir, "pipeline.yml"), "name: marts\n")

	got, err := resolvePipelineDir(root, pipelineDir, root)
	if err != nil {
		t.Fatal(err)
	}
	if got != pipelineDir {
		t.Errorf("by absolute path = %q, want %q", got, pipelineDir)
	}

	got, err = resolvePipelineDir(root, "marts", pipelineDir) // relative would be ./marts from root
	if err != nil {
		t.Fatal(err)
	}
	if got != pipelineDir {
		t.Errorf("by name with cwd inside = %q, want %q", got, pipelineDir)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
