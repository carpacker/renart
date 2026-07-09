package service

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A path item mixes real HTTP methods with a path-level `parameters` sequence
// and scalar keys (`summary`, `$ref`) — the shape api.weather.gov uses. This
// used to fail the whole document with "cannot unmarshal !!seq into
// service.openAPIOperation".
func TestOpenAPIDocumentToleratesNonOperationPathKeys(t *testing.T) {
	const spec = `
openapi: 3.0.0
paths:
  /alerts:
    summary: Active alerts
    parameters:
      - name: status
        in: query
      - name: area
        in: query
    get:
      operationId: listAlerts
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
`
	var doc openAPIDocument
	if err := yaml.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	methods, ok := doc.Paths["/alerts"]
	if !ok {
		t.Fatal("path /alerts was not parsed")
	}
	get, ok := methods["get"]
	if !ok {
		t.Fatalf("get operation missing; parsed keys: %+v", methods)
	}
	if get.OperationID != "listAlerts" {
		t.Fatalf("operationId = %q, want listAlerts", get.OperationID)
	}
	if len(get.Responses) == 0 {
		t.Fatal("get responses were not parsed")
	}

	// The non-operation keys decoded into empty operations; the lookup ignores
	// them because they are not HTTP methods.
	if isOpenAPIMethod("parameters") || isOpenAPIMethod("summary") {
		t.Fatal("parameters/summary must not count as OpenAPI methods")
	}
}

// A per-operation response can be a `$ref` to a shared response component
// instead of an inline object — the shape api.weather.gov uses for GET /alerts.
// Without resolving that ref, the response has no content and column inference
// silently produces nothing ("could not be inferred from ... OpenAPI metadata").
func TestOpenAPIResponseRefResolvesToSchema(t *testing.T) {
	const spec = `
openapi: 3.0.0
paths:
  /alerts:
    get:
      operationId: listAlerts
      responses:
        "200":
          $ref: "#/components/responses/AlertCollection"
components:
  responses:
    AlertCollection:
      description: A collection of alerts.
      content:
        application/geo+json:
          schema:
            $ref: "#/components/schemas/AlertCollectionGeoJson"
  schemas:
    AlertCollectionGeoJson:
      type: object
      properties:
        type:
          type: string
        updated:
          type: string
          format: date-time
        features:
          type: array
          items:
            type: object
`
	var doc openAPIDocument
	if err := yaml.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	nativeSpec := nativeAPISpec{}
	nativeSpec.Request.URL = "https://api.weather.gov/alerts"
	nativeSpec.Request.Method = "GET"

	schema, err := doc.responseSchema(nativeSpec, nativeSpec.Request.URL)
	if err != nil {
		t.Fatalf("responseSchema returned error: %v", err)
	}
	if schema == nil {
		t.Fatal("response schema was nil — the response $ref was not resolved")
	}

	record := doc.arrayItemSchema(doc.schemaAtPath(schema, ""))
	properties := doc.schemaProperties(record)
	for _, name := range []string{"type", "updated", "features"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("expected property %q to be inferred; got %v", name, properties)
		}
	}
	if got := doc.columnType(properties["updated"]); got != "timestamp" {
		t.Fatalf("updated column type = %q, want timestamp", got)
	}
}

func TestOpenAPIOperationMatchesServerBasePath(t *testing.T) {
	const spec = `
openapi: 3.0.0
servers:
  - url: https://api.example.test/v4
paths:
  /anime:
    get:
      operationId: getAnimeSearch
      responses:
        "200":
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      type: object
`
	var doc openAPIDocument
	if err := yaml.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	nativeSpec := nativeAPISpec{}
	nativeSpec.Request.URL = "https://api.example.test/v4/anime?q=naruto"
	nativeSpec.Request.Method = "GET"

	schema, err := doc.responseSchema(nativeSpec, nativeSpec.Request.URL)
	if err != nil {
		t.Fatalf("responseSchema returned error: %v", err)
	}
	if schema == nil {
		t.Fatal("response schema was nil")
	}

	recordsPaths := doc.recordsPathSuggestions(nativeSpec.Request.URL, nativeSpec.Request.Method)
	if !containsRecordsPath(recordsPaths, "data") {
		t.Fatalf("records paths did not include data: %+v", recordsPaths)
	}
}

