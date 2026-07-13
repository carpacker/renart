package service

import (
	"context"
	"fmt"
	"time"

	"github.com/bruin-data/bruin/pkg/ansisql"
	ath "github.com/bruin-data/bruin/pkg/athena"
	bq "github.com/bruin-data/bruin/pkg/bigquery"
	ch "github.com/bruin-data/bruin/pkg/clickhouse"
	"github.com/bruin-data/bruin/pkg/config"
	dbsql "github.com/bruin-data/bruin/pkg/databricks"
	duck "github.com/bruin-data/bruin/pkg/duckdb"
	bruinexecutor "github.com/bruin-data/bruin/pkg/executor"
	fw "github.com/bruin-data/bruin/pkg/fabric"
	bruiningestr "github.com/bruin-data/bruin/pkg/ingestr"
	"github.com/bruin-data/bruin/pkg/jinja"
	ms "github.com/bruin-data/bruin/pkg/mssql"
	my "github.com/bruin-data/bruin/pkg/mysql"
	"github.com/bruin-data/bruin/pkg/pipeline"
	pg "github.com/bruin-data/bruin/pkg/postgres"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/bruin-data/bruin/pkg/s3"
	"github.com/bruin-data/bruin/pkg/scheduler"
	sf "github.com/bruin-data/bruin/pkg/snowflake"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	tri "github.com/bruin-data/bruin/pkg/trino"
	vert "github.com/bruin-data/bruin/pkg/vertica"
	"github.com/spf13/afero"

	"renart/internal/web/duckcoord"
	"renart/internal/web/runstate"
)

