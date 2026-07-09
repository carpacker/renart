package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"gopkg.in/yaml.v3"
)

const maxOpenAPISpecBytes = 20 << 20
const openAPIDocumentCacheTTL = 10 * time.Minute

var openAPIDocumentCache sync.Map

type openAPIDocumentCacheEntry struct {
	document  *openAPIDocument
	fetchedAt time.Time
}

type openAPIDocument struct {
	OpenAPI     string                                 `yaml:"openapi"`
	Swagger     string                                 `yaml:"swagger"`
	Servers     []openAPIServer                        `yaml:"servers"`
	Host        string                                 `yaml:"host"`     // swagger 2.0
	BasePath    string                                 `yaml:"basePath"` // swagger 2.0
	Schemes     []string                               `yaml:"schemes"`  // swagger 2.0
	Paths       map[string]map[string]openAPIOperation `yaml:"paths"`
	Components  openAPIComponents                      `yaml:"components"`
	Definitions map[string]*openAPISchema              `yaml:"definitions"`
	// Responses holds swagger 2.0 top-level shared responses (`#/responses/...`).
	Responses map[string]*openAPIResponse `yaml:"responses"`
}

type openAPIServer struct {
	URL         string `yaml:"url"`
	Description string `yaml:"description"`
}

type openAPIComponents struct {
	Schemas map[string]*openAPISchema `yaml:"schemas"`
	// Responses holds OpenAPI 3.x shared responses (`#/components/responses/...`).
	Responses map[string]*openAPIResponse `yaml:"responses"`
}

type openAPIOperation struct {
	OperationID string                     `yaml:"operationId"`
	Summary     string                     `yaml:"summary"`
	Description string                     `yaml:"description"`
	Responses   map[string]openAPIResponse `yaml:"responses"`
}

// UnmarshalYAML tolerates OpenAPI path-item keys that are not operations. A path
// item mixes HTTP methods (get/post/…) with `parameters` (a sequence),
// `servers` (a sequence), and `$ref`/`summary`/`description` (scalars). Those
// share the `map[string]openAPIOperation` value type, so without this a
// path-level `parameters:` list fails the whole document with "cannot unmarshal
// !!seq into service.openAPIOperation". Non-mapping nodes decode into an empty
// operation and are ignored by the method/operationId lookup.
func (o *openAPIOperation) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	type rawOperation openAPIOperation
	var raw rawOperation
	if err := node.Decode(&raw); err != nil {
		return err
	}
	*o = openAPIOperation(raw)
	return nil
}

type openAPIResponse struct {
	Ref     string                      `yaml:"$ref"`
	Content map[string]openAPIMediaType `yaml:"content"`
	Schema  *openAPISchema              `yaml:"schema"`
}

type openAPIMediaType struct {
	Schema *openAPISchema `yaml:"schema"`
}

type openAPISchema struct {
	Ref                  string                    `yaml:"$ref"`
	Type                 any                       `yaml:"type"`
	Format               string                    `yaml:"format"`
	Nullable             bool                      `yaml:"nullable"`
	Items                *openAPISchema            `yaml:"items"`
	Properties           map[string]*openAPISchema `yaml:"properties"`
	Required             []string                  `yaml:"required"`
	AllOf                []*openAPISchema          `yaml:"allOf"`
	AnyOf                []*openAPISchema          `yaml:"anyOf"`
	OneOf                []*openAPISchema          `yaml:"oneOf"`
	AdditionalProperties any                       `yaml:"additionalProperties"`
}

type apiOpenAPIValidator struct {
	doc  *openAPIDocument
	spec nativeAPISpec
}

func newAPIOpenAPIValidator(ctx context.Context, spec nativeAPISpec) (*apiOpenAPIValidator, error) {
	if apiOpenAPIURL(spec) == "" {
		return nil, nil
	}
	doc, err := fetchOpenAPIDocument(ctx, apiOpenAPIURL(spec))
	if err != nil {
		return nil, err
	}
	return &apiOpenAPIValidator{doc: doc, spec: spec}, nil
}

