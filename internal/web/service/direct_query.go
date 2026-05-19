package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/query"
	"github.com/bruin-data/bruin/pkg/sqlparser"
	"github.com/spf13/afero"
)

func (e *HybridBruinExecutor) QueryAsset(ctx context.Context, req QueryAssetRequest) ([]byte, error) {
	if e.newPipelineBuilder == nil {
		return nil, fmt.Errorf("direct asset query requires a pipeline builder")
	}

	pp, err := getDirectPipelineAndAsset(ctx, e.workspaceRoot, req.AssetPath, afero.NewOsFs())
	if err != nil {
		output, marshalErr := json.Marshal(directErrorResponse{Error: err.Error()})
		if marshalErr != nil {
			return nil, err
		}
		return output, err
	}

	if !pp.Asset.IsSQLAsset() {
		err := fmt.Errorf("asset '%s' is not a SQL asset (type: %s). Only SQL assets can be queried", req.AssetPath, pp.Asset.Type)
		output, marshalErr := json.Marshal(directErrorResponse{Error: err.Error()})
		if marshalErr != nil {
			return nil, err
		}
		return output, err
	}

	connName, conn, queryStr, err := e.buildDirectAssetQuery(ctx, pp, req.Environment)
	if err != nil {
		output, marshalErr := json.Marshal(directErrorResponse{Error: err.Error()})
		if marshalErr != nil {
			return nil, err
		}
		return output, err
	}

	dialect, err := sqlparser.AssetTypeToDialect(pp.Asset.Type)
	if err != nil {
		dialect = ""
	}

	if dialect != "" {
		isSelect, selectErr := isReadOnlySelectQuery(queryStr, pp.Asset.Type)
		if selectErr == nil && !isSelect {
			output, marshalErr := json.Marshal(directErrorResponse{Error: inspectReadOnlyErrorMessage})
			if marshalErr != nil {
				return nil, fmt.Errorf(inspectReadOnlyErrorMessage)
			}
			return output, fmt.Errorf(inspectReadOnlyErrorMessage)
		}
	}

	var parser *sqlparser.SQLParser
	needsParser := strings.TrimSpace(req.Limit) != "" || pp.Config.SelectedEnvironment.SchemaPrefix != ""
	if needsParser {
		parser, err = sqlparser.NewSQLParser(false)
		if err != nil {
			wrappedErr := fmt.Errorf("failed to initialize SQL parser: %w", err)
			output, marshalErr := json.Marshal(directErrorResponse{Error: wrappedErr.Error()})
			if marshalErr != nil {
				return nil, wrappedErr
			}
			return output, wrappedErr
		}
		defer parser.Close()
		if err := parser.Start(); err != nil {
			wrappedErr := fmt.Errorf("failed to start SQL parser: %w", err)
			output, marshalErr := json.Marshal(directErrorResponse{Error: wrappedErr.Error()})
			if marshalErr != nil {
				return nil, wrappedErr
			}
			return output, wrappedErr
		}
	}

	if parser != nil && pp.Config.SelectedEnvironment.SchemaPrefix != "" {
		queryStr, err = applyDirectSchemaPrefix(ctx, queryStr, dialect, parser, pp, conn)
		if err != nil {
			wrappedErr := fmt.Errorf("failed to apply schema prefix: %w", err)
			output, marshalErr := json.Marshal(directErrorResponse{Error: wrappedErr.Error()})
			if marshalErr != nil {
				return nil, wrappedErr
			}
			return output, wrappedErr
		}
	}

	if parser != nil && strings.TrimSpace(req.Limit) != "" {
		limitValue, convErr := strconv.ParseInt(strings.TrimSpace(req.Limit), 10, 64)
		if convErr == nil {
			queryStr = addDirectLimitToQuery(queryStr, limitValue, conn, parser, dialect)
		}
	}

	querier, ok := conn.(interface {
		SelectWithSchema(context.Context, *query.Query) (*query.QueryResult, error)
	})
	if !ok {
		err := fmt.Errorf("connection type %s does not support querying", connName)
		output, marshalErr := json.Marshal(directErrorResponse{Error: err.Error()})
		if marshalErr != nil {
			return nil, err
		}
		return output, err
	}

	result, err := querier.SelectWithSchema(ctx, &query.Query{Query: queryStr})
	if err != nil {
		wrappedErr := fmt.Errorf("query execution failed: %w", err)
		output, marshalErr := json.Marshal(directErrorResponse{Error: wrappedErr.Error()})
		if marshalErr != nil {
			return nil, wrappedErr
		}
		return output, wrappedErr
	}

	response := NewQueryResultDTO(result, connName, queryStr)
	return json.Marshal(response)
}

