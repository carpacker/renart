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
	"github.com/bruin-data/bruin/pkg/doris"
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
	"github.com/bruin-data/bruin/pkg/redshift"
	"github.com/bruin-data/bruin/pkg/s3"
	"github.com/bruin-data/bruin/pkg/sail"
	"github.com/bruin-data/bruin/pkg/scheduler"
	sf "github.com/bruin-data/bruin/pkg/snowflake"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	tri "github.com/bruin-data/bruin/pkg/trino"
	vert "github.com/bruin-data/bruin/pkg/vertica"
	"github.com/spf13/afero"

	"renart/internal/web/duckcoord"
	"renart/internal/web/runstate"
)

func buildDirectMainExecutors(manager config.ConnectionAndDetailsGetter, renderer *jinja.Renderer, parser *sqlparser.SQLParser, pl *pipeline.Pipeline, registry *runstate.Registry, coordinator *duckcoord.Coordinator, workspaceRoot string, fullRefresh bool, sensorMode string) (map[pipeline.AssetType]bruinexecutor.Config, error) {
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
	seedOperator := newSlingSeedOperator(manager, renderer, workspaceRoot)
	ensureExecutorConfig := func(assetType pipeline.AssetType) {
		if executors[assetType] == nil {
			executors[assetType] = bruinexecutor.Config{}
		}
	}
	assignExecutor := func(assetType pipeline.AssetType, main bruinexecutor.Operator, columnCheck bruinexecutor.Operator, customCheck bruinexecutor.Operator, metadataPush bruinexecutor.Operator) {
		ensureExecutorConfig(assetType)
		executors[assetType][scheduler.TaskInstanceTypeMain] = main
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
	assignSeedExecutor := func(assetType pipeline.AssetType, columnCheck bruinexecutor.Operator, customCheck bruinexecutor.Operator, metadataPush bruinexecutor.Operator) {
		assignExecutor(assetType, seedOperator, columnCheck, customCheck, metadataPush)
	}
	assignSensorExecutor := func(assetType pipeline.AssetType, main bruinexecutor.Operator, columnCheck bruinexecutor.Operator, customCheck bruinexecutor.Operator, metadataPush bruinexecutor.Operator) {
		assignExecutor(assetType, main, columnCheck, customCheck, metadataPush)
	}

	ensureExecutorConfig(pipeline.AssetTypeDuckDBQuery)
	executors[pipeline.AssetTypeDuckDBQuery][scheduler.TaskInstanceTypeMain] = duck.NewBasicOperator(manager, wholeFileExtractor, newDirectDuckDBHookMaterializer(fullRefresh), parser)
	duckColumnCheckOperator := duck.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeDuckDBQuery][scheduler.TaskInstanceTypeColumnCheck] = duckColumnCheckOperator
	executors[pipeline.AssetTypeDuckDBQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeDuckDBSeed, duckColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeMotherduckQuery)
	executors[pipeline.AssetTypeMotherduckQuery][scheduler.TaskInstanceTypeMain] = duck.NewBasicOperator(manager, wholeFileExtractor, newDirectDuckDBHookMaterializer(fullRefresh), parser)
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
	assignSensorExecutor(pipeline.AssetTypeBigqueryQuerySensor, bq.NewQuerySensor(manager, wholeFileExtractor, sensorMode), bqColumnCheckOperator, customCheckRunner, bqMetadataPushOperator)
	assignSensorExecutor(pipeline.AssetTypeBigqueryTableSensor, bq.NewTableSensor(manager, sensorMode, wholeFileExtractor), bqColumnCheckOperator, customCheckRunner, bqMetadataPushOperator)
	ensureExecutorConfig(pipeline.AssetTypeAthenaQuery)
	executors[pipeline.AssetTypeAthenaQuery][scheduler.TaskInstanceTypeMain] = ath.NewBasicOperator(manager, wholeFileExtractor, refreshRestrictedAthenaMaterializer{
		configured: ath.NewMaterializer(false),
		full:       ath.NewMaterializer(fullRefresh),
	}, parser)
	athenaColumnCheckOperator := ath.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeAthenaQuery][scheduler.TaskInstanceTypeColumnCheck] = athenaColumnCheckOperator
	executors[pipeline.AssetTypeAthenaQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeAthenaSeed, athenaColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeAthenaSQLSensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), athenaColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeAthenaTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), athenaColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeDatabricksQuery)
	executors[pipeline.AssetTypeDatabricksQuery][scheduler.TaskInstanceTypeMain] = dbsql.NewBasicOperator(manager, wholeFileExtractor, refreshRestrictedQueryBatchMaterializer{
		configured: dbsql.NewMaterializer(false),
		full:       dbsql.NewMaterializer(fullRefresh),
	}, parser)
	databricksColumnCheckOperator := dbsql.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeDatabricksQuery][scheduler.TaskInstanceTypeColumnCheck] = databricksColumnCheckOperator
	executors[pipeline.AssetTypeDatabricksQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeDatabricksSeed, databricksColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeDatabricksQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), databricksColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeDatabricksTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), databricksColumnCheckOperator, customCheckRunner, nil)
	dorisColumnCheckOperator := doris.NewColumnCheckOperator(manager)
	assignSeedExecutor(pipeline.AssetTypeDorisSeed, dorisColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeDorisQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), dorisColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeDorisTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), dorisColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeFabricQuery)
	executors[pipeline.AssetTypeFabricQuery][scheduler.TaskInstanceTypeMain] = fw.NewBasicOperator(manager, wholeFileExtractor, fw.NewMaterializer(fullRefresh), parser)
	fabricColumnCheckOperator := fw.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeFabricQuery][scheduler.TaskInstanceTypeColumnCheck] = fabricColumnCheckOperator
	executors[pipeline.AssetTypeFabricQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeFabricSeed, fabricColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeFabricQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), fabricColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeFabricTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), fabricColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeFabricQueryLegacy)
	executors[pipeline.AssetTypeFabricQueryLegacy][scheduler.TaskInstanceTypeMain] = fw.NewBasicOperator(manager, wholeFileExtractor, fw.NewMaterializer(fullRefresh), parser)
	executors[pipeline.AssetTypeFabricQueryLegacy][scheduler.TaskInstanceTypeColumnCheck] = fabricColumnCheckOperator
	executors[pipeline.AssetTypeFabricQueryLegacy][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeFabricSeedLegacy, fabricColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeFabricQuerySensorLegacy, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), fabricColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeFabricTableSensorLegacy, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), fabricColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeMySQLQuery)
	executors[pipeline.AssetTypeMySQLQuery][scheduler.TaskInstanceTypeMain] = my.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: my.NewMaterializer(fullRefresh),
	}, parser)
	mySQLColumnCheckOperator := my.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeMySQLQuery][scheduler.TaskInstanceTypeColumnCheck] = mySQLColumnCheckOperator
	executors[pipeline.AssetTypeMySQLQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeMySQLSeed, mySQLColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeMySQLQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), mySQLColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeMySQLTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), mySQLColumnCheckOperator, customCheckRunner, nil)
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
	assignSensorExecutor(pipeline.AssetTypeSnowflakeQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), snowflakeColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeSnowflakeTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), snowflakeColumnCheckOperator, customCheckRunner, snowflakeMetadataPushOperator)
	ensureExecutorConfig(pipeline.AssetTypeMsSQLQuery)
	executors[pipeline.AssetTypeMsSQLQuery][scheduler.TaskInstanceTypeMain] = ms.NewBasicOperator(manager, wholeFileExtractor, ms.NewMaterializer(fullRefresh), parser)
	msSQLColumnCheckOperator := ms.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeMsSQLQuery][scheduler.TaskInstanceTypeColumnCheck] = msSQLColumnCheckOperator
	executors[pipeline.AssetTypeMsSQLQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeMsSQLSeed, msSQLColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeMsSQLQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), msSQLColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeMsSQLTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), msSQLColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeSynapseQuery)
	executors[pipeline.AssetTypeSynapseQuery][scheduler.TaskInstanceTypeMain] = ms.NewBasicOperator(manager, wholeFileExtractor, ms.NewMaterializer(fullRefresh), parser)
	executors[pipeline.AssetTypeSynapseQuery][scheduler.TaskInstanceTypeColumnCheck] = msSQLColumnCheckOperator
	executors[pipeline.AssetTypeSynapseQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeSynapseSeed, msSQLColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeSynapseQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), msSQLColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeSynapseTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), msSQLColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeClickHouse)
	executors[pipeline.AssetTypeClickHouse][scheduler.TaskInstanceTypeMain] = ch.NewBasicOperator(manager, wholeFileExtractor, refreshRestrictedQueryBatchMaterializer{
		configured: ch.NewMaterializer(false),
		full:       ch.NewMaterializer(fullRefresh),
	}, parser)
	clickHouseColumnCheckOperator := ch.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeClickHouse][scheduler.TaskInstanceTypeColumnCheck] = clickHouseColumnCheckOperator
	executors[pipeline.AssetTypeClickHouse][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeClickHouseSeed, clickHouseColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeClickHouseQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), clickHouseColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeClickHouseTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), clickHouseColumnCheckOperator, customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeTrinoQuery)
	executors[pipeline.AssetTypeTrinoQuery][scheduler.TaskInstanceTypeMain] = tri.NewBasicOperator(manager, wholeFileExtractor, pipeline.HookWrapperMaterializer{
		Mat: tri.NewMaterializer(fullRefresh),
	}, parser)
	trinoColumnCheckOperator := ath.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeTrinoQuery][scheduler.TaskInstanceTypeColumnCheck] = trinoColumnCheckOperator
	executors[pipeline.AssetTypeTrinoQuery][scheduler.TaskInstanceTypeCustomCheck] = ansisql.NewCustomCheckOperator(manager, renderer)
	assignSeedExecutor(assetTypeTrinoSeed, trinoColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeTrinoQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), trinoColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeDremioQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), ath.NewColumnCheckOperator(manager), customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeSailQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), sail.NewColumnCheckOperator(manager), customCheckRunner, nil)
	ensureExecutorConfig(pipeline.AssetTypeVerticaQuery)
	executors[pipeline.AssetTypeVerticaQuery][scheduler.TaskInstanceTypeMain] = vert.NewBasicOperator(manager, wholeFileExtractor, vert.NewMaterializer(fullRefresh), parser)
	verticaColumnCheckOperator := vert.NewColumnCheckOperator(manager)
	executors[pipeline.AssetTypeVerticaQuery][scheduler.TaskInstanceTypeColumnCheck] = verticaColumnCheckOperator
	executors[pipeline.AssetTypeVerticaQuery][scheduler.TaskInstanceTypeCustomCheck] = customCheckRunner
	assignSeedExecutor(pipeline.AssetTypeVerticaSeed, verticaColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeVerticaQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), verticaColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeVerticaTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), verticaColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypePostgresQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), pgColumnCheckOperator, customCheckRunner, pgMetadataPushOperator)
	assignSensorExecutor(pipeline.AssetTypePostgresTableSensor, ansisql.NewTableSensor(manager, sensorMode, wholeFileExtractor), pgColumnCheckOperator, customCheckRunner, pgMetadataPushOperator)
	assignSensorExecutor(pipeline.AssetTypeRedshiftQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), pgColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeRedshiftTableSensor, redshift.NewTableSensor(manager, sensorMode, wholeFileExtractor), pgColumnCheckOperator, customCheckRunner, pgMetadataPushOperator)
	assignSensorExecutor(pipeline.AssetTypeDuckDBQuerySensor, ansisql.NewQuerySensor(manager, wholeFileExtractor, sensorMode), duckColumnCheckOperator, customCheckRunner, nil)
	assignSensorExecutor(pipeline.AssetTypeS3KeySensor, s3.NewKeySensor(manager, sensorMode), nil, nil, nil)
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

func newDirectDuckDBHookMaterializer(fullRefresh bool) pipeline.HookWrapperMaterializer {
	hoister, _ := sqlparser.NewRustSQLParser(false)
	return pipeline.HookWrapperMaterializer{
		Mat:     duck.NewMaterializer(fullRefresh),
		Hoister: hoister,
	}
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
