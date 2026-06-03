package sqlintelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"renart/internal/web/sqlformat"
)

type polyglotParseResponse struct {
	Success bool            `json:"success"`
	AST     json.RawMessage `json:"ast"`
	Error   any             `json:"error"`
}

type polyglotTokenizeResponse struct {
	Success bool            `json:"success"`
	Tokens  []polyglotToken `json:"tokens"`
	Error   any             `json:"error"`
}

type polyglotToken struct {
	Type string       `json:"token_type"`
	Text string       `json:"text"`
	Span polyglotSpan `json:"span"`
}

type polyglotSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type polyglotValidateResponse struct {
	Valid  bool                      `json:"valid"`
	Errors []polyglotValidationError `json:"errors"`
}

type polyglotValidationError struct {
	Message  string `json:"message"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
}

type polyglotSchema struct {
	Tables []polyglotSchemaTable `json:"tables"`
}

type polyglotSchemaTable struct {
	Name    string                 `json:"name"`
	Columns []polyglotSchemaColumn `json:"columns"`
}

type polyglotSchemaColumn struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type polyglotCTE struct {
	Name         string
	Columns      []SchemaColumn
	ColumnRanges map[string]ParseContextRange
	Metadata     map[string]polyglotColumnMetadata
}

type polyglotColumnMetadata struct {
	SourceMethods     []string
	OriginTable       string
	ActualSchemaKnown bool
}

var (
	polyglotQuotedIdentifierPattern = regexp.MustCompile(`'([^']+)'`)
	polyglotCallLikeSubqueryPattern = regexp.MustCompile(`(?i)\b([A-Za-z_][\w$]*)\s+\(\s*select\b`)
)

func ParseContextWithSchemaPolyglot(query, dialect string, schema Schema, columnSourceMethods ...SchemaColumnSourceMethods) (*ParseContext, error) {
	ctx := context.Background()
	if dialect == "" {
		dialect = sqlformat.DialectGeneric
	}

	parseJSON, err := sqlformat.Call(ctx, "parse", query, dialect)
	if err != nil {
		return nil, err
	}
	var parseResp polyglotParseResponse
	if err := json.Unmarshal([]byte(parseJSON), &parseResp); err != nil {
		return nil, err
	}
	if !parseResp.Success {
		if recovered := recoverPolyglotCallLikeSubquery(query, fmt.Sprint(parseResp.Error)); recovered != nil {
			return recovered, nil
		}
		return &ParseContext{Diagnostics: []ParseContextDiagnostic{{Message: fmt.Sprint(parseResp.Error), Severity: "error"}}, Errors: []string{fmt.Sprint(parseResp.Error)}}, nil
	}

	tokenJSON, err := sqlformat.Call(ctx, "tokenize", query, dialect)
	if err != nil {
		return nil, err
	}
	var tokenResp polyglotTokenizeResponse
	if err := json.Unmarshal([]byte(tokenJSON), &tokenResp); err != nil {
		return nil, err
	}
	if !tokenResp.Success {
		return nil, fmt.Errorf("polyglot tokenization failed: %v", tokenResp.Error)
	}

	ast, err := decodePolyglotAST(parseResp.AST)
	if err != nil {
		return nil, err
	}

	sourceMethods := firstColumnSourceMethods(columnSourceMethods)
	ctes := extractPolyglotCTEs(query, ast, tokenResp.Tokens, schema, sourceMethods)
	validationSchema := mergePolyglotCTEsIntoSchema(schema, ctes)

	validateJSON, err := validateWithPolyglot(ctx, query, dialect, validationSchema)
	if err != nil {
		return nil, err
	}
	var validateResp polyglotValidateResponse
	if validateJSON != "" {
		if err := json.Unmarshal([]byte(validateJSON), &validateResp); err != nil {
			return nil, err
		}
	}

	tables := extractPolyglotTables(query, ast, tokenResp.Tokens, validationSchema, ctes)
	aliasToTable := map[string]string{}
	for _, table := range tables {
		if table.Alias != "" {
			aliasToTable[strings.ToLower(table.Alias)] = table.ResolvedName
		} else if table.Name != "" {
			aliasToTable[strings.ToLower(table.Name)] = table.ResolvedName
		}
	}

	columns := extractPolyglotColumns(query, ast, tokenResp.Tokens, aliasToTable, tables)

	return &ParseContext{
		QueryKind:      polyglotQueryKind(ast),
		IsSingleSelect: len(ast) == 1 && polyglotQueryKind(ast) == "select",
		Tables:         tables,
		Columns:        columns,
		Diagnostics:    polyglotDiagnostics(query, tokenResp.Tokens, validateResp.Errors, tables, columns, validationSchema, ctes, sourceMethods, polyglotSelectAliases(ast), polyglotDescribeColumns(query)),
		Errors:         []string{},
	}, nil
}