func (v *apiOpenAPIValidator) Validate(value any, requestURL string) error {
	if v == nil || v.doc == nil {
		return nil
	}
	schema, err := v.doc.responseSchema(v.spec, requestURL)
	if err != nil {
		return err
	}
	if schema == nil {
		return nil
	}
	if messages := v.doc.validateValue(value, schema, "$", nil); len(messages) > 0 {
		if len(messages) > 5 {
			messages = append(messages[:5], fmt.Sprintf("...and %d more OpenAPI validation errors", len(messages)-5))
		}
		return fmt.Errorf("api response does not match OpenAPI schema: %s", strings.Join(messages, "; "))
	}
	return nil
}

func apiOpenAPIColumns(ctx context.Context, spec nativeAPISpec) ([]pipeline.Column, error) {
	if apiOpenAPIURL(spec) == "" {
		return nil, nil
	}
	doc, err := fetchOpenAPIDocument(ctx, apiOpenAPIURL(spec))
	if err != nil {
		return nil, err
	}
	schema, err := doc.responseSchema(spec, spec.Request.URL)
	if err != nil {
		return nil, err
	}
	recordSchema := doc.schemaAtPath(schema, spec.Response.RecordsPath)
	recordSchema = doc.arrayItemSchema(recordSchema)
	if recordSchema == nil {
		return nil, nil
	}

	if len(spec.Response.Fields) > 0 {
		fieldNames := sortedFieldNames(spec.Response.Fields)
		columns := make([]pipeline.Column, 0, len(fieldNames))
		for _, fieldName := range fieldNames {
			fieldSchema := doc.schemaAtPath(recordSchema, spec.Response.Fields[fieldName])
			columns = append(columns, pipeline.Column{Name: fieldName, Type: doc.columnType(fieldSchema)})
		}
		return columns, nil
	}

	properties := doc.schemaProperties(recordSchema)
	if len(properties) == 0 {
		columnType := doc.columnType(recordSchema)
		if columnType == "" {
			columnType = "json"
		}
		return []pipeline.Column{{Name: "value", Type: columnType}}, nil
	}

	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	columns := make([]pipeline.Column, 0, len(names))
	for _, name := range names {
		columns = append(columns, pipeline.Column{Name: name, Type: doc.columnType(properties[name])})
	}
	return columns, nil
}

func apiOpenAPIURL(spec nativeAPISpec) string {
	if value := strings.TrimSpace(spec.OpenAPI.URL); value != "" {
		return value
	}
	return strings.TrimSpace(spec.OpenAPIURL)
}

func apiOpenAPIValidationMode(spec nativeAPISpec) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(spec.OpenAPI.Validation))
	if mode == "" {
		return "warn", nil
	}
	switch mode {
	case "off", "warn", "error":
		return mode, nil
	default:
		return "", fmt.Errorf("openapi.validation must be off, warn, or error, got %q", spec.OpenAPI.Validation)
	}
}

