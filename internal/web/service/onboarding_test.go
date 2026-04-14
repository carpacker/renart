package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnboardingImportDatabaseReturnsSchemaAssetPaths(t *testing.T) {
	t.Parallel()

	workspaceRoot := t.TempDir()
	runner := &stubRunRunner{output: []byte(`{"status":"ok"}`)}
	svc := NewOnboardingService(workspaceRoot, filepath.Join(workspaceRoot, ".bruin.yml"), runner)

	result := svc.ImportDatabase(context.Background(), OnboardingImportRequest{
		ConnectionName:  "postgres-default",
		EnvironmentName: "default",
		PipelineName:    "analytics",
		Schema:          "analytics",
		Tables:          []string{"analytics.orders"},
		CreateIfMissing: true,
	})

	require.Equal(t, "ok", result.Status)
	assert.Equal(t, []string{"analytics/assets/analytics/orders.asset.yml"}, result.AssetPaths)
	assert.Equal(t, []string{"patch", "fill-asset-dependencies", "analytics"}, runner.args)

	contents, err := os.ReadFile(filepath.Join(workspaceRoot, "analytics", "pipeline.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(contents), "name: analytics")
}