func decodePolyglotAST(raw json.RawMessage) ([]map[string]any, error) {
	var ast []map[string]any
	if err := json.Unmarshal(raw, &ast); err == nil {
		return ast, nil
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(encoded), &ast); err != nil {
		return nil, err
	}
	return ast, nil
}

func recoverPolyglotCallLikeSubquery(query, parseError string) *ParseContext {
	if !strings.Contains(strings.ToLower(parseError), "unexpected token: select") {
		return nil
	}
	matches := polyglotCallLikeSubqueryPattern.FindAllStringSubmatchIndex(query, -1)
	if len(matches) == 0 {
		return nil
	}
	match := matches[len(matches)-1]
	if len(match) < 4 {
		return nil
	}
	name := query[match[2]:match[3]]
	rangeInfo := rangeFromOffsets(query, match[2], match[3])
	return &ParseContext{
		QueryKind:      "select",
		IsSingleSelect: true,
		Diagnostics: []ParseContextDiagnostic{{
			Message:  "Unresolved column: " + name,
			Severity: "error",
			Range:    &rangeInfo,
		}},
		Errors: []string{},
	}
}

func validateWithPolyglot(ctx context.Context, query, dialect string, schema Schema) (string, error) {
	polySchema := polyglotSchema{Tables: make([]polyglotSchemaTable, 0, len(schema))}
	for tableName, columns := range schema {
		table := polyglotSchemaTable{Name: tableName, Columns: make([]polyglotSchemaColumn, 0, len(columns))}
		for columnName, columnType := range columns {
			table.Columns = append(table.Columns, polyglotSchemaColumn{Name: columnName, Type: columnType})
		}
		polySchema.Tables = append(polySchema.Tables, table)
	}
	if len(polySchema.Tables) == 0 {
		return "", nil
	}
	schemaJSON, err := json.Marshal(polySchema)
	if err != nil {
		return "", err
	}
	return sqlformat.Call(ctx, "validate_with_schema", query, string(schemaJSON), dialect, `{"strict":true}`)
}

func extractPolyglotCTEs(query string, ast []map[string]any, tokens []polyglotToken, schema Schema, columnSourceMethods SchemaColumnSourceMethods) map[string]polyglotCTE {
	ctes := map[string]polyglotCTE{}
	walkPolyglot(ast, func(key string, value map[string]any) {
		if key != "with" {
			return
		}
		rawCTEs, ok := value["ctes"].([]any)
		if !ok {
			return
		}
		for _, rawCTE := range rawCTEs {
			cteValue, ok := rawCTE.(map[string]any)
			if !ok {
				continue
			}
			name := polyglotIdentifierName(cteValue["alias"])
			if name == "" {
				continue
			}
			columns, ranges, metadata := polyglotSelectColumns(query, tokens, cteValue["this"], schema, columnSourceMethods)
			ctes[strings.ToLower(name)] = polyglotCTE{Name: name, Columns: columns, ColumnRanges: ranges, Metadata: metadata}
		}
	})
	return ctes
}

func mergePolyglotCTEsIntoSchema(schema Schema, ctes map[string]polyglotCTE) Schema {
	merged := Schema{}
	for tableName, columns := range schema {
		mergedColumns := map[string]string{}
		for columnName, columnType := range columns {
			mergedColumns[columnName] = columnType
		}
		merged[tableName] = mergedColumns
	}
	for _, cte := range ctes {
		columns := map[string]string{}
		for _, column := range cte.Columns {
			columns[column.Name] = column.Type
		}
		merged[cte.Name] = columns
	}
	return merged
}

