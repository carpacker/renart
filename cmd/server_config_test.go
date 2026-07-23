package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bruin-data/bruin/pkg/git"
	gogit "github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/require"
)

func TestResolveServerWorkspaceRootKeepsGitProject(t *testing.T) {
	project := t.TempDir()
	_, err := gogit.PlainInit(project, false)
	require.NoError(t, err)

	root, bootstrap, err := resolveServerWorkspaceRoot(project, true)

	require.NoError(t, err)
	require.Equal(t, project, root)
	require.Empty(t, bootstrap)
}

func TestResolveServerWorkspaceRootBootstrapsImplicitLauncher(t *testing.T) {
	launchRoot := t.TempDir()

	root, bootstrap, err := resolveServerWorkspaceRoot(launchRoot, true)
	require.NoError(t, err)
	require.NotEqual(t, launchRoot, root)
	require.Equal(t, root, bootstrap)
	t.Cleanup(func() { _ = os.RemoveAll(bootstrap) })

	repo, err := git.FindRepoFromPath(root)
	require.NoError(t, err)
	require.Equal(t, filepath.Clean(root), filepath.Clean(repo.Path))
}

func TestResolveServerWorkspaceRootRejectsExplicitNonProject(t *testing.T) {
	launchRoot := t.TempDir()

	root, bootstrap, err := resolveServerWorkspaceRoot(launchRoot, false)

	require.ErrorContains(t, err, "projects must live inside a git repository")
	require.Empty(t, root)
	require.Empty(t, bootstrap)
}

func TestCleanupServerBootstrapRemovesOnlyTemporaryRoot(t *testing.T) {
	root := t.TempDir()
	bootstrap := filepath.Join(root, "bootstrap")
	require.NoError(t, os.Mkdir(bootstrap, 0o755))

	cleanupServerBootstrap(serverConfig{workspaceRoot: bootstrap, bootstrapRoot: bootstrap})

	require.NoDirExists(t, bootstrap)
	require.DirExists(t, root)
}
