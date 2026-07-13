package service

import (
	"testing"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplySelectedEnvironmentRefreshRestriction(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{Name: "analytics.orders"}
	cfg := &config.Config{SelectedEnvironment: &config.Environment{
		Config: &config.EnvironmentConfig{RefreshRestricted: true},
	}}

	applySelectedEnvironmentRefreshRestriction(cfg, []*pipeline.Asset{asset, nil})

	require.NotNil(t, asset.RefreshRestricted)
	assert.True(t, *asset.RefreshRestricted)
}

func TestApplySelectedEnvironmentRefreshRestrictionLeavesUnrestrictedAssetsAlone(t *testing.T) {
	t.Parallel()

	asset := &pipeline.Asset{Name: "analytics.orders"}
	applySelectedEnvironmentRefreshRestriction(&config.Config{}, []*pipeline.Asset{asset})
	assert.Nil(t, asset.RefreshRestricted)
}