func polyglotSelectColumns(query string, tokens []polyglotToken, node any, schema Schema, columnSourceMethods SchemaColumnSourceMethods) ([]SchemaColumn, map[string]ParseContextRange, map[string]polyglotColumnMetadata) {
	selectNode := findPolyglotSelect(node)
	if selectNode == nil {
		return nil, nil, nil
	}
	columns := []SchemaColumn{}
	ranges := map[string]ParseContextRange{}
	metadata := map[string]polyglotColumnMetadata{}
	sourceTables := polyglotSelectSourceTables(selectNode)
	if expressions, ok := selectNode["expressions"].([]any); ok {
		for _, expression := range expressions {
			if isPolyglotStar(expression) {
				for _, tableName := range sourceTables {
					actualSchemaKnown := polyglotActualSchemaKnown(columnSourceMethods[tableName])
					for _, column := range schemaColumns(schema[tableName]) {
						columns = appendUniqueSchemaColumn(columns, column)
						metadata[column.Name] = polyglotColumnMetadata{SourceMethods: columnSourceMethods[tableName][column.Name], OriginTable: tableName, ActualSchemaKnown: actualSchemaKnown}
					}
				}
				continue
			}
			name := polyglotExpressionOutputName(expression)
			if name == "" {
				continue
			}
			columns = appendUniqueSchemaColumn(columns, SchemaColumn{Name: name})
			metadata[name] = polyglotColumnMetadata{SourceMethods: []string{"query-expression"}}
			if found := findTokenRange(query, tokens, []string{name}); found != nil {
				ranges[name] = *found
			}
		}
	}
	return columns, ranges, metadata
}

func polyglotSelectAliases(ast []map[string]any) map[string]bool {
	aliases := map[string]bool{}
	walkPolyglot(ast, func(key string, value map[string]any) {
		if key != "alias" {
			return
		}
		if alias := polyglotIdentifierName(value["alias"]); alias != "" {
			aliases[strings.ToLower(alias)] = true
		}
	})
	return aliases
}

func polyglotDescribeColumns(query string) map[string]bool {
	columns := map[string]bool{}
	if !strings.Contains(strings.ToLower(query), "describe") {
		return columns
	}
	for _, column := range []string{"column_name", "column_type", "null", "key", "default", "extra"} {
		columns[column] = true
	}
	return columns
}

func findPolyglotSelect(node any) map[string]any {
	if mapNode, ok := node.(map[string]any); ok {
		if selectNode, ok := mapNode["select"].(map[string]any); ok {
			return selectNode
		}
		for _, child := range mapNode {
			if found := findPolyglotSelect(child); found != nil {
				return found
			}
		}
	}
	if listNode, ok := node.([]any); ok {
		for _, child := range listNode {
			if found := findPolyglotSelect(child); found != nil {
				return found
			}
		}
	}
	return nil
}

func polyglotSelectSourceTables(selectNode map[string]any) []string {
	var tables []string
	if fromNode, ok := selectNode["from"].(map[string]any); ok {
		walkPolyglot(fromNode, func(key string, value map[string]any) {
			if key == "table" && isPolyglotTableReference(value) {
				name := polyglotIdentifierName(value["name"])
				if name != "" {
					tables = append(tables, strings.Join(polyglotTableParts(value, name), "."))
				}
			}
		})
	}
	return tables
}

func polyglotExpressionOutputName(expression any) string {
	mapExpression, ok := expression.(map[string]any)
	if !ok {
		return ""
	}
	if aliasNode, ok := mapExpression["alias"].(map[string]any); ok {
		if alias := polyglotIdentifierName(aliasNode["alias"]); alias != "" {
			return alias
		}
	}
	if columnNode, ok := mapExpression["column"].(map[string]any); ok {
		return polyglotIdentifierName(columnNode["name"])
	}
	return ""
}

func isPolyglotStar(expression any) bool {
	found := false
	walkPolyglot(expression, func(key string, _ map[string]any) {
		if key == "star" {
			found = true
		}
	})
	return found
}

func appendUniqueSchemaColumn(columns []SchemaColumn, column SchemaColumn) []SchemaColumn {
	for _, existing := range columns {
		if strings.EqualFold(existing.Name, column.Name) {
			return columns
		}
	}
	return append(columns, column)
}

func polyglotQueryKind(ast []map[string]any) string {
	if len(ast) == 0 {
		return "unknown"
	}
	for key := range ast[0] {
		return strings.ToLower(key)
	}
	return "unknown"
}

