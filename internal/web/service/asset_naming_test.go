package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAssetPathForInferredName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "assets/analytics/orders.sql", assetPathForInferredName("analytics.orders", ".sql"))
	assert.Equal(t, "assets/my_project/finance/revenue.asset.yml", assetPathForInferredName("my project.finance.revenue", ".asset.yml"))
}

func TestInferredAssetNameFromPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "analytics.orders", inferredAssetNameFromPath("analytics/assets/analytics/orders.sql"))
	assert.Equal(t, "my_project.finance.revenue", inferredAssetNameFromPath("assets/my_project/finance/revenue.asset.yml"))
	assert.Equal(t, "", inferredAssetNameFromPath("analytics/orders.sql"))
}

func TestPipelineRelPathForAsset(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "analytics", pipelineRelPathForAsset("analytics/assets/analytics/orders.sql"))
	assert.Equal(t, ".", pipelineRelPathForAsset("assets/analytics/orders.sql"))
}

func TestRemoveAssetNameFieldFromContent(t *testing.T) {
	t.Parallel()

	content := "/* @bruin\nname: analytics.orders\ntype: duckdb.sql\n@bruin */\nselect 1"
	assert.Equal(t, "/* @bruin\ntype: duckdb.sql\n@bruin */\nselect 1", removeAssetNameFieldFromContent(content))
	assert.True(t, assetContentHasExplicitName(content))
	assert.False(t, assetContentHasExplicitName("/* @bruin\ntype: duckdb.sql\n@bruin */"))
}
