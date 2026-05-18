package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/ansisql"
	"github.com/bruin-data/bruin/pkg/mssql"
	"github.com/bruin-data/bruin/pkg/oracle"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/bruin-data/bruin/pkg/postgres"
	"github.com/bruin-data/bruin/pkg/query"
)

func createDirectImportedAsset(ctx context.Context, assetsPath, schemaName, tableName string, assetType pipeline.AssetType, conn interface{}, fillColumns bool, table *ansisql.DBTable) (*pipeline.Asset, string) {
	schemaFolder := filepath.Join(assetsPath, strings.ToLower(schemaName))
	isView := table.Type == ansisql.DBTableTypeView && table.ViewDefinition != ""

	var fileName, filePath string
	var materializationType pipeline.MaterializationType
	var content string

	if isView {
		fileName = strings.ToLower(tableName) + ".sql"
		filePath = filepath.Join(schemaFolder, fileName)
		content = table.ViewDefinition
		materializationType = pipeline.MaterializationTypeView
	} else {
		fileName = strings.ToLower(tableName) + ".asset.yml"
		filePath = filepath.Join(schemaFolder, fileName)
	}

	actualAssetType := assetType
	if isView {
		actualAssetType = convertDirectSourceTypeToQueryType(assetType)
	}

	assetName := fmt.Sprintf("%s.%s", strings.ToLower(schemaName), strings.ToLower(tableName))
	asset := &pipeline.Asset{
		Name: assetName,
		Type: actualAssetType,
		ExecutableFile: pipeline.ExecutableFile{
			Name:    fileName,
			Path:    filePath,
			Content: content,
		},
		Description: buildDirectEnhancedDescription(table, schemaName, tableName),
	}

	if isView {
		asset.Materialization = pipeline.Materialization{Type: materializationType}
	}

	if !fillColumns {
		return asset, ""
	}

	if len(table.Columns) > 0 {
		columns := make([]pipeline.Column, 0, len(table.Columns))
		for _, col := range table.Columns {
			columns = append(columns, pipeline.Column{
				Name:        col.Name,
				Type:        col.Type,
				Description: col.Description,
				Checks:      []pipeline.ColumnCheck{},
				Upstreams:   []*pipeline.UpstreamColumn{},
			})
		}
		asset.Columns = columns
		return asset, ""
	}

	if err := fillDirectAssetColumnsFromDB(ctx, asset, conn, schemaName, tableName); err != nil {
		return asset, fmt.Sprintf("Could not fill columns: %v", err)
	}

	return asset, ""
}

func fillDirectAssetColumnsFromDB(ctx context.Context, asset *pipeline.Asset, conn interface{}, schemaName, tableName string) error {
	querier, ok := conn.(interface {
		SelectWithSchema(context.Context, *query.Query) (*query.QueryResult, error)
	})
	if !ok {
		return fmt.Errorf("connection does not support schema introspection")
	}

	fullTableName := schemaName + "." + tableName
	if _, ok := conn.(*postgres.Client); ok {
		fullTableName = postgres.QuoteIdentifier(fullTableName)
	}
	if _, ok := conn.(*mssql.DB); ok {
		fullTableName = mssql.QuoteIdentifier(fullTableName)
	}

	queryStr := fmt.Sprintf("SELECT * FROM %s WHERE 1=0 LIMIT 0", fullTableName)
	if _, ok := conn.(*mssql.DB); ok {
		queryStr = "SELECT TOP 0 * FROM " + fullTableName
	} else if _, ok := conn.(*oracle.Client); ok {
		queryStr = "SELECT * FROM " + fullTableName + " WHERE 1=0"
	}

	result, err := querier.SelectWithSchema(ctx, &query.Query{Query: queryStr})
	if err != nil {
		return err
	}
	if len(result.Columns) == 0 {
		return fmt.Errorf("no columns found for table %s.%s", schemaName, tableName)
	}

	descriptions := fetchDirectColumnDescriptions(ctx, conn, schemaName, tableName)
	skipColumns := map[string]bool{"_IS_CURRENT": true, "_VALID_UNTIL": true, "_VALID_FROM": true}
	columns := make([]pipeline.Column, 0, len(result.Columns))
	for i, colName := range result.Columns {
		if skipColumns[colName] {
			continue
		}
		colType := ""
		if i < len(result.ColumnTypes) {
			colType = result.ColumnTypes[i]
		}
		columns = append(columns, pipeline.Column{
			Name:        colName,
			Type:        colType,
			Description: descriptions[colName],
			Checks:      []pipeline.ColumnCheck{},
			Upstreams:   []*pipeline.UpstreamColumn{},
		})
	}
	asset.Columns = columns
	return nil
}