func extractPolyglotTables(query string, ast []map[string]any, tokens []polyglotToken, schema Schema, ctes map[string]polyglotCTE) []ParseContextTable {
	var tables []ParseContextTable
	usedTableStarts := map[int]bool{}
	walkPolyglot(ast, func(key string, value map[string]any) {
		if key != "table" || !isPolyglotTableReference(value) {
			return
		}
		name := polyglotIdentifierName(value["name"])
		if name == "" {
			return
		}
		parts := polyglotTableParts(value, name)
		fullName := strings.Join(parts, ".")
		alias := polyglotIdentifierName(value["alias"])
		identifierParts, tableTokenStart := partsToRangesForTableOccurrence(query, tokens, parts, usedTableStarts)
		var aliasRange *ParseContextRange
		if alias != "" {
			if found := findTokenRangeFromIndex(query, tokens, []string{alias}, tableTokenStart+len(parts)); found != nil {
				aliasRange = found
			}
		}
		sourceKind := "table"
		resolvedName := fullName
		columns := schemaColumns(schema[fullName])
		var columnRanges map[string]ParseContextRange
		if cte, ok := ctes[strings.ToLower(fullName)]; ok {
			sourceKind = "cte"
			resolvedName = cte.Name
			columns = cte.Columns
			columnRanges = cte.ColumnRanges
		}
		var scopeRange *ParseContextRange
		if len(identifierParts) > 0 {
			scopeRange = buildPolyglotScopeRange(query, tokens, identifierParts[0].Range.Start)
		}
		tables = append(tables, ParseContextTable{
			Name:         fullName,
			SourceKind:   sourceKind,
			ResolvedName: resolvedName,
			Alias:        alias,
			Columns:      columns,
			ColumnRanges: columnRanges,
			Parts:        identifierParts,
			AliasRange:   aliasRange,
			ScopeRange:   scopeRange,
		})
	})
	sortPolyglotTablesBySource(tables)
	return tables
}

func extractPolyglotColumns(query string, ast []map[string]any, tokens []polyglotToken, aliasToTable map[string]string, tables []ParseContextTable) []ParseContextColumn {
	var columns []ParseContextColumn
	usedColumnStarts := map[int]bool{}
	walkPolyglot(ast, func(key string, value map[string]any) {
		if key != "column" {
			return
		}
		name := polyglotIdentifierName(value["name"])
		if name == "" {
			return
		}
		qualifier := polyglotIdentifierName(value["table"])
		parts := []string{name}
		if qualifier != "" {
			parts = []string{qualifier, name}
		}
		partRanges := columnPartsToRanges(query, tokens, parts, tables, usedColumnStarts)
		resolvedTable := aliasToTable[strings.ToLower(qualifier)]
		if qualifier != "" {
			if fullTable, columnName, fullParts := resolveThreePartPolyglotColumn(query, tokens, qualifier, name, tables, usedColumnStarts); fullTable != "" {
				qualifier = fullTable
				name = columnName
				resolvedTable = fullTable
				partRanges = fullParts
			}
		}
		if resolvedTable == "" && qualifier == "" {
			columnOffset := -1
			if len(partRanges) > 0 {
				columnOffset = partRanges[0].Range.Start
			}
			resolvedTable = resolveUnqualifiedPolyglotColumn(name, tables, columnOffset)
		}
		columns = append(columns, ParseContextColumn{
			Name:          name,
			Qualifier:     qualifier,
			ResolvedTable: resolvedTable,
			Parts:         partRanges,
		})
	})
	return columns
}

func resolveUnqualifiedPolyglotColumn(columnName string, tables []ParseContextTable, columnOffset int) string {
	resolved := ""
	for _, table := range tables {
		if columnOffset >= 0 && table.ScopeRange != nil && (columnOffset < table.ScopeRange.Start || columnOffset > table.ScopeRange.End) {
			continue
		}
		for _, column := range table.Columns {
			if strings.EqualFold(column.Name, columnName) {
				if resolved != "" && !strings.EqualFold(resolved, table.ResolvedName) {
					return ""
				}
				resolved = table.ResolvedName
			}
		}
	}
	return resolved
}

