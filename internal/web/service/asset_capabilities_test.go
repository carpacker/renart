package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"renart/internal/web/model"
)

func TestAssetAuthoringCapabilitiesCoverDirectSeedsAndSensors(t *testing.T) {
	t.Parallel()
	capabilities := assetAuthoringCapabilities()
	byType := make(map[string]model.AssetAuthoringCapability, len(capabilities))
	for _, capability := range capabilities {
		_, duplicate := byType[capability.Type]
		assert.False(t, duplicate, "duplicate capability for %s", capability.Type)
		byType[capability.Type] = capability
		assert.NotEmpty(t, capability.ConnectionTypes, capability.Type)
		assert.True(t, isDirectRunAssetTypeSupported(pipeline.AssetType(capability.Type)), capability.Type)
	}

	assert.Len(t, capabilities, len(creatableSeedAssetTypes)+len(creatableSensorAssetTypes))
	assert.Contains(t, byType, string(pipeline.AssetTypeDorisSeed))
	assert.Contains(t, byType, string(pipeline.AssetTypeDremioQuerySensor))
	assert.Contains(t, byType, string(pipeline.AssetTypeSailQuerySensor))
	assert.Equal(t, []string{"trino"}, byType[string(assetTypeTrinoSeed)].ConnectionTypes)
	assert.NotContains(t, byType, string(pipeline.AssetTypeFabricSeedLegacy))
}

func TestTrinoSeedResolvesPipelineDefaultConnection(t *testing.T) {
	t.Parallel()
	pl := &pipeline.Pipeline{DefaultConnections: pipeline.EmptyStringMap{"trino": "trino-e2e"}}

	connection, err := pl.GetConnectionNameForAsset(&pipeline.Asset{Type: assetTypeTrinoSeed})
	require.NoError(t, err)
	assert.Equal(t, "trino-e2e", connection)
}

func TestSensorCapabilitiesDeclareVariantParameters(t *testing.T) {
	t.Parallel()
	for _, capability := range assetAuthoringCapabilities() {
		if capability.Kind != "sensor" {
			continue
		}
		assert.Equal(t, sensorRequiredParameters(capability.Variant), capability.RequiredParameters)
		assert.Equal(t, "30", capability.DefaultParameters["poke_interval"])
		assert.Equal(t, "24h", capability.DefaultParameters["timeout"])
	}
}

func TestDirectSensorExecutorsReplaceNoOpsForChecks(t *testing.T) {
	t.Parallel()
	executors, err := buildDirectMainExecutors(&stubConnectionManager{}, nil, nil, &pipeline.Pipeline{}, nil, nil, "", false, sensorModeWait)
	require.NoError(t, err)

	for _, assetType := range []pipeline.AssetType{
		pipeline.AssetTypeBigqueryQuerySensor,
		pipeline.AssetTypePostgresTableSensor,
		pipeline.AssetTypeSnowflakeQuerySensor,
		pipeline.AssetTypeDorisTableSensor,
		pipeline.AssetTypeDremioQuerySensor,
		pipeline.AssetTypeSailQuerySensor,
		pipeline.AssetTypeDuckDBQuerySensor,
	} {
		config := executors[assetType]
		require.NotNil(t, config, assetType)
		for _, taskType := range []scheduler.TaskInstanceType{
			scheduler.TaskInstanceTypeMain,
			scheduler.TaskInstanceTypeColumnCheck,
			scheduler.TaskInstanceTypeCustomCheck,
		} {
			operator := config[taskType]
			require.NotNil(t, operator, "%s %s", assetType, taskType)
			assert.NotContains(t, strings.ToLower(fmt.Sprintf("%T", operator)), "noop", "%s %s", assetType, taskType)
		}
	}
}

func TestEffectiveSensorModeDefaultsByRunKind(t *testing.T) {
	t.Parallel()
	assert.Equal(t, sensorModeOnce, effectiveSensorMode("", false))
	assert.Equal(t, sensorModeWait, effectiveSensorMode("", true))
	assert.Equal(t, sensorModeSkip, effectiveSensorMode(" SKIP ", true))
	assert.Equal(t, sensorModeOnce, effectiveSensorMode("invalid", false))
}
