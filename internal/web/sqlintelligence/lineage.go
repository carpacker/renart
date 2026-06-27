package sqlintelligence

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"renart/internal/web/sqlformat"
)

// AnnotateOutputColumns derives the output columns (name + type) of a SELECT
// from the query text and a schema built from the upstream *asset definitions*
// — not from querying the warehouse. It drives the polyglot engine's
// `annotate_types` function, which returns a type-annotated AST: computed
// expressions carry an `inferred_type`, while bare column references are
// resolved against the provided schema.
//
// This is the asset-as-source-of-truth column derivation: render an asset's SQL,
// pass its upstream assets' declared columns as the schema, and read back the
// projected columns with types.
func AnnotateOutputColumns(ctx context.Context, query, dialect string, schema Schema) ([]SchemaColumn, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	polySchema := polyglotSchema{Tables: make([]polyglotSchemaTable, 0, len(schema))}
	for tableName, columns := range schema {
		table := polyglotSchemaTable{Name: tableName, Columns: make([]polyglotSchemaColumn, 0, len(columns))}
		for columnName, columnType := range columns {
			table.Columns = append(table.Columns, polyglotSchemaColumn{Name: columnName, Type: columnType})
		}
		polySchema.Tables = append(polySchema.Tables, table)
	}
	schemaJSON, err := json.Marshal(polySchema)
	if err != nil {
		return nil, err
	}

	raw, err := sqlformat.Call(ctx, "annotate_types", query, dialect, string(schemaJSON))
	if err != nil {
		return nil, err
	}

	var response struct {
		Success bool   `json:"success"`
		AST     []any  `json:"ast"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, err
	}
	if !response.Success {
		if response.Error != "" {
			return nil, errors.New(response.Error)
		}
		return nil, errors.New("annotate_types failed")
	}

	selectNode := findPolyglotSelect(response.AST)
	if selectNode == nil {
		return nil, nil
	}
	sourceTables := polyglotSelectSourceTables(selectNode)

	expressions, _ := selectNode["expressions"].([]any)
	columns := make([]SchemaColumn, 0, len(expressions))
	for _, expression := range expressions {
		if isPolyglotStar(expression) {
			for _, tableName := range sourceTables {
				for _, column := range schemaColumns(schema[tableName]) {
					columns = appendUniqueSchemaColumn(columns, column)
				}
			}
			continue
		}

		name := polyglotExpressionOutputName(expression)
		if name == "" {
			continue
		}
		columns = appendUniqueSchemaColumn(columns, SchemaColumn{
			Name: name,
			Type: outputColumnType(expression, sourceTables, schema),
		})
	}
	return columns, nil
}

// outputColumnType resolves a projected expression's type: the annotated
// inferred_type for a computed expression, otherwise a schema lookup for a
// (possibly aliased) bare column reference.
func outputColumnType(expression any, sourceTables []string, schema Schema) string {
	mapExpression, ok := expression.(map[string]any)
	if !ok {
		return ""
	}

	if aliasNode, ok := mapExpression["alias"].(map[string]any); ok {
		if t := inferredTypeName(aliasNode["inferred_type"]); t != "" {
			return t
		}
		// An alias over a bare column carries no inferred_type; resolve the
		// underlying column against the schema.
		if column := underlyingColumnName(aliasNode["this"]); column != "" {
			return schemaColumnType(column, sourceTables, schema)
		}
		// The engine does not annotate types inside UNION branches; fall back to
		// the literal's own type when the projection is a literal.
		if t := literalTypeName(aliasNode["this"]); t != "" {
			return t
		}
		return ""
	}

	if columnNode, ok := mapExpression["column"].(map[string]any); ok {
		if column := polyglotIdentifierName(columnNode["name"]); column != "" {
			return schemaColumnType(column, sourceTables, schema)
		}
	}
	return ""
}

func underlyingColumnName(node any) string {
	mapNode, ok := node.(map[string]any)
	if !ok {
		return ""
	}
	if columnNode, ok := mapNode["column"].(map[string]any); ok {
		return polyglotIdentifierName(columnNode["name"])
	}
	return ""
}

func schemaColumnType(columnName string, sourceTables []string, schema Schema) string {
	for _, tableName := range sourceTables {
		for name, columnType := range schema[tableName] {
			if strings.EqualFold(name, columnName) {
				return columnType
			}
		}
	}
	return ""
}

// literalTypeName derives a SQL type from a literal projection node, used when
// the engine leaves a projection un-annotated (e.g. inside UNION branches).
func literalTypeName(node any) string {
	mapNode, ok := node.(map[string]any)
	if !ok {
		return ""
	}
	literal, ok := mapNode["literal"].(map[string]any)
	if !ok {
		return ""
	}
	switch literal["literal_type"] {
	case "number":
		if value, _ := literal["value"].(string); strings.Contains(value, ".") {
			return "DOUBLE"
		}
		return "INTEGER"
	case "string":
		return "VARCHAR"
	case "boolean":
		return "BOOLEAN"
	default:
		return ""
	}
}

func inferredTypeName(node any) string {
	mapNode, ok := node.(map[string]any)
	if !ok {
		return ""
	}
	dataType, _ := mapNode["data_type"].(string)
	return normalizeInferredType(dataType)
}

// normalizeInferredType maps the polyglot engine's snake_case DataType tokens to
// conventional SQL type names.
func normalizeInferredType(dataType string) string {
	switch strings.ToLower(strings.TrimSpace(dataType)) {
	case "":
		return ""
	case "int", "int32":
		return "INTEGER"
	case "bigint", "int64":
		return "BIGINT"
	case "smallint", "int16":
		return "SMALLINT"
	case "tinyint", "int8":
		return "TINYINT"
	case "var_char":
		return "VARCHAR"
	case "double":
		return "DOUBLE"
	case "float":
		return "FLOAT"
	case "boolean", "bool":
		return "BOOLEAN"
	case "timestamp_tz", "timestamptz":
		return "TIMESTAMPTZ"
	default:
		return strings.ToUpper(strings.ReplaceAll(dataType, "_", ""))
	}
}
