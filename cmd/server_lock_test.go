package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"renart/internal/clientapi"
)

func TestWorkspaceServerLeaseRejectsSecondRuntimeWithOwnerDetails(t *testing.T) {
	isolateWorkspaceServerLeases(t)
	root := t.TempDir()
	first, err := acquireWorkspaceServerLease(t.Context(), root)
	require.NoError(t, err)
	defer first.Close()

	health := newWorkspaceHealthServer(t, root)
	startedAt := time.Date(2026, time.July, 16, 18, 30, 0, 0, time.UTC)
	require.NoError(t, clientapi.WriteServerFile(root, clientapi.ServerFile{
		PID:           4242,
		BaseURL:       health.URL,
		APIBaseURL:    health.URL + "/api",
		WorkspaceRoot: root,
		StartedAt:     startedAt,
	}))

	second, err := acquireWorkspaceServerLease(t.Context(), root)
	require.Nil(t, second)
	require.Error(t, err)
	assert.ErrorIs(t, err, errWorkspaceAlreadyServed)
	assert.ErrorContains(t, err, "PID 4242")
	assert.ErrorContains(t, err, health.URL)
	assert.ErrorContains(t, err, root)
	assert.ErrorContains(t, err, filepath.Join(root, ".renart", "server.lock"))
	primaryPath, pathErr := workspaceServerLeasePath(root)
	require.NoError(t, pathErr)
	assert.ErrorContains(t, err, primaryPath)
}

func TestWorkspaceServerLeaseRejectsLivePreLockServerAndReleasesLease(t *testing.T) {
	isolateWorkspaceServerLeases(t)
	root := t.TempDir()
	health := newWorkspaceHealthServer(t, root)
	require.NoError(t, clientapi.WriteServerFile(root, clientapi.ServerFile{
		PID:           99,
		BaseURL:       health.URL,
		APIBaseURL:    health.URL + "/api",
		WorkspaceRoot: root,
		StartedAt:     time.Now().UTC(),
	}))

	lease, err := acquireWorkspaceServerLease(t.Context(), root)
	require.Nil(t, lease)
	require.ErrorIs(t, err, errWorkspaceAlreadyServed)

	health.Close()
	clientapi.RemoveServerFile(root)
	lease, err = acquireWorkspaceServerLease(t.Context(), root)
	require.NoError(t, err, "the rejected startup must release the newly acquired lease")
	require.NoError(t, lease.Close())
}

func TestWorkspaceServerLeaseAllowsUnlockedPersistentFile(t *testing.T) {
	isolateWorkspaceServerLeases(t)
	root := t.TempDir()
	lockPath := filepath.Join(root, ".renart", "server.lock")
	require.NoError(t, os.MkdirAll(filepath.Dir(lockPath), 0o755))
	require.NoError(t, os.WriteFile(lockPath, []byte("left by an exited process\n"), 0o600))
	primaryPath, err := workspaceServerLeasePath(root)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(primaryPath), 0o700))
	require.NoError(t, os.WriteFile(primaryPath, []byte("left by an exited process\n"), 0o600))

	lease, err := acquireWorkspaceServerLease(t.Context(), root)
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.NoError(t, lease.Close())
	require.NoError(t, lease.Close(), "lease cleanup is idempotent")
}

func TestWorkspaceServerLeaseAllowsDifferentWorkspaces(t *testing.T) {
	isolateWorkspaceServerLeases(t)
	first, err := acquireWorkspaceServerLease(t.Context(), t.TempDir())
	require.NoError(t, err)
	defer first.Close()

	second, err := acquireWorkspaceServerLease(t.Context(), t.TempDir())
	require.NoError(t, err, "different Renart servers may own different workspace runtimes")
	defer second.Close()
}

func TestRuntimeWorkspaceLeaseSkipsEmbeddedCLI(t *testing.T) {
	isolateWorkspaceServerLeases(t)
	root := t.TempDir()
	held, err := acquireWorkspaceServerLease(t.Context(), root)
	require.NoError(t, err)
	defer held.Close()

	lease, err := acquireRuntimeWorkspaceLease(t.Context(), serverConfig{
		workspaceRoot: root,
		headless:      true,
	})
	require.NoError(t, err)
	assert.Nil(t, lease)
}