func fetchDirectColumnDescriptions(ctx context.Context, conn interface{}, schemaName, tableName string) map[string]string {
	descriptions := make(map[string]string)
	selector, ok := conn.(interface {
		Select(context.Context, *query.Query) ([][]interface{}, error)
	})
	if !ok {
		return descriptions
	}

	var queryStr string
	switch conn.(type) {
	case *postgres.Client:
		queryStr = fmt.Sprintf(`
SELECT a.attname as column_name, pg_catalog.col_description(a.attrelid, a.attnum) as column_description
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON a.attrelid = c.oid
JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
WHERE n.nspname = '%s' AND c.relname = '%s' AND a.attnum > 0 AND NOT a.attisdropped
AND pg_catalog.col_description(a.attrelid, a.attnum) IS NOT NULL
`, schemaName, tableName)
	case *mssql.DB:
		queryStr = fmt.Sprintf(`
SELECT c.name AS column_name, CAST(ep.value AS NVARCHAR(MAX)) AS column_description
FROM sys.columns c
JOIN sys.tables t ON c.object_id = t.object_id
JOIN sys.schemas s ON t.schema_id = s.schema_id
LEFT JOIN sys.extended_properties ep ON c.object_id = ep.major_id AND c.column_id = ep.minor_id AND ep.name = 'MS_Description'
WHERE s.name = '%s' AND t.name = '%s' AND ep.value IS NOT NULL
`, schemaName, tableName)
	default:
		return descriptions
	}

	rows, err := selector.Select(ctx, &query.Query{Query: queryStr})
	if err != nil {
		return descriptions
	}
	for _, row := range rows {
		if len(row) >= 2 {
			colName, ok1 := row[0].(string)
			desc, ok2 := row[1].(string)
			if ok1 && ok2 {
				descriptions[colName] = desc
			}
		}
	}
	return descriptions
}

func buildDirectEnhancedDescription(table *ansisql.DBTable, schemaName, tableName string) string {
	var parts []string
	if table.Description != "" {
		parts = append(parts, table.Description, "")
	}
	parts = append(parts, "Imported "+directTableTypeDescription(table.Type)+": "+schemaName+"."+tableName)
	parts = append(parts, "Extracted at: "+time.Now().UTC().Format(time.RFC3339))
	if table.CreatedAt != nil {
		parts = append(parts, "Created at: "+table.CreatedAt.UTC().Format(time.RFC3339))
	}
	if table.LastModified != nil {
		parts = append(parts, "Last modified: "+table.LastModified.UTC().Format(time.RFC3339))
	}
	if table.RowCount != nil {
		parts = append(parts, "Row count: "+formatDirectNumber(*table.RowCount))
	}
	if table.SizeBytes != nil {
		parts = append(parts, "Size: "+formatDirectBytes(*table.SizeBytes))
	}
	if table.Owner != "" {
		parts = append(parts, "Owner: "+table.Owner)
	}
	return strings.Join(parts, "\n")
}

func directTableTypeDescription(tableType ansisql.DBTableType) string {
	if tableType == ansisql.DBTableTypeView {
		return "view"
	}
	return "table"
}

func formatDirectNumber(n int64) string {
	if n < 1000 {
		return strconv.FormatInt(n, 10)
	}
	s := strconv.FormatInt(n, 10)
	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}

func formatDirectBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d bytes", bytes)
	}
}

const (
	fillStatusUpdated = "updated"
	fillStatusSkipped = "skipped"
	fillStatusFailed  = "failed"
)

func matchesDirectImportedTable(selectedTables map[string]bool, databaseName, schemaName, tableName string) bool {
	if len(selectedTables) == 0 {
		return true
	}

	candidates := []string{
		strings.ToLower(strings.TrimSpace(fmt.Sprintf("%s.%s", schemaName, tableName))),
		strings.ToLower(strings.TrimSpace(fmt.Sprintf("%s.%s.%s", databaseName, schemaName, tableName))),
		strings.ToLower(strings.TrimSpace(tableName)),
	}

	for _, candidate := range candidates {
		if candidate != "" && selectedTables[candidate] {
			return true
		}
	}

	return false
}
