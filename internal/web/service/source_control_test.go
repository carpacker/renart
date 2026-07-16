package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceControlStatusWithoutRepository(t *testing.T) {
	service := NewSourceControlService(t.TempDir())

	status, err := service.Status(t.Context())
	require.NoError(t, err)
	assert.False(t, status.HasRepository)
	assert.True(t, status.Clean)
	assert.Empty(t, status.Branch)
	assert.Empty(t, status.Changes)
}

func TestSourceControlInitCreatesRepositoryAndGitignore(t *testing.T) {
	workspaceRoot := t.TempDir()
	service := NewSourceControlService(workspaceRoot)

	status, err := service.Init(t.Context())
	require.NoError(t, err)

	assert.True(t, status.HasRepository)
	assert.Equal(t, "main", status.Branch)
	assert.DirExists(t, filepath.Join(workspaceRoot, ".git"))
	gitignore, err := os.ReadFile(filepath.Join(workspaceRoot, ".gitignore"))
	require.NoError(t, err)
	for _, expected := range []string{
		".renart/state.db*",
		".renart/server.lock",
		"logs/",
		"duckdb-files/",
		".env",
		"__pycache__/",
		".DS_Store",
	} {
		assert.Contains(t, string(gitignore), expected)
	}
	assert.NotContains(t, "\n"+string(gitignore)+"\n", "\n.renart/\n")
}

func TestSourceControlInitExistingRepositoryErrors(t *testing.T) {
	workspaceRoot := t.TempDir()
	_, err := git.PlainInit(workspaceRoot, false)
	require.NoError(t, err)

	service := NewSourceControlService(workspaceRoot)
	_, err = service.Init(t.Context())
	require.Error(t, err)
	assert.True(t, errors.Is(err, git.ErrRepositoryAlreadyExists))
}

func TestSourceControlInitDoesNotOverwriteExistingGitignore(t *testing.T) {
	workspaceRoot := t.TempDir()
	const existing = "custom/\n"
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".gitignore"), []byte(existing), 0o644))

	service := NewSourceControlService(workspaceRoot)
	_, err := service.Init(t.Context())
	require.NoError(t, err)

	gitignore, err := os.ReadFile(filepath.Join(workspaceRoot, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, existing, string(gitignore))
	assert.NotContains(t, string(gitignore), ".renart/state.db*")
}

func TestSourceControlUnstageWithoutCommitHistory(t *testing.T) {
	workspaceRoot := t.TempDir()
	_, err := git.PlainInit(workspaceRoot, false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "pipeline.yml"), []byte("name: test\n"), 0o644))

	service := NewSourceControlService(workspaceRoot)
	require.NoError(t, service.Stage([]string{"pipeline.yml"}))

	status, err := service.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, status.Changes, 1)
	assert.True(t, status.Changes[0].Staged)

	require.NoError(t, service.Unstage([]string{"pipeline.yml"}))
	status, err = service.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, status.Changes, 1)
	assert.False(t, status.Changes[0].Staged)
	assert.Equal(t, "?", status.Changes[0].WorktreeStatus)
}

func TestSourceControlStageDeletedFile(t *testing.T) {
	workspaceRoot := t.TempDir()
	repo, err := git.PlainInit(workspaceRoot, false)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, "pipeline.yml"), []byte("name: test\n"), 0o644))

	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("pipeline.yml")
	require.NoError(t, err)
	_, err = worktree.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.com", When: time.Now()}})
	require.NoError(t, err)

	// Deleting then staging must record the deletion — the SkipStatus fast path
	// only applies to existing files, so deletions still fall back correctly.
	require.NoError(t, os.Remove(filepath.Join(workspaceRoot, "pipeline.yml")))

	service := NewSourceControlService(workspaceRoot)
	require.NoError(t, service.Stage([]string{"pipeline.yml"}))

	status, err := service.Status(t.Context())
	require.NoError(t, err)
	require.Len(t, status.Changes, 1)
	assert.True(t, status.Changes[0].Staged)
	assert.Equal(t, "D", status.Changes[0].StagedStatus)
}
