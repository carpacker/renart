package service

import (
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