func (e *HybridBruinExecutor) QueryConnection(ctx context.Context, req QueryConnectionRequest) ([]byte, error) {
	if e.newConnectionManager == nil {
		return nil, fmt.Errorf("direct connection query requires a connection manager")
	}

	manager, err := e.newConnectionManager(ctx, req.Environment)
	if err != nil {
		return nil, err
	}

	conn := manager.GetConnection(req.ConnectionName)
	if conn == nil {
		return nil, fmt.Errorf("connection %q not found", req.ConnectionName)
	}

	querier, ok := conn.(interface {
		SelectWithSchema(context.Context, *query.Query) (*query.QueryResult, error)
	})
	if !ok {
		return nil, fmt.Errorf("connection %q does not support querying", req.ConnectionName)
	}

	result, err := querier.SelectWithSchema(ctx, &query.Query{Query: req.Query})
	if err != nil {
		return nil, err
	}

	response := NewQueryResultDTO(result, req.ConnectionName, req.Query)

	if strings.EqualFold(strings.TrimSpace(req.Output), "json") || strings.TrimSpace(req.Output) == "" {
		return json.Marshal(response)
	}

	return nil, fmt.Errorf("direct connection query only supports json output")
}

func (e *HybridBruinExecutor) buildDirectAssetQuery(ctx context.Context, pp *directPipelineInfo, environment string) (string, interface{}, string, error) {
	if strings.TrimSpace(environment) != "" {
		if _, err := selectConfigEnvironment(pp.Config, environment); err != nil {
			return "", nil, "", fmt.Errorf("failed to use the environment '%s': %w", environment, err)
		}
	}

	var manager config.ConnectionAndDetailsGetter
	if e.newConnectionManager != nil {
		var err error
		manager, err = e.newConnectionManager(ctx, pp.Config.SelectedEnvironmentName)
		if err != nil {
			return "", nil, "", fmt.Errorf("failed to create connection manager: %w", err)
		}
	} else {
		connectionManager, err := newConnectionManagerFromConfig(ctx, pp.Config)
		if err != nil {
			return "", nil, "", fmt.Errorf("failed to create connection manager: %w", err)
		}
		manager = connectionManager
	}

	connName, err := pp.Pipeline.GetConnectionNameForAsset(pp.Asset)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to get connection: %w", err)
	}
	conn := manager.GetConnection(connName)
	if conn == nil {
		return "", nil, "", fmt.Errorf("connection %q not found", connName)
	}

	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)
	startDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 0, time.UTC)
	renderer := jinja.NewRendererWithStartEndDates(&startDate, &endDate, &now, pp.Pipeline.Name, "your-run-id", nil)
	fetchCtx := context.WithValue(ctx, pipeline.RunConfigStartDate, startDate)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigEndDate, endDate)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigExecutionDate, now)
	fetchCtx = context.WithValue(fetchCtx, pipeline.RunConfigRunID, "your-run-id")
	fetchCtx = context.WithValue(fetchCtx, config.EnvironmentContextKey, pp.Config.SelectedEnvironment)

	extractor := &query.WholeFileExtractor{Fs: afero.NewOsFs(), Renderer: renderer}
	clonedExtractor, err := extractor.CloneForAsset(fetchCtx, pp.Pipeline, pp.Asset)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to clone extractor for asset %s: %w", pp.Asset.Name, err)
	}

	queries, err := clonedExtractor.ExtractQueriesFromString(pp.Asset.ExecutableFile.Content)
	if err != nil {
		return "", nil, "", fmt.Errorf("failed to extract query: %w", err)
	}
	if len(queries) == 0 {
		return "", nil, "", fmt.Errorf("no query found in asset")
	}

	return connName, conn, queries[0].Query, nil
}

func addDirectLimitToQuery(queryStr string, limit int64, conn interface{}, parser *sqlparser.SQLParser, dialect string) string {
	if parser != nil {
		isSingleSelect, err := parser.IsSingleSelectQuery(queryStr, dialect)
		if err == nil && !isSingleSelect {
			return queryStr
		}
	}

	if parser != nil {
		limitedQuery, err := parser.AddLimit(queryStr, int(limit), dialect)
		if err == nil {
			return limitedQuery
		}
	}

	if limiter, ok := conn.(interface{ Limit(string, int64) string }); ok {
		return limiter.Limit(queryStr, limit)
	}

	queryStr = strings.TrimRight(queryStr, "; \n\t")
	return fmt.Sprintf("SELECT * FROM (\n%s\n) as t LIMIT %d", queryStr, limit)
}

func applyDirectSchemaPrefix(_ context.Context, queryStr, dialect string, parser *sqlparser.SQLParser, pp *directPipelineInfo, conn interface{}) (string, error) {
	if dialect == "" || pp.Config.SelectedEnvironment == nil || pp.Config.SelectedEnvironment.SchemaPrefix == "" {
		return queryStr, nil
	}

	usedTables, err := parser.UsedTables(queryStr, dialect)
	if err != nil {
		return queryStr, nil
	}
	if len(usedTables) == 0 {
		return queryStr, nil
	}

	_ = conn
	renameMapping := map[string]string{}
	for _, tableReference := range usedTables {
		parts := strings.Split(tableReference, ".")
		if len(parts) != 2 {
			continue
		}
		schemaName := parts[0]
		tableName := parts[1]
		renameMapping[tableReference] = fmt.Sprintf("%s%s.%s", pp.Config.SelectedEnvironment.SchemaPrefix, schemaName, tableName)
	}
	if len(renameMapping) == 0 {
		return queryStr, nil
	}

	rewrittenQuery, err := parser.RenameTables(queryStr, dialect, renameMapping)
	if err != nil {
		return "", fmt.Errorf("failed to rewrite query with schema prefix: %w", err)
	}
	return rewrittenQuery, nil
}