func fetchOpenAPIDocument(ctx context.Context, specURL string) (*openAPIDocument, error) {
	specURL = strings.TrimSpace(specURL)
	if specURL == "" {
		return nil, nil
	}
	if cached, ok := openAPIDocumentCache.Load(specURL); ok {
		if entry, ok := cached.(openAPIDocumentCacheEntry); ok && entry.document != nil && time.Since(entry.fetchedAt) < openAPIDocumentCacheTTL {
			return entry.document, nil
		}
		openAPIDocumentCache.Delete(specURL)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOpenAPISpecBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxOpenAPISpecBytes {
		return nil, errors.New("OpenAPI spec is too large")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenAPI spec request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var doc openAPIDocument
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	openAPIDocumentCache.Store(specURL, openAPIDocumentCacheEntry{document: &doc, fetchedAt: time.Now()})
	return &doc, nil
}

func (doc *openAPIDocument) responseSchema(spec nativeAPISpec, requestURL string) (*openAPISchema, error) {
	op, err := doc.operation(spec, requestURL)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, nil
	}
	response := selectOpenAPIResponse(op.Responses, spec.OpenAPI.ResponseStatus)
	if response == nil {
		return nil, nil
	}
	response = doc.resolveResponse(response, nil)
	if response == nil {
		return nil, nil
	}
	if response.Schema != nil {
		return doc.resolveSchema(response.Schema, nil), nil
	}
	if len(response.Content) == 0 {
		return nil, nil
	}
	contentTypes := make([]string, 0, len(response.Content))
	for contentType := range response.Content {
		contentTypes = append(contentTypes, contentType)
	}
	sort.Strings(contentTypes)
	selected := ""
	for _, contentType := range contentTypes {
		if strings.Contains(strings.ToLower(contentType), "json") {
			selected = contentType
			break
		}
	}
	if selected == "" {
		selected = contentTypes[0]
	}
	return doc.resolveSchema(response.Content[selected].Schema, nil), nil
}

func (doc *openAPIDocument) operation(spec nativeAPISpec, requestURL string) (*openAPIOperation, error) {
	operationID := strings.TrimSpace(spec.OpenAPI.OperationID)
	method := strings.ToLower(strings.TrimSpace(spec.OpenAPI.Method))
	if method == "" {
		method = strings.ToLower(strings.TrimSpace(spec.Request.Method))
	}
	if method == "" {
		method = http.MethodGet
	}
	method = strings.ToLower(method)

	if operationID != "" {
		for _, methods := range doc.Paths {
			for candidateMethod, op := range methods {
				if !isOpenAPIMethod(candidateMethod) {
					continue
				}
				if op.OperationID == operationID {
					return &op, nil
				}
			}
		}
		return nil, fmt.Errorf("OpenAPI operation_id %q was not found", operationID)
	}

	path := strings.TrimSpace(spec.OpenAPI.Path)
	if path == "" {
		path = requestPath(requestURL)
	}
	if path == "" {
		path = requestPath(spec.Request.URL)
	}
	if path == "" {
		return nil, errors.New("OpenAPI path could not be inferred from request.url")
	}

	for _, candidatePath := range doc.operationPathCandidates(path) {
		if methods, ok := doc.Paths[candidatePath]; ok {
			if op, ok := methods[method]; ok {
				return &op, nil
			}
		}
	}
	for _, candidatePath := range doc.operationPathCandidates(path) {
		for template, methods := range doc.Paths {
			if !openAPIPathMatches(template, candidatePath) {
				continue
			}
			if op, ok := methods[method]; ok {
				return &op, nil
			}
		}
	}
	return nil, fmt.Errorf("OpenAPI operation %s %s was not found", strings.ToUpper(method), path)
}

func (doc *openAPIDocument) operationPathCandidates(path string) []string {
	path = normalizeOpenAPIPath(path)
	candidates := []string{path}
	seen := map[string]bool{path: true}
	add := func(candidate string) {
		candidate = normalizeOpenAPIPath(candidate)
		if candidate == "" || seen[candidate] {
			return
		}
		seen[candidate] = true
		candidates = append(candidates, candidate)
	}

	for _, server := range doc.Servers {
		serverPath := openAPIServerPath(server.URL)
		if serverPath == "" || serverPath == "/" {
			continue
		}
		if stripped, ok := stripOpenAPIPathPrefix(path, serverPath); ok {
			add(stripped)
		}
	}
	if strings.TrimSpace(doc.BasePath) != "" {
		basePath := normalizeOpenAPIPath(doc.BasePath)
		if stripped, ok := stripOpenAPIPathPrefix(path, basePath); ok {
			add(stripped)
		}
	}
	return candidates
}

func stripOpenAPIPathPrefix(path, prefix string) (string, bool) {
	path = normalizeOpenAPIPath(path)
	prefix = normalizeOpenAPIPath(prefix)
	if prefix == "" || prefix == "/" || path == prefix {
		return "/", path == prefix
	}
	if !strings.HasPrefix(path, prefix+"/") {
		return "", false
	}
	return strings.TrimPrefix(path, prefix), true
}

func normalizeOpenAPIPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	if len(trimmed) > 1 {
		trimmed = strings.TrimRight(trimmed, "/")
	}
	return trimmed
}

func openAPIServerPath(rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	if strings.HasPrefix(rawURL, "/") {
		return normalizeOpenAPIPath(rawURL)
	}
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return ""
	}
	return normalizeOpenAPIPath(parsed.Path)
}

