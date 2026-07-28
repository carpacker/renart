package secretstore

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalVaultProviderLifecycleAndIsolation(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	provider := newLocalVaultProviderForTests(directory, 8)
	projectID := "project-a"
	passphrase := []byte("correct horse battery staple")
	ref := Ref{Provider: localVaultProviderName, Key: "warehouse/password"}
	request := ResolveRequest{
		ProjectID: projectID, Environment: "default", Reference: ref,
		Purpose: PurposeSecretAdministration,
	}

	assert.Equal(t, LocalVaultUninitialized, provider.Status(projectID).State)
	require.NoError(t, provider.Initialize(t.Context(), projectID, passphrase))
	assert.Equal(t, LocalVaultUnlocked, provider.Status(projectID).State)

	_, err := provider.Put(t.Context(), PutRequest{
		ProjectID: projectID, Environment: "default", Reference: ref,
		Purpose: PurposeSecretAdministration, Value: []byte("vault-canary"),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, provider.Status(projectID).SecretCount)

	ciphertext, err := os.ReadFile(provider.vaultPath(projectID))
	require.NoError(t, err)
	assert.NotContains(t, string(ciphertext), "vault-canary")
	assert.NotContains(t, string(ciphertext), string(passphrase))
	assert.NotContains(t, string(ciphertext), projectID)
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(provider.vaultPath(projectID))
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	lease, err := provider.Resolve(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, []byte("vault-canary"), lease.Bytes())
	require.NoError(t, lease.Close(t.Context()))

	provider.Lock(projectID)
	assert.Equal(t, LocalVaultLocked, provider.Status(projectID).State)
	status, err := provider.Stat(t.Context(), request)
	assert.Equal(t, StatusPermissionRequired, status.State)
	assert.ErrorIs(t, err, ErrPermissionRequired)
	require.ErrorIs(t, provider.Unlock(t.Context(), projectID, []byte("wrong passphrase")), ErrVaultInvalidPassphrase)
	require.NoError(t, provider.Unlock(t.Context(), projectID, passphrase))

	_, err = provider.Resolve(t.Context(), ResolveRequest{
		ProjectID: projectID, Environment: "production", Reference: ref,
		Purpose: PurposeQuery,
	})
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = provider.Resolve(t.Context(), ResolveRequest{
		ProjectID: "project-b", Environment: "default", Reference: ref,
		Purpose: PurposeQuery,
	})
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestLocalVaultProviderRefreshesConcurrentChanges(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	first := newLocalVaultProviderForTests(directory, 8)
	second := newLocalVaultProviderForTests(directory, 8)
	projectID := "shared-project"
	passphrase := []byte("shared vault passphrase")
	firstRef := Ref{Provider: localVaultProviderName, Key: "warehouse/first"}
	secondRef := Ref{Provider: localVaultProviderName, Key: "warehouse/second"}

	require.NoError(t, first.Initialize(t.Context(), projectID, passphrase))
	require.NoError(t, second.Unlock(t.Context(), projectID, passphrase))
	_, err := first.Put(t.Context(), PutRequest{
		ProjectID: projectID, Environment: "default", Reference: firstRef,
		Purpose: PurposeSecretAdministration, Value: []byte("first"),
	})
	require.NoError(t, err)
	_, err = second.Put(t.Context(), PutRequest{
		ProjectID: projectID, Environment: "default", Reference: secondRef,
		Purpose: PurposeSecretAdministration, Value: []byte("second"),
	})
	require.NoError(t, err)

	for _, test := range []struct {
		ref  Ref
		want []byte
	}{
		{firstRef, []byte("first")},
		{secondRef, []byte("second")},
	} {
		lease, resolveErr := first.Resolve(t.Context(), ResolveRequest{
			ProjectID: projectID, Environment: "default", Reference: test.ref,
			Purpose: PurposeQuery,
		})
		require.NoError(t, resolveErr)
		assert.Equal(t, test.want, lease.Bytes())
		require.NoError(t, lease.Close(t.Context()))
	}
}

func TestLocalVaultProviderChangesPassphrase(t *testing.T) {
	t.Parallel()

	provider := newLocalVaultProviderForTests(t.TempDir(), 8)
	projectID := "project-a"
	oldPassphrase := []byte("old secure passphrase")
	newPassphrase := []byte("new secure passphrase")
	require.NoError(t, provider.Initialize(t.Context(), projectID, oldPassphrase))
	require.NoError(t, provider.ChangePassphrase(t.Context(), projectID, newPassphrase))
	provider.Lock(projectID)

	assert.ErrorIs(
		t,
		provider.Unlock(t.Context(), projectID, oldPassphrase),
		ErrVaultInvalidPassphrase,
	)
	require.NoError(t, provider.Unlock(t.Context(), projectID, newPassphrase))
}

func TestLocalVaultProviderRejectsInvalidOrCorruptVaults(t *testing.T) {
	t.Parallel()

	provider := newLocalVaultProviderForTests(t.TempDir(), 8)
	assert.ErrorContains(t, provider.Initialize(t.Context(), "project", []byte("too short")), "12 characters")
	require.NoError(t, provider.Initialize(
		t.Context(),
		"project",
		[]byte("valid passphrase"),
	))
	provider.Lock("project")
	require.NoError(t, os.WriteFile(
		provider.vaultPath("project"),
		bytes.Repeat([]byte("not-age"), 4),
		0o600,
	))
	err := provider.Unlock(t.Context(), "project", []byte("valid passphrase"))
	assert.Error(t, err)
	assert.False(t, errors.Is(err, ErrVaultInvalidPassphrase))
	assert.ErrorIs(t, err, ErrUnavailable)
}
