package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/ansisql"
	ath "github.com/bruin-data/bruin/pkg/athena"
	bq "github.com/bruin-data/bruin/pkg/bigquery"
	ch "github.com/bruin-data/bruin/pkg/clickhouse"
	"github.com/bruin-data/bruin/pkg/config"
	dbsql "github.com/bruin-data/bruin/pkg/databricks"
	"github.com/bruin-data/bruin/pkg/doris"
	duck "github.com/bruin-data/bruin/pkg/duckdb"
	bruinexecutor "github.com/bruin-data/bruin/pkg/executor"
	fw "github.com/bruin-data/bruin/pkg/fabric"
	bruiningestr "github.com/bruin-data/bruin/pkg/ingestr"
	"github.com/bruin-data/bruin/pkg/jinja"
	ms "github.com/bruin-data/bruin/pkg/mssql"
	my "github.com/bruin-data/bruin/pkg/mysql"
	"github.com/bruin-data/bruin/pkg/oracle"
	"github.com/bruin-data/bruin/pkg/pipeline"
	pg "github.com/bruin-data/bruin/pkg/postgres"
	"github.com/bruin-data/bruin/pkg/sail"
	"github.com/bruin-data/bruin/pkg/scheduler"
	sf "github.com/bruin-data/bruin/pkg/snowflake"
	sr "github.com/bruin-data/bruin/pkg/starrocks"
	vert "github.com/bruin-data/bruin/pkg/vertica"
)

var errDirectCheckDestinationUnsupported = errors.New("quality checks are not supported for this destination")

type directCheckConnectionTypeGetter interface {
	GetConnectionType(name string) string
}

// destinationAwareCheckOperator lets destination-resolved materializing assets
// reuse exact warehouse check operators without changing their public asset
// type. It resolves only a connection name and its non-secret type; credentials
// stay inside the connection manager and are never copied into an error or
// preview.
type destinationAwareCheckOperator struct {
	manager   config.ConnectionGetter
	executors map[pipeline.AssetType]bruinexecutor.Config
}

func (o *destinationAwareCheckOperator) Run(ctx context.Context, instance scheduler.TaskInstance) error {
	if instance == nil || instance.GetAsset() == nil || instance.GetPipeline() == nil {
		return errors.New("quality-check task is incomplete")
	}
	if instance.GetType() != scheduler.TaskInstanceTypeColumnCheck && instance.GetType() != scheduler.TaskInstanceTypeCustomCheck {
		return fmt.Errorf("destination-aware quality-check executor cannot run task type %s", instance.GetType())
	}

	connectionName, err := targetConnectionNameForAsset(instance.GetAsset(), instance.GetPipeline())
	if err != nil {
		return fmt.Errorf("resolve quality-check destination: %w", err)
	}
	connectionType := ""
	if getter, ok := o.manager.(directCheckConnectionTypeGetter); ok {
		connectionType = strings.TrimSpace(getter.GetConnectionType(connectionName))
	}

	var destinationType pipeline.AssetType
	if connectionType != "" {
		destinationType, _ = queryAssetTypeForConnectionType(connectionType)
	} else if assetType, ok := sqlAssetTypeForConnectionName(instance.GetPipeline(), connectionName); ok {
		destinationType = pipeline.AssetType(assetType)
	}
	if destinationType == "" {
		return unsupportedDirectCheckDestination(connectionType)
	}

	destinationConfig, ok := o.executors[destinationType]
	if !ok {
		return unsupportedDirectCheckDestination(connectionType)
	}
	operator, ok := destinationConfig[instance.GetType()]
	if !ok || operator == nil {
		return unsupportedDirectCheckDestination(connectionType)
	}

	resolvedInstance, copyExecutedQuery, err := cloneCheckInstanceWithConnection(instance, connectionName)
	if err != nil {
		return err
	}
	runErr := operator.Run(ctx, resolvedInstance)
	copyExecutedQuery()
	return runErr
}

func unsupportedDirectCheckDestination(connectionType string) error {
	connectionType = normalizeConnectionType(connectionType)
	if connectionType == "" {
		return errDirectCheckDestinationUnsupported
	}
	return fmt.Errorf("%w: destination type %q has no SQL quality-check operator", errDirectCheckDestinationUnsupported, connectionType)
}

