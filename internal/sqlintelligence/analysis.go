package sqlintelligence

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"renart/internal/sqlformat"
)

// QueryAnalysis is the compact, schema-aware query result returned by
// Polyglot. It intentionally keeps the facts useful to schema inference and
// lineage without retaining the much larger annotated AST.
type QueryAnalysis struct {
	Shape           string                `json:"shape"`
	CTEs            []string              `json:"ctes"`
	CTEFacts        []QueryCTEFact        `json:"cteFacts"`
	Projections     []QueryProjection     `json:"projections"`
	Relations       []QueryRelation       `json:"relations"`
	BaseTables      []QueryRelation       `json:"baseTables"`
	StarProjections []QueryStarProjection `json:"starProjections"`
	SetOperations   []QuerySetOperation   `json:"setOperations"`

	OutputColumns       []SchemaColumn `json:"-"`
	OutputNamesComplete bool           `json:"-"`
	OutputTypesComplete bool           `json:"-"`
}

type QueryProjection struct {
	Index             int                     `json:"index"`
	Name              *string                 `json:"name"`
	IsStar            bool                    `json:"isStar"`
	StarTable         *string                 `json:"starTable"`
	TransformKind     string                  `json:"transformKind"`
	TransformFunction *QueryTransformFunction `json:"transformFunction,omitempty"`
	CastType          *string                 `json:"castType"`
	TypeHint          *string                 `json:"typeHint"`
	Nullability       string                  `json:"nullability"`
	Upstream          []QueryColumnReference  `json:"upstream"`
}

type QueryTransformFunction struct {
	Name        string                 `json:"name"`
	LiteralArgs []string               `json:"literalArgs"`
	ColumnArgs  []QueryColumnReference `json:"columnArgs"`
}

type QueryCTEFact struct {
	Name          string   `json:"name"`
	Columns       []string `json:"columns"`
	BodySQL       string   `json:"bodySql"`
	OutputColumns []string `json:"outputColumns"`
}

type QueryStarProjection struct {
	Index           int      `json:"index"`
	Table           *string  `json:"table"`
	ExpandedColumns []string `json:"expandedColumns"`
}

type QueryColumnReference struct {
	SourceName  *string `json:"sourceName"`
	SourceAlias *string `json:"sourceAlias"`
	SourceKind  string  `json:"sourceKind"`
	Table       *string `json:"table"`
	Column      string  `json:"column"`
	Unqualified bool    `json:"unqualified"`
	Confidence  string  `json:"confidence"`
}

type QueryRelation struct {
	Name    string   `json:"name"`
	Alias   *string  `json:"alias"`
	Kind    string   `json:"kind"`
	Columns []string `json:"columns"`
	Catalog *string  `json:"catalog"`
	Schema  *string  `json:"schema"`
	Table   *string  `json:"table"`
}

type QuerySetOperation struct {
	Kind          string                    `json:"kind"`
	All           bool                      `json:"all"`
	Distinct      bool                      `json:"distinct"`
	OutputColumns []string                  `json:"outputColumns"`
	Branches      []QuerySetOperationBranch `json:"branches"`
}

type QuerySetOperationBranch struct {
	Index       int               `json:"index"`
	Projections []QueryProjection `json:"projections"`
}

type polyglotAnalyzeQueryOptions struct {
	Dialect string         `json:"dialect"`
	Schema  polyglotSchema `json:"schema"`
}

type polyglotAnalyzeQueryResponse struct {
	Success  bool           `json:"success"`
	Analysis *QueryAnalysis `json:"analysis"`
	Error    any            `json:"error"`
}

const queryAnalysisCacheCapacity = 256

var polyglotQueryAnalysisCache = newQueryAnalysisCache(queryAnalysisCacheCapacity)

// AnalyzeQuery returns Polyglot's compact query facts. Successful results are
// cached by SQL, normalized dialect, and deterministic schema payload so graph
// fixpoint rounds and repeated revision builds do not repeat the WASM work.
// Failures and canceled requests never enter the cache.
func AnalyzeQuery(ctx context.Context, query, dialect string, schema Schema, constraintSets ...SchemaConstraints) (QueryAnalysis, error) {
	if err := ctx.Err(); err != nil {
		return QueryAnalysis{}, err
	}
	if strings.TrimSpace(query) == "" {
		return QueryAnalysis{}, errors.New("cannot analyze empty SQL")
	}

	optionsJSON, err := marshalAnalyzeQueryOptions(dialect, schema, constraintSets...)
	if err != nil {
		return QueryAnalysis{}, err
	}
	key := queryAnalysisKey(query, optionsJSON)
	if cached, ok := polyglotQueryAnalysisCache.get(key); ok {
		return cached, nil
	}

	analysis, err := analyzeQueryUncached(ctx, query, optionsJSON, schema)
	if err != nil {
		return QueryAnalysis{}, err
	}
	if err := ctx.Err(); err != nil {
		return QueryAnalysis{}, err
	}
	polyglotQueryAnalysisCache.add(key, analysis)
	return analysis, nil
}

func marshalAnalyzeQueryOptions(dialect string, schema Schema, constraintSets ...SchemaConstraints) (string, error) {
	options := polyglotAnalyzeQueryOptions{
		Dialect: polyglotAnalyzeDialect(dialect),
		Schema:  buildPolyglotSchema(schema, constraintSets...),
	}
	raw, err := json.Marshal(options)
	return string(raw), err
}

