package service

import (
	"context"
	"testing"

	"gopkg.in/yaml.v3"
)

const suggestionsSpec = `
openapi: 3.0.0
servers:
  - url: https://api.example.test
paths:
  /alerts:
    get:
      summary: Returns all alerts
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  title:
                    type: string
                  pagination:
                    type: object
                    properties:
                      next:
                        type: string
                      has_more:
                        type: boolean
                  features:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: string
                        properties:
                          type: object
  /alerts/{id}:
    get:
      summary: Returns a single alert
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
`

func suggestionsDoc(t *testing.T) *openAPIDocument {
	t.Helper()
	var doc openAPIDocument
	if err := yaml.Unmarshal([]byte(suggestionsSpec), &doc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	return &doc
}

func TestRequestURLSuggestionsUseServerBase(t *testing.T) {
	doc := suggestionsDoc(t)
	urls := doc.requestURLSuggestions("https://api.example.test/openapi.json")
	if len(urls) != 2 {
		t.Fatalf("expected 2 request URLs, got %d: %+v", len(urls), urls)
	}
	// Sorted by path; base comes from the server, not the spec URL path.
	if urls[0].URL != "https://api.example.test/alerts" || urls[0].Method != "GET" {
		t.Fatalf("unexpected first suggestion: %+v", urls[0])
	}
	if urls[0].Summary != "Returns all alerts" {
		t.Fatalf("summary = %q, want the operation summary", urls[0].Summary)
	}
	if urls[1].URL != "https://api.example.test/alerts/{id}" {
		t.Fatalf("second suggestion = %q, want the templated path", urls[1].URL)
	}
}

func TestRequestURLBaseFallsBackToSpecOrigin(t *testing.T) {
	doc := suggestionsDoc(t)
	doc.Servers = nil // force the spec-origin fallback
	urls := doc.requestURLSuggestions("https://fallback.test/v1/openapi.json")
	if len(urls) == 0 || urls[0].URL != "https://fallback.test/alerts" {
		t.Fatalf("expected origin fallback base, got %+v", urls)
	}
}

func TestOpenAPISuggestionsReturnsEmptyArraysWithoutOpenAPIURL(t *testing.T) {
	service := SuggestionsService{}
	result, apiErr := service.OpenAPISuggestions(context.Background(), "", "", "")
	if apiErr != nil {
		t.Fatalf("OpenAPISuggestions failed: %v", apiErr)
	}
	if result.Status != "ok" {
		t.Fatalf("status = %q, want ok", result.Status)
	}
	if result.RequestURLs == nil {
		t.Fatal("RequestURLs is nil, want an empty slice")
	}
	if result.RecordsPaths == nil {
		t.Fatal("RecordsPaths is nil, want an empty slice")
	}
	if result.ResponsePaths == nil {
		t.Fatal("ResponsePaths is nil, want an empty slice")
	}
}

func TestRecordsPathSuggestionsSurfaceArrays(t *testing.T) {
	doc := suggestionsDoc(t)
	paths := doc.recordsPathSuggestions("https://api.example.test/alerts", "GET")

	byPath := map[string]string{}
	for _, suggestion := range paths {
		byPath[suggestion.Path] = suggestion.Detail
	}
	if _, ok := byPath[""]; !ok {
		t.Fatalf("root records path missing: %+v", paths)
	}
	detail, ok := byPath["features"]
	if !ok {
		t.Fatalf("array records path 'features' missing: %+v", paths)
	}
	if detail == "" {
		t.Fatal("expected a detail describing the features array")
	}
}

func TestRecordsPathSuggestionsEmptyWithoutRequestURL(t *testing.T) {
	doc := suggestionsDoc(t)
	if got := doc.recordsPathSuggestions("", "GET"); got != nil {
		t.Fatalf("expected nil without a request URL, got %+v", got)
	}
}

func TestResponsePathSuggestionsSurfaceScalarPaths(t *testing.T) {
	doc := suggestionsDoc(t)
	paths := doc.responsePathSuggestions("https://api.example.test/alerts", "GET")

	byPath := map[string]string{}
	for _, suggestion := range paths {
		byPath[suggestion.Path] = suggestion.Detail
	}
	if byPath["pagination.next"] != "string" {
		t.Fatalf("pagination.next suggestion = %q, want string; all paths: %+v", byPath["pagination.next"], paths)
	}
	if byPath["pagination.has_more"] != "boolean" {
		t.Fatalf("pagination.has_more suggestion = %q, want boolean; all paths: %+v", byPath["pagination.has_more"], paths)
	}
}