func cloneCheckInstanceWithConnection(instance scheduler.TaskInstance, connectionName string) (scheduler.TaskInstance, func(), error) {
	asset := instance.GetAsset()
	if asset == nil {
		return nil, nil, errors.New("quality-check asset is missing")
	}
	resolvedAsset := *asset
	resolvedAsset.Connection = connectionName

	switch original := instance.(type) {
	case *scheduler.ColumnCheckInstance:
		clone := *original
		assetInstance := *original.AssetInstance
		assetInstance.Asset = &resolvedAsset
		clone.AssetInstance = &assetInstance
		return &clone, func() { original.ExecutedQuery = clone.ExecutedQuery }, nil
	case *scheduler.CustomCheckInstance:
		clone := *original
		assetInstance := *original.AssetInstance
		assetInstance.Asset = &resolvedAsset
		clone.AssetInstance = &assetInstance
		return &clone, func() { original.ExecutedQuery = clone.ExecutedQuery }, nil
	default:
		return nil, nil, fmt.Errorf("cannot resolve destination for task type %s", instance.GetType())
	}
}

// buildDirectCheckExecutors is the shared quality-check seam for direct runs
// and read-only rendering. The mapping must stay singular because generated
// check SQL is destination-specific even when its definition is portable.
func buildDirectCheckExecutors(manager config.ConnectionGetter, renderer *jinja.Renderer) (map[pipeline.AssetType]bruinexecutor.Config, error) {
	executors := make(map[pipeline.AssetType]bruinexecutor.Config)
	var checkRenderer jinja.RendererInterface
	if renderer != nil {
		checkRenderer = renderer
	}
	customCheck := ansisql.NewCustomCheckOperator(manager, checkRenderer)
	assign := func(assetTypes []pipeline.AssetType, columnCheck, custom bruinexecutor.Operator) {
		for _, assetType := range assetTypes {
			cfg := executors[assetType]
			if cfg == nil {
				cfg = bruinexecutor.Config{}
				executors[assetType] = cfg
			}
			if columnCheck != nil {
				cfg[scheduler.TaskInstanceTypeColumnCheck] = columnCheck
			}
			if custom != nil {
				cfg[scheduler.TaskInstanceTypeCustomCheck] = custom
			}
		}
	}

	duckChecks := duck.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeDuckDBQuery, pipeline.AssetTypeDuckDBSeed,
		pipeline.AssetTypeDuckDBQuerySensor, pipeline.AssetTypeMotherduckQuery,
	}, duckChecks, customCheck)

	postgresChecks := pg.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypePostgresQuery, pipeline.AssetTypePostgresSeed,
		pipeline.AssetTypePostgresQuerySensor, pipeline.AssetTypePostgresTableSensor,
		pipeline.AssetTypeRedshiftQuery, pipeline.AssetTypeRedshiftSeed,
		pipeline.AssetTypeRedshiftQuerySensor, pipeline.AssetTypeRedshiftTableSensor,
	}, postgresChecks, customCheck)

	bigQueryChecks, err := bq.NewColumnCheckOperator(manager)
	if err != nil {
		return nil, err
	}
	assign([]pipeline.AssetType{
		pipeline.AssetTypeBigqueryQuery, pipeline.AssetTypeBigquerySeed,
		pipeline.AssetTypeBigqueryQuerySensor, pipeline.AssetTypeBigqueryTableSensor,
	}, bigQueryChecks, customCheck)

	athenaChecks := ath.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeAthenaQuery, pipeline.AssetTypeAthenaSeed,
		pipeline.AssetTypeAthenaSQLSensor, pipeline.AssetTypeAthenaTableSensor,
		pipeline.AssetTypeTrinoQuery, assetTypeTrinoSeed,
		pipeline.AssetTypeTrinoQuerySensor, pipeline.AssetTypeDremioQuerySensor,
	}, athenaChecks, customCheck)

	databricksChecks := dbsql.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeDatabricksQuery, pipeline.AssetTypeDatabricksSeed,
		pipeline.AssetTypeDatabricksQuerySensor, pipeline.AssetTypeDatabricksTableSensor,
	}, databricksChecks, customCheck)

	dorisChecks := doris.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeDorisSeed, pipeline.AssetTypeDorisQuerySensor,
		pipeline.AssetTypeDorisTableSensor,
	}, dorisChecks, customCheck)

	fabricChecks := fw.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeFabricQuery, pipeline.AssetTypeFabricSeed,
		pipeline.AssetTypeFabricQueryLegacy, pipeline.AssetTypeFabricSeedLegacy,
		pipeline.AssetTypeFabricQuerySensor, pipeline.AssetTypeFabricTableSensor,
		pipeline.AssetTypeFabricQuerySensorLegacy, pipeline.AssetTypeFabricTableSensorLegacy,
	}, fabricChecks, customCheck)

	mySQLChecks := my.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeMySQLQuery, pipeline.AssetTypeMySQLSeed,
		pipeline.AssetTypeMySQLQuerySensor, pipeline.AssetTypeMySQLTableSensor,
	}, mySQLChecks, customCheck)

	snowflakeChecks := sf.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeSnowflakeQuery, pipeline.AssetTypeSnowflakeSeed,
		pipeline.AssetTypeSnowflakeQuerySensor, pipeline.AssetTypeSnowflakeTableSensor,
	}, snowflakeChecks, customCheck)

	msSQLChecks := ms.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeMsSQLQuery, pipeline.AssetTypeMsSQLSeed,
		pipeline.AssetTypeMsSQLQuerySensor, pipeline.AssetTypeMsSQLTableSensor,
		pipeline.AssetTypeSynapseQuery, pipeline.AssetTypeSynapseSeed,
		pipeline.AssetTypeSynapseQuerySensor, pipeline.AssetTypeSynapseTableSensor,
	}, msSQLChecks, customCheck)

	clickHouseChecks := ch.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeClickHouse, pipeline.AssetTypeClickHouseSeed,
		pipeline.AssetTypeClickHouseQuerySensor, pipeline.AssetTypeClickHouseTableSensor,
	}, clickHouseChecks, customCheck)

	starRocksChecks := sr.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeStarRocksQuery, pipeline.AssetTypeStarRocksSeed,
		pipeline.AssetTypeStarRocksQuerySensor, pipeline.AssetTypeStarRocksTableSensor,
	}, starRocksChecks, customCheck)

	assign([]pipeline.AssetType{pipeline.AssetTypeSailQuerySensor}, sail.NewColumnCheckOperator(manager), customCheck)

	verticaChecks := vert.NewColumnCheckOperator(manager)
	assign([]pipeline.AssetType{
		pipeline.AssetTypeVerticaQuery, pipeline.AssetTypeVerticaSeed,
		pipeline.AssetTypeVerticaQuerySensor, pipeline.AssetTypeVerticaTableSensor,
	}, verticaChecks, customCheck)

	assign([]pipeline.AssetType{pipeline.AssetTypeOracleQuery}, oracle.NewColumnCheckOperator(manager), customCheck)

	// Python, API, and Load assets choose their physical target independently
	// of their asset type. Delegate both check kinds to the operator for that
	// resolved destination instead of guessing from the pipeline majority.
	destinationChecks := &destinationAwareCheckOperator{manager: manager, executors: executors}
	for _, assetType := range []pipeline.AssetType{
		pipeline.AssetTypePython,
		pipeline.AssetType(apiAssetType),
		pipeline.AssetType(loadAssetType),
	} {
		executors[assetType] = bruinexecutor.Config{
			scheduler.TaskInstanceTypeColumnCheck: destinationChecks,
			scheduler.TaskInstanceTypeCustomCheck: destinationChecks,
			// These destination-resolved assets do not expose a direct metadata
			// publisher. An explicit no-op prevents metadata task instances from
			// rerunning their side-effecting API/Sling main operation.
			scheduler.TaskInstanceTypeMetadataPush: bruinexecutor.NoOpOperator{},
		}
	}

	// Ingestr resolves its check operator from the destination type at runtime.
	executors[pipeline.AssetTypeIngestr] = bruinexecutor.Config{
		scheduler.TaskInstanceTypeColumnCheck: bruiningestr.NewColumnCheckOperator(&executors),
		scheduler.TaskInstanceTypeCustomCheck: bruiningestr.NewCustomCheckOperator(&executors),
	}

	return executors, nil
}

func mergeDirectCheckExecutors(target, checks map[pipeline.AssetType]bruinexecutor.Config) {
	for assetType, checkConfig := range checks {
		cfg := target[assetType]
		if cfg == nil {
			cfg = bruinexecutor.Config{}
			target[assetType] = cfg
		}
		for taskType, operator := range checkConfig {
			cfg[taskType] = operator
		}
	}
}