// When a property is defined across several allOf branches (a base object plus
// a narrowing override — the GeoJSON feature-collection shape), the record
// schema at a records_path must deep-merge them. Last-write-wins used to keep
// only the override branch's `features` items and infer a single `properties`
// column instead of the feature's id/type/geometry/properties.
func TestRecordsPathDeepMergesAllOfBranches(t *testing.T) {
	const spec = `
openapi: 3.0.0
paths:
  /alerts:
    get:
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/AlertCollectionGeoJson"
components:
  schemas:
    AlertCollectionGeoJson:
      allOf:
        - $ref: "#/components/schemas/FeatureCollection"
        - type: object
          properties:
            features:
              type: array
              items:
                type: object
                properties:
                  properties:
                    $ref: "#/components/schemas/Alert"
    FeatureCollection:
      type: object
      properties:
        type:
          type: string
        features:
          type: array
          items:
            $ref: "#/components/schemas/Feature"
    Feature:
      type: object
      properties:
        id:
          type: string
        type:
          type: string
        geometry:
          type: object
        properties:
          type: object
    Alert:
      type: object
      properties:
        event:
          type: string
`
	var doc openAPIDocument
	if err := yaml.Unmarshal([]byte(spec), &doc); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	nativeSpec := nativeAPISpec{}
	nativeSpec.Request.URL = "https://example.test/alerts"
	nativeSpec.Request.Method = "GET"
	nativeSpec.Response.RecordsPath = "features"

	schema, err := doc.responseSchema(nativeSpec, nativeSpec.Request.URL)
	if err != nil {
		t.Fatalf("responseSchema returned error: %v", err)
	}
	record := doc.arrayItemSchema(doc.schemaAtPath(schema, nativeSpec.Response.RecordsPath))
	properties := doc.schemaProperties(record)
	for _, name := range []string{"id", "type", "geometry", "properties"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("feature column %q missing after allOf merge; got %v", name, keysOf(properties))
		}
	}
	// The override narrowed `properties` to the Alert object — the merge keeps it.
	if got := doc.columnType(properties["properties"]); got != "json" {
		t.Fatalf("properties column type = %q, want json", got)
	}
	if got := doc.columnType(properties["geometry"]); got != "json" {
		t.Fatalf("geometry column type = %q, want json", got)
	}
}

func TestOpenAPIValidationAllowsNullOptionalProperty(t *testing.T) {
	doc := &openAPIDocument{}
	schema := &openAPISchema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]*openAPISchema{
			"id":          {Type: "string"},
			"description": {Type: "string"},
		},
	}

	messages := doc.validateValue(map[string]any{
		"id":          "alert-1",
		"description": nil,
	}, schema, "$", nil)
	if len(messages) > 0 {
		t.Fatalf("optional null should validate as absent, got %v", messages)
	}
}

func TestOpenAPIValidationRejectsNullRequiredProperty(t *testing.T) {
	doc := &openAPIDocument{}
	schema := &openAPISchema{
		Type:     "object",
		Required: []string{"description"},
		Properties: map[string]*openAPISchema{
			"description": {Type: "string"},
		},
	}

	messages := doc.validateValue(map[string]any{
		"description": nil,
	}, schema, "$", nil)
	if !containsValidationMessage(messages, "$.description is null") {
		t.Fatalf("required null should fail validation, got %v", messages)
	}
}

func containsValidationMessage(messages []string, want string) bool {
	for _, message := range messages {
		if strings.Contains(message, want) {
			return true
		}
	}
	return false
}

func containsRecordsPath(paths []OpenAPIRecordsPathSuggestion, want string) bool {
	for _, path := range paths {
		if path.Path == want {
			return true
		}
	}
	return false
}

func keysOf(m map[string]*openAPISchema) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}
