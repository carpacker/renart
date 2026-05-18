package service

import (
	"context"
	"fmt"
	"strings"
)

type DBObjectInfo struct {
	Schema        string
	Name          string
	QualifiedName string
	Kind          string
}

func (s *ExecutionService) fetchObjectsForConnection(ctx context.Context, connectionName, environment string) ([]DBObjectInfo, error) {
	queries := []string{
		`SELECT table_schema, table_name, table_type FROM information_schema.tables`,
		`SHOW TABLES`,
	}

	var rows []map[string]any
	var lastErr error
	for _, query := range queries {
		_, qRows, err := s.RunConnectionQueryForEnvironment(ctx, connectionName, environment, query)
		if err != nil {
			lastErr = err
			continue
		}
		rows = qRows
		break
	}

	if len(rows) == 0 {
		return []DBObjectInfo{}, lastErr
	}

	objects := make([]DBObjectInfo, 0, len(rows))
	for _, row := range rows {
		name := ReadStringField(row, "table_name", "name", "table")
		if name == "" {
			continue
		}

		schema := ReadStringField(row, "table_schema", "schema", "database")
		qualifiedName := name
		if schema != "" {
			qualifiedName = schema + "." + name
		}

		kind := strings.ToLower(ReadStringField(row, "table_type", "type"))
		if strings.Contains(kind, "view") {
			kind = "view"
		} else if kind != "" {
			kind = "table"
		} else {
			kind = "table"
		}

		objects = append(objects, DBObjectInfo{Schema: schema, Name: name, QualifiedName: qualifiedName, Kind: kind})
	}

	return objects, nil
}

func (s *ExecutionService) fetchRowCountsForObjects(ctx context.Context, connectionName, environment string, objects []DBObjectInfo) map[string]int64 {
	result := make(map[string]int64)
	if len(objects) == 0 {
		return result
	}

	queries := make([]string, 0, len(objects))
	for _, object := range objects {
		queries = append(queries, fmt.Sprintf(
			"SELECT '%s' AS object_name, COUNT(*) AS row_count FROM %s",
			EscapeSQLLiteral(object.QualifiedName),
			QuoteQualifiedIdentifier(object.QualifiedName),
		))
	}

	countQuery := strings.Join(queries, " UNION ALL ")
	_, rows, err := s.RunConnectionQueryForEnvironment(ctx, connectionName, environment, countQuery)
	if err != nil {
		return result
	}

	for _, row := range rows {
		objName := ReadStringField(row, "object_name")
		if objName == "" {
			continue
		}

		if count, ok := ReadInt64Field(row, "row_count"); ok {
			result[NormalizeIdentifier(objName)] = count
			parts := strings.Split(NormalizeIdentifier(objName), ".")
			if len(parts) > 1 {
				result[parts[len(parts)-1]] = count
			}
		}
	}

	return result
}
