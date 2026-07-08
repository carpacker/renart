package service

import (
	"context"
	"fmt"
	neturl "net/url"
	"sort"
	"strings"
)

// OpenAPIEndpointSuggestion is one request-URL candidate derived from an
// OpenAPI spec's paths (base server URL + path template).
type OpenAPIEndpointSuggestion struct {
	URL     string `json:"url"`
	Method  string `json:"method"`
	Summary string `json:"summary,omitempty"`
}

// OpenAPIRecordsPathSuggestion is one candidate value for `response.records_path`
// — a dot path into the selected endpoint's response schema that yields records.
type OpenAPIRecordsPathSuggestion struct {
	Path   string `json:"path"`
	Detail string `json:"detail,omitempty"`
}

// OpenAPISuggestionsResult feeds the API-asset editor's intellisense for the
// `request.url` and `response.records_path` fields.
type OpenAPISuggestionsResult struct {
	Status       string                         `json:"status"`
	RequestURLs  []OpenAPIEndpointSuggestion    `json:"request_urls"`
	RecordsPaths []OpenAPIRecordsPathSuggestion `json:"records_paths"`
	Error        string                         `json:"error,omitempty"`
}

const maxRecordsPathDepth = 4

// OpenAPISuggestions fetches (cached) the OpenAPI document at openapiURL and
// derives editor completions: the list of request URLs from the spec's paths,
// and — when requestURL identifies a path — the record paths available in that
// operation's response schema. An empty openapiURL yields an empty, non-error
// result so the caller can pass through whatever the asset currently has.
func (s *SuggestionsService) OpenAPISuggestions(ctx context.Context, openapiURL, requestURL, method string) (OpenAPISuggestionsResult, *APIError) {
	openapiURL = strings.TrimSpace(openapiURL)
	if openapiURL == "" {
		return OpenAPISuggestionsResult{Status: "ok"}, nil
	}
	doc, err := fetchOpenAPIDocument(ctx, openapiURL)
	if err != nil {
		return OpenAPISuggestionsResult{}, &APIError{Status: 400, Code: "openapi_fetch_failed", Message: err.Error()}
	}
	if doc == nil {
		return OpenAPISuggestionsResult{Status: "ok"}, nil
	}

	result := OpenAPISuggestionsResult{
		Status:       "ok",
		RequestURLs:  doc.requestURLSuggestions(openapiURL),
		RecordsPaths: doc.recordsPathSuggestions(requestURL, method),
	}
	return result, nil
}

// baseURL resolves the server URL requests are made against: the first OpenAPI
// 3.x server, or the swagger 2.0 host/scheme/basePath, falling back to the
// origin of the spec URL itself (weather.gov serves the spec and the API from
// the same host).
func (doc *openAPIDocument) baseURL(specURL string) string {
	specOrigin := ""
	if parsed, err := neturl.Parse(strings.TrimSpace(specURL)); err == nil && parsed.Host != "" {
		specOrigin = parsed.Scheme + "://" + parsed.Host
	}

	if len(doc.Servers) > 0 {
		server := strings.TrimSpace(doc.Servers[0].URL)
		if server != "" {
			if strings.HasPrefix(server, "/") {
				return strings.TrimRight(specOrigin+server, "/")
			}
			return strings.TrimRight(server, "/")
		}
	}

	if strings.TrimSpace(doc.Host) != "" {
		scheme := "https"
		for _, candidate := range doc.Schemes {
			if strings.EqualFold(strings.TrimSpace(candidate), "https") {
				scheme = "https"
				break
			}
			if strings.TrimSpace(candidate) != "" {
				scheme = strings.ToLower(strings.TrimSpace(candidate))
			}
		}
		return strings.TrimRight(scheme+"://"+strings.TrimSpace(doc.Host)+strings.TrimSpace(doc.BasePath), "/")
	}

	return specOrigin
}