func selectOpenAPIResponse(responses map[string]openAPIResponse, preferredStatus string) *openAPIResponse {
	if len(responses) == 0 {
		return nil
	}
	preferredStatus = strings.TrimSpace(preferredStatus)
	if preferredStatus != "" {
		if response, ok := responses[preferredStatus]; ok {
			return &response
		}
	}
	for _, status := range []string{"200", "201", "202", "default"} {
		if response, ok := responses[status]; ok {
			return &response
		}
	}
	statuses := make([]string, 0, len(responses))
	for status := range responses {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	for _, status := range statuses {
		if strings.HasPrefix(status, "2") {
			response := responses[status]
			return &response
		}
	}
	response := responses[statuses[0]]
	return &response
}

func requestPath(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "/") {
		return trimmed
	}
	parsed, err := neturl.Parse(trimmed)
	if err != nil {
		return ""
	}
	return parsed.Path
}

func openAPIPathMatches(template, actual string) bool {
	templateParts := splitPath(template)
	actualParts := splitPath(actual)
	if len(templateParts) != len(actualParts) {
		return false
	}
	for index := range templateParts {
		if templateParts[index] == actualParts[index] {
			continue
		}
		if isOpenAPIPathParam(templateParts[index]) || isJinjaPathParam(actualParts[index]) {
			continue
		}
		return false
	}
	return true
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func isOpenAPIPathParam(value string) bool {
	return strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") || strings.HasPrefix(value, ":")
}

func isJinjaPathParam(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "{{") && strings.HasSuffix(trimmed, "}}")
}

func isOpenAPIMethod(method string) bool {
	switch strings.ToLower(method) {
	case "get", "put", "post", "delete", "patch", "head", "options", "trace":
		return true
	default:
		return false
	}
}

func (doc *openAPIDocument) schemaAtPath(schema *openAPISchema, path string) *openAPISchema {
	current := doc.resolveSchema(schema, nil)
	for _, part := range strings.Split(strings.TrimSpace(path), ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		current = doc.arrayItemSchema(current)
		properties := doc.schemaProperties(current)
		current = properties[part]
		if current == nil {
			return nil
		}
	}
	return doc.resolveSchema(current, nil)
}

func (doc *openAPIDocument) arrayItemSchema(schema *openAPISchema) *openAPISchema {
	resolved := doc.resolveSchema(schema, nil)
	if resolved == nil {
		return nil
	}
	if schemaHasType(resolved, "array") && resolved.Items != nil {
		return doc.resolveSchema(resolved.Items, nil)
	}
	return resolved
}