func resolveThreePartPolyglotColumn(query string, tokens []polyglotToken, qualifier, name string, tables []ParseContextTable, usedStarts map[int]bool) (string, string, []ParseContextPart) {
	for _, table := range tables {
		tableParts := strings.Split(table.ResolvedName, ".")
		if len(tableParts) != 2 || !strings.EqualFold(tableParts[0], qualifier) || !strings.EqualFold(tableParts[1], name) {
			continue
		}
		for _, column := range table.Columns {
			parts := []string{qualifier, name, column.Name}
			for index := 0; index+4 < len(tokens); index++ {
				if isPolyglotTableStart(tokens, index) {
					continue
				}
				if !strings.EqualFold(tokens[index].Text, parts[0]) || tokens[index+1].Text != "." || !strings.EqualFold(tokens[index+2].Text, parts[1]) || tokens[index+3].Text != "." || !strings.EqualFold(tokens[index+4].Text, parts[2]) {
					continue
				}
				ranges := []ParseContextPart{
					{Name: qualifier, Kind: "schema", Range: rangeFromOffsets(query, tokens[index].Span.Start, tokens[index].Span.End)},
					{Name: name, Kind: "table", Range: rangeFromOffsets(query, tokens[index+2].Span.Start, tokens[index+2].Span.End)},
					{Name: column.Name, Kind: "column", Range: rangeFromOffsets(query, tokens[index+4].Span.Start, tokens[index+4].Span.End)},
				}
				return table.ResolvedName, column.Name, ranges
			}
		}
	}
	return "", "", nil
}

func columnPartsToRanges(query string, tokens []polyglotToken, parts []string, tables []ParseContextTable, usedStarts map[int]bool) []ParseContextPart {
	if len(parts) == 1 {
		for index, token := range tokens {
			if usedStarts[index] || !strings.EqualFold(token.Text, parts[0]) {
				continue
			}
			if index > 0 && strings.EqualFold(tokens[index-1].Text, "as") {
				continue
			}
			usedStarts[index] = true
			rangeInfo := rangeFromOffsets(query, token.Span.Start, token.Span.End)
			return []ParseContextPart{{Name: parts[0], Kind: "column", Range: rangeInfo}}
		}
	}
	if len(parts) > 0 {
		for index := 0; index < len(tokens); index++ {
			if usedStarts[index] {
				continue
			}
			matched := true
			for partIndex, part := range parts {
				tokenIndex := index + partIndex*2
				if tokenIndex >= len(tokens) || !strings.EqualFold(tokens[tokenIndex].Text, part) {
					matched = false
					break
				}
				if partIndex < len(parts)-1 && (tokenIndex+1 >= len(tokens) || tokens[tokenIndex+1].Text != ".") {
					matched = false
					break
				}
			}
			if matched {
				usedStarts[index] = true
				ranges, _ := partsToRangesFrom(query, tokens, parts, "column", index)
				return ranges
			}
		}
	}
	return partsToRanges(query, tokens, parts, "column")
}

func walkPolyglot(value any, visit func(string, map[string]any)) {
	switch typed := value.(type) {
	case []map[string]any:
		for _, item := range typed {
			walkPolyglot(item, visit)
		}
	case []any:
		for _, item := range typed {
			walkPolyglot(item, visit)
		}
	case map[string]any:
		for key, child := range typed {
			if childMap, ok := child.(map[string]any); ok {
				visit(key, childMap)
			}
			walkPolyglot(child, visit)
		}
	}
}

func isPolyglotTableReference(value map[string]any) bool {
	_, hasName := value["name"]
	_, hasAlias := value["alias"]
	_, hasSchema := value["schema"]
	_, hasCatalog := value["catalog"]
	_, hasColumnAliases := value["column_aliases"]
	return hasName && (hasAlias || hasSchema || hasCatalog || hasColumnAliases)
}

func polyglotTableParts(value map[string]any, name string) []string {
	parts := []string{}
	if catalog := polyglotIdentifierName(value["catalog"]); catalog != "" {
		parts = append(parts, catalog)
	}
	if schema := polyglotIdentifierName(value["schema"]); schema != "" {
		parts = append(parts, schema)
	}
	parts = append(parts, name)
	return parts
}

func polyglotIdentifierName(value any) string {
	if value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	mapValue, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if name, ok := mapValue["name"].(string); ok {
		return name
	}
	return ""
}