func (doc *openAPIDocument) requestURLSuggestions(specURL string) []OpenAPIEndpointSuggestion {
	base := doc.baseURL(specURL)
	paths := make([]string, 0, len(doc.Paths))
	for path := range doc.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	suggestions := make([]OpenAPIEndpointSuggestion, 0, len(paths))
	for _, path := range paths {
		method, operation := doc.preferredOperation(doc.Paths[path])
		if method == "" {
			continue
		}
		summary := strings.TrimSpace(operation.Summary)
		if summary == "" {
			summary = firstLine(operation.Description)
		}
		suggestions = append(suggestions, OpenAPIEndpointSuggestion{
			URL:     base + path,
			Method:  strings.ToUpper(method),
			Summary: summary,
		})
	}
	return suggestions
}

// preferredOperation picks the operation a data-ingestion asset most likely
// wants: GET when present, otherwise the alphabetically-first HTTP method so the
// choice is deterministic.
func (doc *openAPIDocument) preferredOperation(methods map[string]openAPIOperation) (string, openAPIOperation) {
	if op, ok := methods["get"]; ok {
		return "get", op
	}
	candidates := make([]string, 0, len(methods))
	for method := range methods {
		if isOpenAPIMethod(method) {
			candidates = append(candidates, strings.ToLower(method))
		}
	}
	if len(candidates) == 0 {
		return "", openAPIOperation{}
	}
	sort.Strings(candidates)
	return candidates[0], methods[candidates[0]]
}

func (doc *openAPIDocument) recordsPathSuggestions(requestURL, method string) []OpenAPIRecordsPathSuggestion {
	spec := nativeAPISpec{}
	spec.Request.URL = strings.TrimSpace(requestURL)
	spec.Request.Method = strings.TrimSpace(method)
	if spec.Request.URL == "" {
		return nil
	}

	schema, err := doc.responseSchema(spec, spec.Request.URL)
	if err != nil || schema == nil {
		return nil
	}

	seen := map[string]bool{}
	suggestions := make([]OpenAPIRecordsPathSuggestion, 0, 8)
	add := func(path, detail string) {
		if seen[path] {
			return
		}
		seen[path] = true
		suggestions = append(suggestions, OpenAPIRecordsPathSuggestion{Path: path, Detail: detail})
	}

	rootDetail := "response root"
	if schemaHasType(doc.resolveSchema(schema, nil), "array") {
		rootDetail = "response root (array of records)"
	} else if len(doc.schemaProperties(schema)) > 0 {
		rootDetail = "response root (object)"
	}
	add("", rootDetail)

	doc.walkRecordsPaths(schema, nil, 0, add)
	return suggestions
}

// walkRecordsPaths surfaces every dot path whose value is an array — the shape
// records_path is meant to point at (each array element becomes one record) —
// descending through nested objects and array items up to a bounded depth.
func (doc *openAPIDocument) walkRecordsPaths(schema *openAPISchema, prefix []string, depth int, add func(path, detail string)) {
	if depth >= maxRecordsPathDepth {
		return
	}
	properties := doc.schemaProperties(schema)
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		property := doc.resolveSchema(properties[name], nil)
		path := append(append([]string{}, prefix...), name)
		joined := strings.Join(path, ".")

		if schemaHasType(property, "array") && property.Items != nil {
			item := doc.arrayItemSchema(property)
			add(joined, "array of "+recordItemLabel(doc, item))
			doc.walkRecordsPaths(item, path, depth+1, add)
			continue
		}
		if len(doc.schemaProperties(property)) > 0 {
			doc.walkRecordsPaths(property, path, depth+1, add)
		}
	}
}

func recordItemLabel(doc *openAPIDocument, schema *openAPISchema) string {
	resolved := doc.resolveSchema(schema, nil)
	if resolved == nil {
		return "records"
	}
	if fields := doc.schemaProperties(resolved); len(fields) > 0 {
		return fmt.Sprintf("objects (%d fields)", len(fields))
	}
	if types := schemaTypes(resolved); len(types) > 0 {
		return types[0]
	}
	return "records"
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if index := strings.IndexAny(text, "\r\n"); index >= 0 {
		text = text[:index]
	}
	const maxSummaryLen = 120
	if len(text) > maxSummaryLen {
		text = strings.TrimSpace(text[:maxSummaryLen]) + "…"
	}
	return text
}