func TestWorkspaceExecutionCoordinatorSeparatesLiveExecutionFromRecovery(t *testing.T) {
	isolateWorkspaceServerLeases(t)
	coordinator, err := newWorkspaceExecutionCoordinator(t.TempDir())
	require.NoError(t, err)

	releaseFirst, err := coordinator.AcquireShared(t.Context())
	require.NoError(t, err)
	releaseSecond, err := coordinator.AcquireShared(t.Context())
	require.NoError(t, err, "independent executions may hold the workspace lease concurrently")

	releaseRecovery, acquired, err := coordinator.tryAcquireExclusive()
	require.NoError(t, err)
	assert.False(t, acquired, "claim recovery must not enter while any physical execution is live")
	assert.Nil(t, releaseRecovery)

	require.NoError(t, releaseFirst())
	releaseRecovery, acquired, err = coordinator.tryAcquireExclusive()
	require.NoError(t, err)
	assert.False(t, acquired, "all shared executors must finish before recovery")
	assert.Nil(t, releaseRecovery)
	require.NoError(t, releaseSecond())

	releaseRecovery, acquired, err = coordinator.tryAcquireExclusive()
	require.NoError(t, err)
	require.True(t, acquired, "an exited executor leaves its claims recoverable")
	require.NotNil(t, releaseRecovery)

	waitCtx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
	defer cancel()
	_, err = coordinator.AcquireShared(waitCtx)
	require.Error(t, err, "new physical work must wait while recovery owns the exclusive lease")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, releaseRecovery())

	releaseExecution, err := coordinator.AcquireShared(t.Context())
	require.NoError(t, err)
	require.NoError(t, releaseExecution())
}

func TestWorkspaceExecutionCoordinatorAllowsDifferentWorkspaces(t *testing.T) {
	isolateWorkspaceServerLeases(t)
	first, err := newWorkspaceExecutionCoordinator(t.TempDir())
	require.NoError(t, err)
	second, err := newWorkspaceExecutionCoordinator(t.TempDir())
	require.NoError(t, err)

	releaseFirst, err := first.acquireExclusive(t.Context())
	require.NoError(t, err)
	defer releaseFirst()
	releaseSecond, err := second.AcquireShared(t.Context())
	require.NoError(t, err, "execution coordination remains scoped to one workspace")
	require.NoError(t, releaseSecond())
}

func TestNewWebServerChecksWorkspaceLeaseBeforeStateDatabase(t *testing.T) {
	isolateWorkspaceServerLeases(t)
	root := t.TempDir()
	held, err := acquireWorkspaceServerLease(t.Context(), root)
	require.NoError(t, err)
	defer held.Close()

	statePath := filepath.Join(root, ".renart", "state.db")
	require.NoError(t, os.WriteFile(statePath, []byte("not a sqlite database"), 0o600))

	server, cleanup, err := newWebServer(t.Context(), serverConfig{
		workspaceRoot:      root,
		staticDir:          filepath.Join(root, "static"),
		watchMode:          "auto",
		watchPoll:          time.Second,
		schedulerEnabled:   true,
		schedulerStatePath: statePath,
	}, zap.NewNop())
	assert.Nil(t, server)
	assert.Nil(t, cleanup)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errWorkspaceAlreadyServed))
	assert.NotContains(t, err.Error(), "integrity check", "the state database must not be opened")
}

func TestWorkspaceServerLeaseSurvivesCompatibilityFileRemoval(t *testing.T) {
	isolateWorkspaceServerLeases(t)
	root := t.TempDir()
	first, err := acquireWorkspaceServerLease(t.Context(), root)
	require.NoError(t, err)
	defer first.Close()

	// Simulate `git clean` unlinking the old in-worktree lock and discovery
	// record. The primary lease lives outside the worktree and must still reject
	// another current Renart process.
	require.NoError(t, os.Remove(filepath.Join(root, ".renart", "server.lock")))
	clientapi.RemoveServerFile(root)

	second, err := acquireWorkspaceServerLease(t.Context(), root)
	require.Nil(t, second)
	require.ErrorIs(t, err, errWorkspaceAlreadyServed)
}

func TestWorkspaceServerLeaseCanonicalizesSymlinkedRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires privileges on Windows")
	}
	isolateWorkspaceServerLeases(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	require.NoError(t, os.Mkdir(root, 0o755))
	alias := filepath.Join(parent, "workspace-alias")
	require.NoError(t, os.Symlink(root, alias))

	first, err := acquireWorkspaceServerLease(t.Context(), root)
	require.NoError(t, err)
	defer first.Close()

	second, err := acquireWorkspaceServerLease(t.Context(), alias)
	require.Nil(t, second)
	require.ErrorIs(t, err, errWorkspaceAlreadyServed)
}

func isolateWorkspaceServerLeases(t *testing.T) {
	t.Helper()
	t.Setenv("RENART_WORKSPACE_LOCK_DIR", filepath.Join(t.TempDir(), "workspace-leases"))
}

func newWorkspaceHealthServer(t *testing.T, root string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `{"status":"ok","version":"test","workspace_root":%q,"project_id":"project"}`, root)
	}))
	t.Cleanup(server.Close)
	return server
}