func buildDirectMainExecutors(manager config.ConnectionAndDetailsGetter, renderer *jinja.Renderer, parser *sqlparser.SQLParser, pl *pipeline.Pipeline, registry *runstate.Registry, coordinator *duckcoord.Coordinator, workspaceRoot string, fullRefresh bool) (map[pipeline.AssetType]bruinexecutor.Config, error) {
	executors := make(map[pipeline.AssetType]bruinexecutor.Config, len(bruinexecutor.DefaultExecutorsV2))
	for assetType, cfg := range bruinexecutor.DefaultExecutorsV2 {
		if cfg == nil {
			executors[assetType] = nil
			continue
		}
		cloned := make(bruinexecutor.Config, len(cfg))
		for instanceType, operator := range cfg {
			cloned[instanceType] = operator
		}
		executors[assetType] = cloned
	}
	for assetType := range executors {
		if isDirectRunAssetTypeSupported(assetType) {
			continue
		}
		if executors[assetType] == nil {
			executors[assetType] = bruinexecutor.Config{}
		}
		executors[assetType][scheduler.TaskInstanceTypeMain] = directUnsupportedOperator{assetType: assetType}
	}

	wholeFileExtractor := &query.WholeFileExtractor{Fs: afero.NewOsFs(), Renderer: renderer}
	customCheckRunner := ansisql.NewCustomCheckOperator(manager, renderer)
	seedOperator, err := bruiningestr.NewSeedOperator(manager, renderer)
	if err != nil {
		return nil, err
	}
	ensureExecutorConfig := func(assetType pipeline.AssetType) {
		if executors[assetType] == nil {
			executors[assetType] = bruinexecutor.Config{}
		}
	}
	assignSeedExecutor := func(assetType pipeline.AssetType, columnCheck bruinexecutor.Operator, customCheck bruinexecutor.Operator, metadataPush bruinexecutor.Operator) {
		ensureExecutorConfig(assetType)
		executors[assetType][scheduler.TaskInstanceTypeMain] = seedOperator
		if columnCheck != nil {
			executors[assetType][scheduler.TaskInstanceTypeColumnCheck] = columnCheck
		}
		if customCheck != nil {
			executors[assetType][scheduler.TaskInstanceTypeCustomCheck] = customCheck
		}
		if metadataPush != nil {
			executors[assetType][scheduler.TaskInstanceTypeMetadataPush] = metadataPush
		}
	}

	ensureExecutorConfig(pipeline.AssetTypeDuckDBQuery)
	executors[pipeline.AssetTypeDuckDBQuery][scheduler.TaskInstanceTypeMain] = duck.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: duck.NewMaterializer(fullRefresh),
	}, parser)
	duckColumnCheckOperator := duck.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeDuckDBQuery][scheduler.TaskInstanceTypeColumnCheck] = duckColumnCheckOperator
	executors[pipeline.AssetTypeDuckDBQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeDuckDBSeed, duckColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeMotherduckQuery)
	executors[pipeline.AssetTypeMotherduckQuery][scheduler.TaskInstanceTypeMain] = duck.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: duck.NewMaterializer(fullRefresh),
	}, parser)
	executors[pipeline.AssetTypeMotherduckQuery][scheduler.TaskInstanceTypeColumnCheck] = duck.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeMotherduckQuery][scheduler.TaskInstanceTypeCustomCheck] = ansisql.NewCustomCheckOperator(manager, renderer)
	ensureExecutorConfig(pipeline.AssetTypePostgresQuery)
	executors[pipeline.AssetTypePostgresQuery][scheduler.TaskInstanceTypeMain] = pg.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: pg.NewMaterializer(fullRefresh),
	}, parser)
	pgColumnCheckOperator := pg.NewColumnCheckOperator(manager)
	pgMetadataPushOperator := pg.NewMetadataPushOperator(manager)
	executors[pipeline.AssetTypePostgresQuery][scheduler.TaskInstanceTypeColumnCheck] = pgColumnCheckOperator
	executors[pipeline.AssetTypePostgresQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	executors[pipeline.AssetTypePostgresQuery][scheduler.TaskInstanceTypeMetadataPush] = pgMetadataPushOperator
	assignSeedExecutor(pipeline.AssetTypePostgresSeed, pgColumnCheckOperator, customCheckRunner, pgMetadataPushOperator)
	ensureExecutorConfig(pipeline.AssetTypeRedshiftQuery)
	executors[pipeline.AssetTypeRedshiftQuery][scheduler.TaskInstanceTypeMain] = pg.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: pg.NewMaterializer(fullRefresh),
	}, parser)
	executors[pipeline.AssetTypeRedshiftQuery][scheduler.TaskInstanceTypeColumnCheck] = pgColumnCheckOperator
	executors[pipeline.AssetTypeRedshiftQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeRedshiftSeed, pgColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeBigqueryQuery)
	executors[pipeline.AssetTypeBigqueryQuery][scheduler.TaskInstanceTypeMain] = bq.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: bq.NewMaterializer(fullRefresh),
	}, parser)
	bqColumnCheckOperator, err := bq.NewColumnCheckOperator(manager)
	if err != nil {
		return nil, err
	}
	executors[pipeline.AssetTypeBigqueryQuery][scheduler.TaskInstanceTypeColumnCheck] = bqColumnCheckOperator
	bqMetadataPushOperator := bq.NewMetadataPushOperator(manager)
	executors[pipeline.AssetTypeBigqueryQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	executors[pipeline.AssetTypeBigqueryQuery][scheduler.TaskInstanceTypeMetadataPush] = bqMetadataPushOperator
	assignSeedExecutor(pipeline.AssetTypeBigquerySeed, bqColumnCheckOperator, customCheckRunner, bqMetadataPushOperator)
	ensureExecutorConfig(pipeline.AssetTypeBigqueryQuerySensor)
	executors[pipeline.AssetTypeBigqueryQuerySensor][scheduler.TaskInstanceTypeMain] = bq.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeBigqueryTableSensor)
	executors[pipeline.AssetTypeBigqueryTableSensor][scheduler.TaskInstanceTypeMain] = bq.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeAthenaQuery)
	executors[pipeline.AssetTypeAthenaQuery][scheduler.TaskInstanceTypeMain] = ath.NewBasicOperator(manager, wholeFileExtractor, refreshRestrictedAthenaMaterializer{
		configured: ath.NewMaterializer(false),
		full:       ath.NewMaterializer(fullRefresh),
	}, parser)
	athenaColumnCheckOperator := ath.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeAthenaQuery][scheduler.TaskInstanceTypeColumnCheck] = athenaColumnCheckOperator
	executors[pipeline.AssetTypeAthenaQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeAthenaSeed, athenaColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeAthenaSQLSensor)
	executors[pipeline.AssetTypeAthenaSQLSensor][scheduler.TaskInstanceTypeMain] = ath.NewQuerySensor(manager, renderer, 30)
	ensureExecutorConfig(pipeline.AssetTypeAthenaTableSensor)
	executors[pipeline.AssetTypeAthenaTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeDatabricksQuery)
	executors[pipeline.AssetTypeDatabricksQuery][scheduler.TaskInstanceTypeMain] = dbsql.NewBasicOperator(manager, wholeFileExtractor, refreshRestrictedQueryBatchMaterializer{
		configured: dbsql.NewMaterializer(false),
		full:       dbsql.NewMaterializer(fullRefresh),
	}, parser)
	databricksColumnCheckOperator := dbsql.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeDatabricksQuery][scheduler.TaskInstanceTypeColumnCheck] = databricksColumnCheckOperator
	executors[pipeline.AssetTypeDatabricksQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeDatabricksSeed, databricksColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeDatabricksQuerySensor)
	executors[pipeline.AssetTypeDatabricksQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeDatabricksTableSensor)
	executors[pipeline.AssetTypeDatabricksTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeFabricQuery)
	executors[pipeline.AssetTypeFabricQuery][scheduler.TaskInstanceTypeMain] = fw.NewBasicOperator(manager, wholeFileExtractor, fw.NewMaterializer(fullRefresh), parser)
	fabricColumnCheckOperator := fw.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeFabricQuery][scheduler.TaskInstanceTypeColumnCheck] = fabricColumnCheckOperator
	executors[pipeline.AssetTypeFabricQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeFabricSeed, fabricColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeFabricQuerySensor)
	executors[pipeline.AssetTypeFabricQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeFabricTableSensor)
	executors[pipeline.AssetTypeFabricTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeFabricQueryLegacy)
	executors[pipeline.AssetTypeFabricQueryLegacy][scheduler.TaskInstanceTypeMain] = fw.NewBasicOperator(manager, wholeFileExtractor, fw.NewMaterializer(fullRefresh), parser)
	executors[pipeline.AssetTypeFabricQueryLegacy][scheduler.TaskInstanceTypeColumnCheck] = fabricColumnCheckOperator
	executors[pipeline.AssetTypeFabricQueryLegacy][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeFabricSeedLegacy, fabricColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeFabricQuerySensorLegacy)
	executors[pipeline.AssetTypeFabricQuerySensorLegacy][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeFabricTableSensorLegacy)
	executors[pipeline.AssetTypeFabricTableSensorLegacy][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeMySQLQuery)
	executors[pipeline.AssetTypeMySQLQuery][scheduler.TaskInstanceTypeMain] = my.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: my.NewMaterializer(fullRefresh),
	}, parser)
	mySQLColumnCheckOperator := my.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeMySQLQuery][scheduler.TaskInstanceTypeColumnCheck] = mySQLColumnCheckOperator
	executors[pipeline.AssetTypeMySQLQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeMySQLSeed, mySQLColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeMySQLQuerySensor)
	executors[pipeline.AssetTypeMySQLQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeMySQLTableSensor)
	executors[pipeline.AssetTypeMySQLTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeSnowflakeQuery)
	executors[pipeline.AssetTypeSnowflakeQuery][scheduler.TaskInstanceTypeMain] = sf.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: sf.NewMaterializer(fullRefresh),
	}, parser)
	snowflakeColumnCheckOperator := sf.NewColumnCheckOperator(manager)
	snowflakeMetadataPushOperator := sf.NewMetadataPushOperator(manager)
	executors[pipeline.AssetTypeSnowflakeQuery][scheduler.TaskInstanceTypeColumnCheck] = snowflakeColumnCheckOperator
	executors[pipeline.AssetTypeSnowflakeQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	executors[pipeline.AssetTypeSnowflakeQuery][scheduler.TaskInstanceTypeMetadataPush] = snowflakeMetadataPushOperator
	assignSeedExecutor(pipeline.AssetTypeSnowflakeSeed, snowflakeColumnCheckOperator, customCheckRunner, snowflakeMetadataPushOperator)
	ensureExecutorConfig(pipeline.AssetTypeSnowflakeQuerySensor)
	executors[pipeline.AssetTypeSnowflakeQuerySensor][scheduler.TaskInstanceTypeMain] = sf.NewQuerySensor(manager, wholeFileExtractor, 30)
	ensureExecutorConfig(pipeline.AssetTypeSnowflakeTableSensor)
	executors[pipeline.AssetTypeSnowflakeTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeMsSQLQuery)
	executors[pipeline.AssetTypeMsSQLQuery][scheduler.TaskInstanceTypeMain] = ms.NewBasicOperator(manager, wholeFileExtractor, ms.NewMaterializer(fullRefresh), parser)
	msSQLColumnCheckOperator := ms.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeMsSQLQuery][scheduler.TaskInstanceTypeColumnCheck] = msSQLColumnCheckOperator
	executors[pipeline.AssetTypeMsSQLQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeMsSQLSeed, msSQLColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeMsSQLQuerySensor)
	executors[pipeline.AssetTypeMsSQLQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeMsSQLTableSensor)
	executors[pipeline.AssetTypeMsSQLTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeSynapseQuery)
	executors[pipeline.AssetTypeSynapseQuery][scheduler.TaskInstanceTypeMain] = ms.NewBasicOperator(manager, wholeFileExtractor, ms.NewMaterializer(fullRefresh), parser)
	executors[pipeline.AssetTypeSynapseQuery][scheduler.TaskInstanceTypeColumnCheck] = msSQLColumnCheckOperator
	executors[pipeline.AssetTypeSynapseQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeSynapseSeed, msSQLColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeSynapseQuerySensor)
	executors[pipeline.AssetTypeSynapseQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeSynapseTableSensor)
	executors[pipeline.AssetTypeSynapseTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeClickHouse)
	executors[pipeline.AssetTypeClickHouse][scheduler.TaskInstanceTypeMain] = ch.NewBasicOperator(manager, wholeFileExtractor, refreshRestrictedQueryBatchMaterializer{
		configured: ch.NewMaterializer(false),
		full:       ch.NewMaterializer(fullRefresh),
	}, parser)
	clickHouseColumnCheckOperator := ch.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeClickHouse][scheduler.TaskInstanceTypeColumnCheck] = clickHouseColumnCheckOperator
	executors[pipeline.AssetTypeClickHouse][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeClickHouseSeed, clickHouseColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeClickHouseQuerySensor)
	executors[pipeline.AssetTypeClickHouseQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeClickHouseTableSensor)
	executors[pipeline.AssetTypeClickHouseTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeTrinoQuery)
	executors[pipeline.AssetTypeTrinoQuery][scheduler.TaskInstanceTypeMain] = tri.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: tri.NewMaterializer(fullRefresh),
	}, parser)
	executors[pipeline.AssetTypeTrinoQuery][scheduler.TaskInstanceTypeCustomCheck] = ansisql.NewCustomCheckOperator(manager, renderer)
	ensureExecutorConfig(pipeline.AssetTypeTrinoQuerySensor)
	executors[pipeline.AssetTypeTrinoQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeVerticaQuery)
	executors[pipeline.AssetTypeVerticaQuery][scheduler.TaskInstanceTypeMain] = vert.NewBasicOperator(manager, wholeFileExtractor, vert.NewMaterializer(fullRefresh), parser)
	verticaColumnCheckOperator := vert.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeVerticaQuery][scheduler.TaskInstanceTypeColumnCheck] = verticaColumnCheckOperator
	executors[pipeline.AssetTypeVerticaQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeVerticaSeed, verticaColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeVerticaQuerySensor)
	executors[pipeline.AssetTypeVerticaQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeVerticaTableSensor)
	executors[pipeline.AssetTypeVerticaTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypePostgresQuerySensor)
	executors[pipeline.AssetTypePostgresQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypePostgresTableSensor)
	executors[pipeline.AssetTypePostgresTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeRedshiftQuerySensor)
	executors[pipeline.AssetTypeRedshiftQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeRedshiftTableSensor)
	executors[pipeline.AssetTypeRedshiftTableSensor][scheduler.TaskInstanceTypeMain] = ansisql.NewTableSensor(manager, "once", wholeFileExtractor)
	ensureExecutorConfig(pipeline.AssetTypeDuckDBQuerySensor)
	executors[pipeline.AssetTypeDuckDBQuerySensor][scheduler.TaskInstanceTypeMain] = ansisql.NewQuerySensor(manager, wholeFileExtractor, "once")
	ensureExecutorConfig(pipeline.AssetTypeS3KeySensor)
	executors[pipeline.AssetTypeS3KeySensor][scheduler.TaskInstanceTypeMain] = s3.NewKeySensor(manager, "once")
	ensureExecutorConfig(pipeline.AssetTypeOracleQuery)
	executors[pipeline.AssetTypeOracleQuery][scheduler.TaskInstanceTypeMain] = directOracleBasicOperator{connection: manager, extractor: wholeFileExtractor}
	executors[pipeline.AssetTypeOracleQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	ensureExecutorConfig(pipeline.AssetTypePython)
	executors[pipeline.AssetTypePython][scheduler.TaskInstanceTypeMain] = newRenartPythonOperator(manager, directPythonEnvVariables(pl), renartPythonOperatorOptions{
		registry:          registry,
		enableBroker:      true,
		duckDBCoordinator: coordinator,
		workspaceRoot:     workspaceRoot,
	})
	ingestrOperator, err := bruiningestr.NewBasicOperator(manager, renderer)
	if err != nil {
		return nil, err
	}
	ensureExecutorConfig(pipeline.AssetTypeIngestr)
	executors[pipeline.AssetTypeIngestr][scheduler.TaskInstanceTypeMain] = ingestrOperator
	return executors, nil
}

