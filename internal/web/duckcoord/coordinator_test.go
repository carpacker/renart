package duckcoord

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalPathNormalizesURIsQueriesAndSymlinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	require.NoError(t, os.Mkdir(realDir, 0o755))
	linkDir := filepath.Join(root, "linked")
	require.NoError(t, os.Symlink(realDir, linkDir))

	physical, err := CanonicalPath(root, filepath.Join("real", "warehouse.duckdb"))
	require.NoError(t, err)
	throughLink, err := CanonicalPath(root, filepath.Join("linked", "warehouse.duckdb")+"?access_mode=read_only")
	require.NoError(t, err)
	uri, err := CanonicalPath("", "duckdb://"+filepath.ToSlash(filepath.Join(linkDir, "warehouse.duckdb")))
	require.NoError(t, err)

	assert.Equal(t, physical, throughLink)
	assert.Equal(t, physical, uri)
	assert.Equal(t, filepath.Join(realDir, "warehouse.duckdb"), physical)
}

func TestCanonicalPathSkipsNonFileDatabases(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", ":memory:", "md:analytics", "motherduck:analytics"} {
		path, err := CanonicalPath(t.TempDir(), raw)
		require.NoError(t, err)
		assert.Empty(t, path)
	}
}

func TestCoordinatorSerializesTheSameDatabase(t *testing.T) {
	t.Parallel()

	coordinator := New(Options{LockDir: t.TempDir(), RetryDelay: time.Millisecond})
	database := filepath.Join(t.TempDir(), "shared.duckdb")
	first, err := coordinator.Acquire(context.Background(), []string{database}, Owner{})
	require.NoError(t, err)
	defer first.Release()

	acquired := make(chan *Lease, 1)
	waiting := make(chan struct{}, 1)
	go func() {
		lease, acquireErr := coordinator.Acquire(context.Background(), []string{database}, Owner{
			OnWait: func(string) { waiting <- struct{}{} },
		})
		if acquireErr == nil {
			acquired <- lease
		}
	}()

	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("second lease did not report waiting")
	}
	select {
	case lease := <-acquired:
		lease.Release()
		t.Fatal("second lease acquired before the first was released")
	case <-time.After(40 * time.Millisecond):
	}

	first.Release()
	select {
	case lease := <-acquired:
		lease.Release()
	case <-time.After(time.Second):
		t.Fatal("second lease did not acquire after release")
	}
}

func TestCoordinatorAllowsDifferentDatabasesConcurrently(t *testing.T) {
	t.Parallel()

	coordinator := New(Options{LockDir: t.TempDir(), RetryDelay: time.Millisecond})
	first, err := coordinator.Acquire(context.Background(), []string{filepath.Join(t.TempDir(), "first.duckdb")}, Owner{})
	require.NoError(t, err)
	defer first.Release()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	second, err := coordinator.Acquire(ctx, []string{filepath.Join(t.TempDir(), "second.duckdb")}, Owner{})
	require.NoError(t, err)
	second.Release()
}

func TestCoordinatorSortsMultipleDatabasesToAvoidDeadlock(t *testing.T) {
	t.Parallel()

	coordinator := New(Options{LockDir: t.TempDir(), RetryDelay: time.Millisecond})
	root := t.TempDir()
	firstPath := filepath.Join(root, "a.duckdb")
	secondPath := filepath.Join(root, "b.duckdb")

	start := make(chan struct{})
	done := make(chan error, 2)
	acquire := func(paths []string) {
		<-start
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		lease, err := coordinator.Acquire(ctx, paths, Owner{})
		if err == nil {
			time.Sleep(20 * time.Millisecond)
			lease.Release()
		}
		done <- err
	}
	go acquire([]string{firstPath, secondPath})
	go acquire([]string{secondPath, firstPath})
	close(start)

	require.NoError(t, <-done)
	require.NoError(t, <-done)
}

func TestCoordinatorWaitIsContextCancellable(t *testing.T) {
	t.Parallel()

	coordinator := New(Options{LockDir: t.TempDir(), RetryDelay: time.Millisecond})
	database := filepath.Join(t.TempDir(), "shared.duckdb")
	first, err := coordinator.Acquire(context.Background(), []string{database}, Owner{})
	require.NoError(t, err)
	defer first.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = coordinator.Acquire(ctx, []string{database}, Owner{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestCoordinatorSerializesAcrossProcesses(t *testing.T) {
	if os.Getenv("RENART_DUCKCOORD_HELPER") == "1" {
		runCoordinatorHelperProcess()
		return
	}

	root := t.TempDir()
	lockDir := filepath.Join(root, "locks")
	database := filepath.Join(root, "shared.duckdb")
	ready := filepath.Join(root, "ready")
	release := filepath.Join(root, "release")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCoordinatorSerializesAcrossProcesses$")
	cmd.Env = append(os.Environ(),
		"RENART_DUCKCOORD_HELPER=1",
		"RENART_DUCKCOORD_LOCK_DIR="+lockDir,
		"RENART_DUCKCOORD_DATABASE="+database,
		"RENART_DUCKCOORD_READY="+ready,
		"RENART_DUCKCOORD_RELEASE="+release,
	)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = os.WriteFile(release, nil, 0o600)
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	}, 3*time.Second, 10*time.Millisecond)

	coordinator := New(Options{LockDir: lockDir, RetryDelay: time.Millisecond})
	waited := false
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := coordinator.Acquire(ctx, []string{database}, Owner{OnWait: func(string) { waited = true }})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.True(t, waited)

	require.NoError(t, os.WriteFile(release, nil, 0o600))
	require.NoError(t, cmd.Wait())

	lease, err := coordinator.Acquire(context.Background(), []string{database}, Owner{})
	require.NoError(t, err)
	lease.Release()
}

func runCoordinatorHelperProcess() {
	coordinator := New(Options{
		LockDir:    os.Getenv("RENART_DUCKCOORD_LOCK_DIR"),
		RetryDelay: time.Millisecond,
	})
	lease, err := coordinator.Acquire(context.Background(), []string{os.Getenv("RENART_DUCKCOORD_DATABASE")}, Owner{})
	if err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv("RENART_DUCKCOORD_READY"), nil, 0o600); err != nil {
		os.Exit(3)
	}
	for {
		if _, err := os.Stat(os.Getenv("RENART_DUCKCOORD_RELEASE")); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	lease.Release()
	os.Exit(0)
}