func polyglotAnalyzeDialect(dialect string) string {
	switch strings.ToLower(strings.TrimSpace(dialect)) {
	case "", "generic":
		return sqlformat.DialectGeneric
	case "postgres", "postgresql":
		return "postgresql"
	default:
		return strings.ToLower(strings.TrimSpace(dialect))
	}
}

func analyzeQueryUncached(ctx context.Context, query, optionsJSON string, schema Schema) (QueryAnalysis, error) {
	raw, err := sqlformat.Call(ctx, "analyze_query", query, optionsJSON)
	if err != nil {
		return QueryAnalysis{}, err
	}
	var response polyglotAnalyzeQueryResponse
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return QueryAnalysis{}, err
	}
	if !response.Success || response.Analysis == nil {
		message := strings.TrimSpace(fmt.Sprint(response.Error))
		if message == "" || message == "<nil>" {
			message = "analyze_query failed"
		}
		return QueryAnalysis{}, errors.New(message)
	}
	finalizeQueryAnalysis(response.Analysis, schema)
	return *response.Analysis, nil
}

func finalizeQueryAnalysis(analysis *QueryAnalysis, schema Schema) {
	analysis.OutputNamesComplete = len(analysis.Projections) > 0
	analysis.OutputTypesComplete = len(analysis.Projections) > 0
	analysis.OutputColumns = make([]SchemaColumn, 0, len(analysis.Projections))
	for _, projection := range analysis.Projections {
		name := ""
		if projection.Name != nil {
			name = strings.TrimSpace(*projection.Name)
		}
		if projection.IsStar || name == "" || isSyntheticProjectionName(name, projection.Index) {
			analysis.OutputNamesComplete = false
			analysis.OutputTypesComplete = false
			continue
		}

		columnType := queryProjectionType(projection, schema)
		if columnType == "" {
			analysis.OutputTypesComplete = false
		}
		analysis.OutputColumns = appendUniqueSchemaColumn(analysis.OutputColumns, SchemaColumn{
			Name:     name,
			Type:     columnType,
			Nullable: queryProjectionNullable(projection.Nullability),
		})
	}
	if len(analysis.OutputColumns) == 0 {
		analysis.OutputNamesComplete = false
		analysis.OutputTypesComplete = false
	}
}

func queryProjectionNullable(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "non_null":
		value := false
		return &value
	case "nullable":
		value := true
		return &value
	default:
		return nil
	}
}

func isSyntheticProjectionName(name string, index int) bool {
	return strings.EqualFold(name, fmt.Sprintf("_col_%d", index))
}

func queryProjectionType(projection QueryProjection, schema Schema) string {
	if projection.TypeHint != nil && strings.TrimSpace(*projection.TypeHint) != "" {
		return normalizeInferredType(*projection.TypeHint)
	}
	if projection.CastType != nil && strings.TrimSpace(*projection.CastType) != "" {
		return normalizeInferredType(*projection.CastType)
	}

	var inferred string
	for _, upstream := range projection.Upstream {
		for _, candidate := range []*string{upstream.Table, upstream.SourceName} {
			if candidate == nil || strings.TrimSpace(*candidate) == "" {
				continue
			}
			columns, ok := schemaForName(*candidate, schema)
			if !ok {
				continue
			}
			for columnName, columnType := range columns {
				if !strings.EqualFold(strings.TrimSpace(columnName), strings.TrimSpace(upstream.Column)) || strings.TrimSpace(columnType) == "" {
					continue
				}
				normalized := normalizeInferredType(columnType)
				if inferred != "" && normalizedTypeText(inferred) != normalizedTypeText(normalized) {
					return ""
				}
				inferred = normalized
			}
		}
	}
	return inferred
}

func queryAnalysisKey(query, optionsJSON string) [sha256.Size]byte {
	return sha256.Sum256([]byte(query + "\x00" + optionsJSON))
}

type queryAnalysisCache struct {
	mu       sync.Mutex
	capacity int
	entries  map[[sha256.Size]byte]*list.Element
	lru      *list.List
}

type queryAnalysisCacheEntry struct {
	key      [sha256.Size]byte
	analysis QueryAnalysis
}

func newQueryAnalysisCache(capacity int) *queryAnalysisCache {
	if capacity < 1 {
		capacity = 1
	}
	return &queryAnalysisCache{
		capacity: capacity,
		entries:  make(map[[sha256.Size]byte]*list.Element, capacity),
		lru:      list.New(),
	}
}

func (c *queryAnalysisCache) get(key [sha256.Size]byte) (QueryAnalysis, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return QueryAnalysis{}, false
	}
	c.lru.MoveToFront(element)
	return element.Value.(queryAnalysisCacheEntry).analysis, true
}

func (c *queryAnalysisCache) add(key [sha256.Size]byte, analysis QueryAnalysis) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.entries[key]; ok {
		existing.Value = queryAnalysisCacheEntry{key: key, analysis: analysis}
		c.lru.MoveToFront(existing)
		return
	}
	element := c.lru.PushFront(queryAnalysisCacheEntry{key: key, analysis: analysis})
	c.entries[key] = element
	if c.lru.Len() <= c.capacity {
		return
	}
	oldest := c.lru.Back()
	delete(c.entries, oldest.Value.(queryAnalysisCacheEntry).key)
	c.lru.Remove(oldest)
}