func directPythonEnvVariables(pl *pipeline.Pipeline) map[string]string {
	if pl == nil {
		return map[string]string{}
	}
	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	startDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 0, time.UTC)
	return jinja.PythonEnvVariables(&startDate, &endDate, &now, pl.Name, "renart-run", false, "")
}

type directOracleBasicOperator struct {
	connection config.ConnectionGetter
	extractor  query.QueryExtractor
}

type directUnsupportedOperator struct {
	assetType pipeline.AssetType
}

func (o directUnsupportedOperator) Run(_ context.Context, _ scheduler.TaskInstance) error {
	return fmt.Errorf("direct execution is not implemented for asset type %q", o.assetType)
}

func (o directOracleBasicOperator) Run(ctx context.Context, ti scheduler.TaskInstance) error {
	return o.RunTask(ctx, ti.GetPipeline(), ti.GetAsset())
}

func (o directOracleBasicOperator) RunTask(ctx context.Context, p *pipeline.Pipeline, asset *pipeline.Asset) error {
	if asset.Materialization.Type != pipeline.MaterializationTypeNone {
		return fmt.Errorf("direct oracle execution only supports assets without materialization")
	}

	extractor, err := o.extractor.CloneForAsset(ctx, p, asset)
	if err != nil {
		return fmt.Errorf("failed to clone extractor for asset %s: %w", asset.Name, err)
	}
	queries, err := extractor.ExtractQueriesFromString(asset.ExecutableFile.Content)
	if err != nil {
		return fmt.Errorf("cannot extract queries from the task file: %w", err)
	}
	if len(queries) == 0 {
		return nil
	}

	connName, err := p.GetConnectionNameForAsset(asset)
	if err != nil {
		return err
	}
	rawConn := o.connection.GetConnection(connName)
	if rawConn == nil {
		return config.NewConnectionNotFoundError(ctx, "", connName)
	}
	conn, ok := rawConn.(interface {
		RunQueryWithoutResult(context.Context, *query.Query) error
	})
	if !ok {
		return fmt.Errorf("connection %q cannot run oracle queries", connName)
	}

	for _, queryToRun := range queries {
		ansisql.LogQueryIfVerbose(ctx, ctx.Value(bruinexecutor.KeyPrinter), queryToRun.Query)
		if err := conn.RunQueryWithoutResult(ctx, queryToRun); err != nil {
			return err
		}
	}
	return nil
}
