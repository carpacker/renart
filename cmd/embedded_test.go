package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/service"
)

func TestEmbeddedServerExcludesRuntimeStateWithoutChangingGitignore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo, err := git.PlainInit(root, false)
	require.NoError(t, err)
	const gitignoreContents = "custom/\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignoreContents), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".bruin.yml"), []byte("environments: {}\n"), 0o644))
	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add(".gitignore")
	require.NoError(t, err)
	_, err = worktree.Commit("initial", &git.CommitOptions{Author: &object.Signature{
		Name: "test", Email: "test@example.com", When: time.Now(),
	}})
	require.NoError(t, err)

	_, cleanup, err := newEmbeddedServer(t.Context(), root)
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	cleanup()

	gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, gitignoreContents, string(gitignore))
	require.FileExists(t, filepath.Join(root, ".renart", "state.db"))

	exclude, err := os.ReadFile(filepath.Join(root, ".git", "info", "exclude"))
	require.NoError(t, err)
	assert.Contains(t, string(exclude), "/.renart/state.db*\n")

	status, err := service.NewSourceControlService(root).Status(t.Context())
	require.NoError(t, err)
	for _, change := range status.Changes {
		assert.False(t, strings.HasPrefix(filepath.ToSlash(change.Path), ".renart/state.db"), change.Path)
	}
}