// resolveResponse follows a response object's `$ref` to a shared response
// component. OpenAPI 3.x keeps these under `#/components/responses/...` and
// swagger 2.0 under `#/responses/...`; a per-operation `200: {$ref: ...}` is
// common (weather.gov does this), and without resolution the response has no
// content and yields no schema.
func (doc *openAPIDocument) resolveResponse(response *openAPIResponse, seen map[string]bool) *openAPIResponse {
	if response == nil {
		return nil
	}
	ref := strings.TrimSpace(response.Ref)
	if ref == "" {
		return response
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	if seen[ref] {
		return response
	}
	seen[ref] = true
	if strings.HasPrefix(ref, "#/components/responses/") {
		name := strings.TrimPrefix(ref, "#/components/responses/")
		return doc.resolveResponse(doc.Components.Responses[name], seen)
	}
	if strings.HasPrefix(ref, "#/responses/") {
		name := strings.TrimPrefix(ref, "#/responses/")
		return doc.resolveResponse(doc.Responses[name], seen)
	}
	return response
}

func (doc *openAPIDocument) resolveSchema(schema *openAPISchema, seen map[string]bool) *openAPISchema {
	if schema == nil {
		return nil
	}
	ref := strings.TrimSpace(schema.Ref)
	if ref == "" {
		return schema
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	if seen[ref] {
		return schema
	}
	seen[ref] = true
	if strings.HasPrefix(ref, "#/components/schemas/") {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		return doc.resolveSchema(doc.Components.Schemas[name], seen)
	}
	if strings.HasPrefix(ref, "#/definitions/") {
		name := strings.TrimPrefix(ref, "#/definitions/")
		return doc.resolveSchema(doc.Definitions[name], seen)
	}
	return schema
}

func (doc *openAPIDocument) schemaProperties(schema *openAPISchema) map[string]*openAPISchema {
	resolved := doc.resolveSchema(schema, nil)
	if resolved == nil {
		return nil
	}
	properties := map[string]*openAPISchema{}
	// A property defined in more than one allOf branch (or in a branch and the
	// schema's own properties) is deep-merged, not overwritten — OpenAPI allOf
	// semantics. Weather.gov's AlertCollectionGeoJson, for example, defines
	// `features` in a base branch (items → GeoJsonFeature) and narrows it in a
	// second branch (items → { properties: Alert }); last-write-wins would drop
	// the base feature's id/type/geometry and leave a lone `properties` column.
	mergeProperty := func(name string, property *openAPISchema) {
		if existing, ok := properties[name]; ok {
			properties[name] = doc.mergeSchemas(existing, property, 0)
			return
		}
		properties[name] = property
	}
	for _, part := range resolved.AllOf {
		for name, property := range doc.schemaProperties(part) {
			mergeProperty(name, property)
		}
	}
	for name, property := range resolved.Properties {
		mergeProperty(name, property)
	}
	if len(properties) == 0 {
		return nil
	}
	return properties
}

const maxSchemaMergeDepth = 32

// mergeSchemas combines two schemas that describe the same value (overlapping
// allOf branches or a base/override pair), returning a new schema whose
// properties and array items are recursively merged. The depth guard stops
// runaway recursion on self-referential specs (e.g. GeoJSON geometry
// collections).
func (doc *openAPIDocument) mergeSchemas(a, b *openAPISchema, depth int) *openAPISchema {
	left := doc.resolveSchema(a, nil)
	right := doc.resolveSchema(b, nil)
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	if depth >= maxSchemaMergeDepth {
		return left
	}

	merged := &openAPISchema{
		Type:     left.Type,
		Format:   left.Format,
		Nullable: left.Nullable || right.Nullable,
	}
	if len(schemaTypes(left)) == 0 {
		merged.Type = right.Type
	}
	if merged.Format == "" {
		merged.Format = right.Format
	}
	merged.Required = append(append([]string{}, left.Required...), right.Required...)

	properties := map[string]*openAPISchema{}
	for name, property := range doc.schemaProperties(left) {
		properties[name] = property
	}
	for name, property := range doc.schemaProperties(right) {
		if existing, ok := properties[name]; ok {
			properties[name] = doc.mergeSchemas(existing, property, depth+1)
			continue
		}
		properties[name] = property
	}
	if len(properties) > 0 {
		merged.Properties = properties
	}

	if left.Items != nil || right.Items != nil {
		merged.Items = doc.mergeSchemas(left.Items, right.Items, depth+1)
	}
	return merged
}

func (doc *openAPIDocument) columnType(schema *openAPISchema) string {
	resolved := doc.resolveSchema(schema, nil)
	if resolved == nil {
		return ""
	}
	for _, typ := range schemaTypes(resolved) {
		switch typ {
		case "integer":
			return "integer"
		case "number":
			return "float"
		case "boolean":
			return "boolean"
		case "string":
			switch strings.ToLower(strings.TrimSpace(resolved.Format)) {
			case "date-time", "datetime":
				return "timestamp"
			case "date":
				return "date"
			default:
				return "string"
			}
		case "array", "object":
			return "json"
		}
	}
	if len(doc.schemaProperties(resolved)) > 0 || resolved.Items != nil {
		return "json"
	}
	// Composed schemas (oneOf/anyOf, e.g. GeoJSON geometry) carry no single
	// scalar type but still serialize as a nested object → treat as json rather
	// than leaving the column untyped.
	if len(resolved.OneOf) > 0 || len(resolved.AnyOf) > 0 || len(resolved.AllOf) > 0 {
		return "json"
	}
	return ""
}

func (doc *openAPIDocument) validateValue(value any, schema *openAPISchema, path string, seen map[*openAPISchema]bool) []string {
	resolved := doc.resolveSchema(schema, nil)
	if resolved == nil {
		return nil
	}
	if seen == nil {
		seen = map[*openAPISchema]bool{}
	}
	if seen[resolved] {
		return nil
	}
	seen[resolved] = true

	var messages []string
	for _, part := range resolved.AllOf {
		messages = append(messages, doc.validateValue(value, part, path, seen)...)
	}
	if len(resolved.AnyOf) > 0 || len(resolved.OneOf) > 0 {
		options := append([]*openAPISchema{}, resolved.AnyOf...)
		options = append(options, resolved.OneOf...)
		matched := false
		var optionMessages []string
		for _, option := range options {
			candidateMessages := doc.validateValue(value, option, path, nil)
			if len(candidateMessages) == 0 {
				matched = true
				break
			}
			optionMessages = candidateMessages
		}
		if !matched && len(optionMessages) > 0 {
			messages = append(messages, optionMessages...)
		}
	}

	if value == nil {
		if resolved.Nullable || schemaHasType(resolved, "null") || len(schemaTypes(resolved)) == 0 {
			return messages
		}
		return append(messages, path+" is null")
	}

	types := schemaTypes(resolved)
	if len(types) == 0 {
		if len(doc.schemaProperties(resolved)) > 0 {
			types = []string{"object"}
		} else if resolved.Items != nil {
			types = []string{"array"}
		}
	}
	if len(types) == 0 {
		return messages
	}

	for _, typ := range types {
		switch typ {
		case "object":
			object, ok := value.(map[string]any)
			if !ok {
				continue
			}
			messages = append(messages, doc.validateObject(object, resolved, path, seen)...)
			return messages
		case "array":
			array, ok := value.([]any)
			if !ok {
				continue
			}
			if resolved.Items != nil {
				for index, item := range array {
					messages = append(messages, doc.validateValue(item, resolved.Items, fmt.Sprintf("%s[%d]", path, index), nil)...)
				}
			}
			return messages
		case "string":
			if _, ok := value.(string); ok {
				return messages
			}
		case "integer":
			if number, ok := value.(float64); ok && math.Trunc(number) == number {
				return messages
			}
		case "number":
			if _, ok := value.(float64); ok {
				return messages
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return messages
			}
		case "null":
			// handled above
		}
	}
	return append(messages, fmt.Sprintf("%s expected %s, got %T", path, strings.Join(types, "/"), value))
}

func (doc *openAPIDocument) validateObject(object map[string]any, schema *openAPISchema, path string, seen map[*openAPISchema]bool) []string {
	properties := doc.schemaProperties(schema)
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	for _, part := range schema.AllOf {
		resolved := doc.resolveSchema(part, nil)
		if resolved == nil {
			continue
		}
		for _, name := range resolved.Required {
			required[name] = true
		}
	}
	var messages []string
	for name := range required {
		if _, ok := object[name]; !ok {
			messages = append(messages, fmt.Sprintf("%s.%s is required", path, name))
		}
	}
	for name, property := range properties {
		value, ok := object[name]
		if !ok {
			continue
		}
		if value == nil && !required[name] {
			// Many public APIs emit null for optional response fields even when
			// the OpenAPI schema only models the non-null value shape. Treating
			// optional null as absent keeps ingestion resilient without weakening
			// required field validation.
			continue
		}
		messages = append(messages, doc.validateValue(value, property, path+"."+name, seen)...)
	}
	return messages
}

func schemaTypes(schema *openAPISchema) []string {
	if schema == nil {
		return nil
	}
	switch typed := schema.Type.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.ToLower(strings.TrimSpace(typed))}
	case []string:
		values := make([]string, 0, len(typed))
		for _, value := range typed {
			if strings.TrimSpace(value) != "" {
				values = append(values, strings.ToLower(strings.TrimSpace(value)))
			}
		}
		return values
	case []any:
		values := make([]string, 0, len(typed))
		for _, value := range typed {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				values = append(values, strings.ToLower(strings.TrimSpace(text)))
			}
		}
		return values
	default:
		return nil
	}
}

func schemaHasType(schema *openAPISchema, typ string) bool {
	for _, candidate := range schemaTypes(schema) {
		if candidate == typ {
			return true
		}
	}
	return false
}
