package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
		".renart/server.json*",
		".renart/scheduler.lock",
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

func TestEnsureRuntimeGitExcludesPreservesGitignoreAndHidesRuntimeFiles(t *testing.T) {
	workspaceRoot := t.TempDir()
	repo, err := git.PlainInit(workspaceRoot, false)
	require.NoError(t, err)
	const existing = "custom/\n"
	require.NoError(t, os.WriteFile(filepath.Join(workspaceRoot, ".gitignore"), []byte(existing), 0o644))

	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add(".gitignore")
	require.NoError(t, err)
	_, err = worktree.Commit("initial", &git.CommitOptions{Author: &object.Signature{
		Name: "test", Email: "test@example.com", When: time.Now(),
	}})
	require.NoError(t, err)

	require.NoError(t, EnsureRuntimeGitExcludes(workspaceRoot))
	require.NoError(t, EnsureRuntimeGitExcludes(workspaceRoot), "updating local excludes must be idempotent")

	gitignore, err := os.ReadFile(filepath.Join(workspaceRoot, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, existing, string(gitignore), "the user's tracked ignore file must not be changed")
	exclude, err := os.ReadFile(filepath.Join(workspaceRoot, ".git", "info", "exclude"))
	require.NoError(t, err)
	for _, expected := range []string{
		"/.renart/state.db*",
		"/.renart/server.lock",
		"/.renart/server.json*",
		"/.renart/scheduler.lock",
	} {
		assert.Equal(t, 1, strings.Count(string(exclude), expected), expected)
	}

	stateDir := filepath.Join(workspaceRoot, ".renart")
	require.NoError(t, os.MkdirAll(stateDir, 0o755))
	for _, name := range []string{"state.db", "state.db-wal", "server.lock", "server.json", "server.json.tmp", "scheduler.lock"} {
		require.NoError(t, os.WriteFile(filepath.Join(stateDir, name), []byte("runtime\n"), 0o600))
	}
	status, err := NewSourceControlService(workspaceRoot).Status(t.Context())
	require.NoError(t, err)
	assert.True(t, status.Clean, "%v", status.Changes)
	assert.Empty(t, status.Changes)

	// Excludes only suppress untracked local state. If a runtime file was
	// explicitly forced into the index, Renart must not conceal that tracked
	// change from the user.
	_, err = worktree.Add(".renart/server.lock")
	require.NoError(t, err)
	status, err = NewSourceControlService(workspaceRoot).Status(t.Context())
	require.NoError(t, err)
	require.Len(t, status.Changes, 1)
	assert.Equal(t, ".renart/server.lock", status.Changes[0].Path)
	assert.True(t, status.Changes[0].Staged)
}

func TestEnsureRuntimeGitExcludesAnchorsNestedWorkspaceToRepositoryRoot(t *testing.T) {
	repositoryRoot := t.TempDir()
	_, err := git.PlainInit(repositoryRoot, false)
	require.NoError(t, err)
	workspaceRoot := filepath.Join(repositoryRoot, "projects", "[analytics]")
	require.NoError(t, os.MkdirAll(workspaceRoot, 0o755))

	require.NoError(t, EnsureRuntimeGitExcludes(workspaceRoot))
	exclude, err := os.ReadFile(filepath.Join(repositoryRoot, ".git", "info", "exclude"))
	require.NoError(t, err)
	assert.Contains(t, string(exclude), "/projects/\\[analytics\\]/.renart/server.lock\n")
	assert.NotContains(t, string(exclude), "\n/.renart/server.lock\n")
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
