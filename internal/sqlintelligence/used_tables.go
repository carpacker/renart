package sqlintelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"renart/internal/sqlformat"
)

// UsedTables returns the table references reported by Polyglot for a SQL
// script. Its compatibility rules intentionally match Bruin's Rust parser:
// write targets are excluded (except CREATE TABLE), unqualified CTE aliases
// are filtered out, and a preceding T-SQL USE statement qualifies subsequent
// table references.
func UsedTables(query, dialect string) ([]string, error) {
	return UsedTablesContext(context.Background(), query, dialect)
}

// UsedTablesContext is the context-aware form of UsedTables.
func UsedTablesContext(ctx context.Context, query, dialect string) ([]string, error) {
	if dialect == "" {
		dialect = sqlformat.DialectGeneric
	}

	parseJSON, err := sqlformat.Call(ctx, "parse", query, dialect)
	if err != nil {
		return nil, err
	}
	var response polyglotParseResponse
	if err := json.Unmarshal([]byte(parseJSON), &response); err != nil {
		return nil, fmt.Errorf("decode Polyglot parse response: %w", err)
	}
	if !response.Success {
		return nil, fmt.Errorf("Polyglot SQL parse failed: %v", response.Error)
	}

	statements, err := decodePolyglotAST(response.AST)
	if err != nil {
		return nil, fmt.Errorf("decode Polyglot AST: %w", err)
	}

	seen := make(map[string]struct{})
	currentDatabase := ""
	for _, statement := range statements {
		kind, body := polyglotWrapper(statement)
		if kind == "use" {
			currentDatabase = polyglotIdentifierName(body["this"])
			continue
		}

		if kind == "create_table" {
			if target, ok := body["name"].(map[string]any); ok {
				addPolyglotTableName(seen, polyglotTableParts(target, polyglotIdentifierName(target["name"])))
			}
		}

		cteAliases := polyglotCTEAliases(statement)
		for _, table := range polyglotWrappers(statement, "table") {
			name := polyglotIdentifierName(table["name"])
			if name == "" {
				continue
			}
			schema := polyglotIdentifierName(table["schema"])
			catalog := polyglotIdentifierName(table["catalog"])
			if schema == "" && catalog == "" {
				if _, isCTE := cteAliases[name]; isCTE {
					continue
				}
			}

			parts := polyglotTableParts(table, name)
			if strings.EqualFold(dialect, "tsql") && currentDatabase != "" {
				parts = polyglotTSQLTableParts(table, currentDatabase, name)
			}
			addPolyglotTableName(seen, parts)
		}

		if strings.EqualFold(dialect, "tsql") {
			for _, function := range polyglotWrappers(statement, "function") {
				if polyglotLooksLikeTSQLTableHint(function) {
					if name, ok := function["name"].(string); ok && name != "" {
						seen[name] = struct{}{}
					}
				}
			}
		}
	}

	tables := make([]string, 0, len(seen))
	for table := range seen {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables, nil
}

func polyglotWrapper(node map[string]any) (string, map[string]any) {
	if len(node) != 1 {
		return "", nil
	}
	for kind, raw := range node {
		body, _ := raw.(map[string]any)
		return kind, body
	}
	return "", nil
}

func polyglotWrappers(node any, wanted string) []map[string]any {
	result := []map[string]any{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []map[string]any:
			for _, item := range typed {
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			if len(typed) == 1 {
				if child, ok := typed[wanted].(map[string]any); ok {
					result = append(result, child)
				}
			}
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(node)
	return result
}

func polyglotCTEAliases(statement any) map[string]struct{} {
	aliases := map[string]struct{}{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case []map[string]any:
			for _, item := range typed {
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			if with, ok := typed["with"].(map[string]any); ok {
				if ctes, ok := with["ctes"].([]any); ok {
					for _, rawCTE := range ctes {
						if cte, ok := rawCTE.(map[string]any); ok {
							if alias := polyglotIdentifierName(cte["alias"]); alias != "" {
								aliases[alias] = struct{}{}
							}
						}
					}
				}
			}
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(statement)
	return aliases
}

func polyglotTSQLTableParts(table map[string]any, currentDatabase, name string) []string {
	parts := []string{}
	if catalog := polyglotIdentifierName(table["catalog"]); catalog != "" {
		parts = append(parts, catalog)
	} else {
		parts = append(parts, currentDatabase)
	}
	if schema := polyglotIdentifierName(table["schema"]); schema != "" {
		parts = append(parts, schema)
	} else {
		parts = append(parts, "dbo")
	}
	return append(parts, name)
}

func addPolyglotTableName(seen map[string]struct{}, parts []string) {
	name := strings.Join(parts, ".")
	if name != "" {
		seen[name] = struct{}{}
	}
}

func polyglotLooksLikeTSQLTableHint(function map[string]any) bool {
	args, ok := function["args"].([]any)
	if !ok || len(args) == 0 || len(args) > 2 {
		return false
	}
	for _, arg := range args {
		wrapper, ok := arg.(map[string]any)
		if !ok {
			return false
		}
		kind, _ := polyglotWrapper(wrapper)
		if kind != "column" {
			return false
		}
	}
	return true
}