func schemaColumns(columns map[string]string) []SchemaColumn {
	result := make([]SchemaColumn, 0, len(columns))
	for name, columnType := range columns {
		result = append(result, SchemaColumn{Name: name, Type: columnType})
	}
	return result
}

func partsToRanges(query string, tokens []polyglotToken, parts []string, lastKind string) []ParseContextPart {
	ranges, _ := partsToRangesFrom(query, tokens, parts, lastKind, 0)
	return ranges
}

func partsToRangesFrom(query string, tokens []polyglotToken, parts []string, lastKind string, start int) ([]ParseContextPart, int) {
	ranges := make([]ParseContextPart, 0, len(parts))
	searchStart := start
	for index, part := range parts {
		foundIndex, foundRange := findTokenRangeFrom(query, tokens, []string{part}, searchStart)
		if foundRange == nil {
			continue
		}
		searchStart = foundIndex + 1
		kind := "schema"
		if index == len(parts)-1 {
			kind = lastKind
		} else if lastKind == "column" && index == len(parts)-2 {
			kind = "table"
		}
		ranges = append(ranges, ParseContextPart{Name: part, Kind: kind, Range: *foundRange})
	}
	return ranges, searchStart
}

func partsToRangesForTableOccurrence(query string, tokens []polyglotToken, parts []string, usedStarts map[int]bool) ([]ParseContextPart, int) {
	for index := 0; index < len(tokens); index++ {
		if usedStarts[index] || !isPolyglotTableStart(tokens, index) {
			continue
		}
		matched := true
		for partIndex, part := range parts {
			tokenIndex := index + partIndex*2
			if tokenIndex >= len(tokens) || !strings.EqualFold(tokens[tokenIndex].Text, part) {
				matched = false
				break
			}
			if partIndex < len(parts)-1 && (tokenIndex+1 >= len(tokens) || tokens[tokenIndex+1].Text != ".") {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		usedStarts[index] = true
		ranges, _ := partsToRangesFrom(query, tokens, parts, "table", index)
		return ranges, index
	}
	return partsToRanges(query, tokens, parts, "table"), 0
}

func isPolyglotTableStart(tokens []polyglotToken, index int) bool {
	if index <= 0 {
		return false
	}
	previous := strings.ToLower(tokens[index-1].Text)
	return previous == "from" || previous == "join" || previous == "update" || previous == "into"
}

func sortPolyglotTablesBySource(tables []ParseContextTable) {
	sort.SliceStable(tables, func(i, j int) bool {
		return polyglotTableStart(tables[i]) < polyglotTableStart(tables[j])
	})
}

func polyglotTableStart(table ParseContextTable) int {
	if len(table.Parts) == 0 {
		return 1 << 30
	}
	return table.Parts[0].Range.Start
}

func findTokenRange(query string, tokens []polyglotToken, parts []string) *ParseContextRange {
	_, rangeInfo := findTokenRangeFrom(query, tokens, parts, 0)
	return rangeInfo
}

func findTokenRangeFromIndex(query string, tokens []polyglotToken, parts []string, start int) *ParseContextRange {
	_, rangeInfo := findTokenRangeFrom(query, tokens, parts, start)
	return rangeInfo
}

func findTokenRangeFrom(query string, tokens []polyglotToken, parts []string, start int) (int, *ParseContextRange) {
	for index := start; index <= len(tokens)-len(parts); index++ {
		matched := true
		for partIndex, part := range parts {
			if !strings.EqualFold(tokens[index+partIndex].Text, part) {
				matched = false
				break
			}
		}
		if matched {
			rangeInfo := rangeFromOffsets(query, tokens[index].Span.Start, tokens[index+len(parts)-1].Span.End)
			return index, &rangeInfo
		}
	}
	return -1, nil
}

func polyglotDiagnostics(query string, tokens []polyglotToken, errors []polyglotValidationError, tables []ParseContextTable, columns []ParseContextColumn, schema Schema, ctes map[string]polyglotCTE, sourceMethods SchemaColumnSourceMethods, selectAliases map[string]bool, describeColumns map[string]bool) []ParseContextDiagnostic {
	diagnostics := make([]ParseContextDiagnostic, 0, len(errors))
	for _, item := range errors {
		severity := strings.ToLower(strings.TrimSpace(item.Severity))
		if severity == "" {
			severity = "error"
		}
		var rangeInfo *ParseContextRange
		match := polyglotQuotedIdentifierPattern.FindStringSubmatch(item.Message)
		if len(match) > 1 {
			rangeInfo = findTokenRange(query, tokens, []string{match[1]})
		}
		message := item.Message
		if strings.EqualFold(item.Code, "E201") && len(match) > 1 {
			if selectAliases[strings.ToLower(match[1])] || describeColumns[strings.ToLower(match[1])] {
				continue
			}
			message = "Unresolved column: " + match[1]
		}
		diagnostics = append(diagnostics, ParseContextDiagnostic{Message: message, Severity: severity, Range: rangeInfo})
	}
	diagnostics = append(diagnostics, polyglotLocalColumnDiagnostics(columns, tables, selectAliases, describeColumns)...)
	diagnostics = append(diagnostics, polyglotUnmaterializedColumnWarnings(columns, ctes, sourceMethods)...)
	return diagnostics
}

func polyglotLocalColumnDiagnostics(columns []ParseContextColumn, tables []ParseContextTable, selectAliases map[string]bool, describeColumns map[string]bool) []ParseContextDiagnostic {
	diagnostics := []ParseContextDiagnostic{}
	for _, column := range columns {
		if column.Name == "" {
			continue
		}
		if column.Qualifier == "" && (selectAliases[strings.ToLower(column.Name)] || describeColumns[strings.ToLower(column.Name)]) {
			continue
		}
		if column.ResolvedTable == "" {
			if column.Qualifier != "" {
				var rangeInfo *ParseContextRange
				if len(column.Parts) > 0 {
					rangeCopy := column.Parts[0].Range
					rangeInfo = &rangeCopy
				}
				diagnostics = append(diagnostics, ParseContextDiagnostic{Message: "Unresolved table or alias: " + column.Qualifier, Severity: "error", Range: rangeInfo})
				continue
			}
			if column.Qualifier == "" && len(tables) > 0 {
				var rangeInfo *ParseContextRange
				if len(column.Parts) > 0 {
					rangeCopy := column.Parts[len(column.Parts)-1].Range
					rangeInfo = &rangeCopy
				}
				diagnostics = append(diagnostics, ParseContextDiagnostic{Message: "Unresolved column: " + column.Name, Severity: "error", Range: rangeInfo})
			}
			continue
		}
		table := polyglotTableByResolvedName(tables, column.ResolvedTable)
		if table == nil || !polyglotTableHasColumn(*table, column.Name) {
			var rangeInfo *ParseContextRange
			if len(column.Parts) > 0 {
				rangeCopy := column.Parts[len(column.Parts)-1].Range
				rangeInfo = &rangeCopy
			}
			diagnostics = append(diagnostics, ParseContextDiagnostic{Message: "Unresolved column: " + column.Name, Severity: "error", Range: rangeInfo})
		}
	}
	return diagnostics
}

func polyglotTableByResolvedName(tables []ParseContextTable, resolvedName string) *ParseContextTable {
	for index := range tables {
		if strings.EqualFold(tables[index].ResolvedName, resolvedName) || strings.EqualFold(tables[index].Name, resolvedName) {
			return &tables[index]
		}
	}
	return nil
}

func polyglotTableHasColumn(table ParseContextTable, columnName string) bool {
	for _, column := range table.Columns {
		if strings.EqualFold(column.Name, columnName) {
			return true
		}
	}
	return false
}

func polyglotUnmaterializedColumnWarnings(columns []ParseContextColumn, ctes map[string]polyglotCTE, sourceMethods SchemaColumnSourceMethods) []ParseContextDiagnostic {
	warnings := []ParseContextDiagnostic{}
	for _, column := range columns {
		metadata := polyglotMetadataForColumn(column.ResolvedTable, column.Name, ctes, sourceMethods)
		if !polyglotShouldWarnUnmaterialized(metadata) {
			continue
		}
		var rangeInfo *ParseContextRange
		if len(column.Parts) > 0 {
			rangeCopy := column.Parts[len(column.Parts)-1].Range
			rangeInfo = &rangeCopy
		}
		originTable := metadata.OriginTable
		if originTable == "" {
			originTable = "an upstream Bruin asset"
		}
		warnings = append(warnings, ParseContextDiagnostic{
			Message:  fmt.Sprintf("Column '%s' is defined in the Bruin asset '%s', but it has not been materialized yet.", column.Name, originTable),
			Severity: "warning",
			Range:    rangeInfo,
		})
	}
	return warnings
}

func polyglotMetadataForColumn(tableName, columnName string, ctes map[string]polyglotCTE, sourceMethods SchemaColumnSourceMethods) polyglotColumnMetadata {
	if tableName == "" {
		return polyglotColumnMetadata{}
	}
	if cte, ok := ctes[strings.ToLower(tableName)]; ok {
		if metadata, ok := cte.Metadata[columnName]; ok {
			return metadata
		}
		for sourceTable, methodsByColumn := range sourceMethods {
			if methods, ok := methodsByColumn[columnName]; ok {
				return polyglotColumnMetadata{SourceMethods: methods, OriginTable: sourceTable, ActualSchemaKnown: polyglotActualSchemaKnown(methodsByColumn)}
			}
		}
		return polyglotColumnMetadata{}
	}
	methodsByColumn := sourceMethods[tableName]
	return polyglotColumnMetadata{SourceMethods: methodsByColumn[columnName], OriginTable: tableName, ActualSchemaKnown: polyglotActualSchemaKnown(methodsByColumn)}
}

func polyglotShouldWarnUnmaterialized(metadata polyglotColumnMetadata) bool {
	methods := map[string]bool{}
	for _, method := range metadata.SourceMethods {
		methods[method] = true
	}
	definition := methods["workspace-load"] || methods["workspace-event"] || methods["asset-column-inference"] || methods["asset-sql-definition"]
	materialized := methods["connection-column-discovery"] || methods["materialized-workspace-load"]
	return metadata.ActualSchemaKnown && definition && !materialized
}

func polyglotActualSchemaKnown(methodsByColumn map[string][]string) bool {
	for _, methods := range methodsByColumn {
		for _, method := range methods {
			if method == "connection-column-discovery" || method == "materialized-workspace-load" {
				return true
			}
		}
	}
	return false
}

func firstColumnSourceMethods(columnSourceMethods []SchemaColumnSourceMethods) SchemaColumnSourceMethods {
	if len(columnSourceMethods) == 0 || columnSourceMethods[0] == nil {
		return SchemaColumnSourceMethods{}
	}
	return columnSourceMethods[0]
}

func buildPolyglotScopeRange(query string, tokens []polyglotToken, tableStart int) *ParseContextRange {
	start := 0
	end := len(query)
	tableDepth := polyglotDepthAt(tokens, tableStart)
	depth := 0
	for _, token := range tokens {
		if token.Span.Start >= tableStart {
			break
		}
		if token.Text == "(" {
			depth++
		} else if token.Text == ")" && depth > 0 {
			depth--
		}
		if strings.EqualFold(token.Text, "select") && depth == tableDepth {
			start = token.Span.Start
		}
	}
	depth = tableDepth
	for _, token := range tokens {
		if token.Span.Start <= tableStart {
			continue
		}
		if token.Text == "(" {
			depth++
		} else if token.Text == ")" {
			if depth == tableDepth {
				end = token.Span.End
				break
			}
			if depth > 0 {
				depth--
			}
		}
	}
	rangeInfo := rangeFromOffsets(query, start, end)
	return &rangeInfo
}

func polyglotDepthAt(tokens []polyglotToken, offset int) int {
	depth := 0
	for _, token := range tokens {
		if token.Span.Start >= offset {
			break
		}
		if token.Text == "(" {
			depth++
		} else if token.Text == ")" && depth > 0 {
			depth--
		}
	}
	return depth
}

func rangeFromOffsets(query string, start, end int) ParseContextRange {
	line, col := offsetToLineCol(query, start)
	endLine, endCol := offsetToLineCol(query, end)
	return ParseContextRange{Start: start, End: end, Line: line, Col: col, EndLine: endLine, EndCol: endCol}
}

func offsetToLineCol(query string, offset int) (int, int) {
	if offset < 0 {
		return 1, 1
	}
	if offset > len(query) {
		offset = len(query)
	}
	line := 1
	lineStart := 0
	for index, char := range query[:offset] {
		if char == '\n' {
			line++
			lineStart = index + 1
		}
	}
	return line, offset - lineStart + 1
}
