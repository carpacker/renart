package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/jinja"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"renart/internal/web/service/assetmeta"
)

const apiAssetType = "api"

type nativeAPISpec struct {
	Name       string            `yaml:"name"`
	Type       string            `yaml:"type"`
	Connection string            `yaml:"connection"`
	Parameters *nativeAPISpec    `yaml:"parameters"`
	OpenAPI    nativeAPIOpenAPI  `yaml:"openapi"`
	OpenAPIURL string            `yaml:"openapi_url"`
	Request    nativeAPIRequest  `yaml:"request"`
	Iterate    nativeAPIIterate  `yaml:"iterate"`
	Response   nativeAPIResponse `yaml:"response"`
	Load       nativeAPILoad     `yaml:"load"`
}

type nativeAPIOpenAPI struct {
	URL            string `yaml:"url"`
	Path           string `yaml:"path"`
	Method         string `yaml:"method"`
	OperationID    string `yaml:"operation_id"`
	ResponseStatus string `yaml:"response_status"`
}

type nativeAPIRequest struct {
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"`
	Headers map[string]string `yaml:"headers"`
}

type nativeAPIIterate struct {
	Over []string `yaml:"over"`
	As   string   `yaml:"as"`
}

type nativeAPIResponse struct {
	RecordsPath string            `yaml:"records_path"`
	Fields      map[string]string `yaml:"fields"`
}

type nativeAPILoad struct {
	Destination string `yaml:"destination"`
	Target      string `yaml:"target"`
	Object      string `yaml:"object"`
	Mode        string `yaml:"mode"`
}

type nativeAPIAssetDefinition struct {
	Name       string            `yaml:"name"`
	Type       string            `yaml:"type"`
	Connection string            `yaml:"connection"`
	Depends    []string          `yaml:"depends"`
	Meta       map[string]string `yaml:"meta"`
	Parameters map[string]any    `yaml:"parameters"`
}

func isAPIAssetType(assetType string) bool {
	return strings.EqualFold(strings.TrimSpace(assetType), apiAssetType)
}

func isAPIAsset(asset *pipeline.Asset) bool {
	return asset != nil && isAPIAssetType(string(asset.Type))
}

func isAPIExecutablePath(path string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".api.yml") ||
		strings.HasSuffix(strings.ToLower(strings.TrimSpace(path)), ".api.yaml")
}

func apiRunFilePathForDefinition(definitionPath, assetName string) string {
	dir := filepath.Dir(definitionPath)
	base := strings.TrimSuffix(filepath.Base(definitionPath), filepath.Ext(definitionPath))
	base = strings.TrimSuffix(base, ".asset")
	if strings.TrimSpace(base) == "" || base == "." {
		base = assetNameLeafPath(assetName)
	}
	return filepath.Join(dir, base+".api.yml")
}

func apiAwareYamlTaskCreator(fs afero.Fs) pipeline.TaskCreator {
	stock := pipeline.CreateTaskFromYamlDefinition(fs)
	return func(filePath string) (*pipeline.Asset, error) {
		content, err := afero.ReadFile(fs, filePath)
		if err != nil {
			return nil, err
		}
		var definition nativeAPIAssetDefinition
		if err := yaml.Unmarshal(content, &definition); err != nil {
			return nil, err
		}
		if !isAPIAssetType(definition.Type) {
			return stock(filePath)
		}

		absPath, err := filepath.Abs(filePath)
		if err != nil {
			absPath = filePath
		}
		upstreams := make([]pipeline.Upstream, 0, len(definition.Depends))
		for _, dep := range definition.Depends {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			upstreams = append(upstreams, pipeline.Upstream{Value: dep, Type: "asset", Mode: pipeline.UpstreamModeFull})
		}
		asset := &pipeline.Asset{
			Name:       strings.TrimSpace(definition.Name),
			Type:       pipeline.AssetType(apiAssetType),
			Connection: strings.TrimSpace(definition.Connection),
			Upstreams:  upstreams,
			Meta:       definition.Meta,
			ExecutableFile: pipeline.ExecutableFile{
				Name:    filepath.Base(absPath),
				Path:    absPath,
				Content: string(content),
			},
		}
		// The narrow API definition struct intentionally omits the fields renart's
		// asset workbench edits (columns, owner, tags, description, materialization).
		// Bruin's stock reader parses them, but it models `parameters:` as flat
		// map[string]string and errors on an API asset's nested request/response
		// spec — which would silently drop these fields (so a dropped column, an
		// edited owner, etc. never round-trips). Parse them from a copy of the file
		// with `parameters` stripped so they load and survive a save.
		if stripped, stripErr := stripYAMLTopLevelKey(content, "parameters"); stripErr == nil {
			if metaAsset, metaErr := pipeline.ConvertYamlToTask(stripped); metaErr == nil && metaAsset != nil {
				asset.Columns = metaAsset.Columns
				asset.Owner = metaAsset.Owner
				asset.Tags = metaAsset.Tags
				asset.Description = metaAsset.Description
				asset.Materialization = metaAsset.Materialization
			}
		}
		return asset, nil
	}
}

func defaultAPIAssetContent(assetName string) string {
	return `type: api

parameters:
  openapi:
    url: https://petstore3.swagger.io/api/v3/openapi.json

  request:
    url: https://petstore3.swagger.io/api/v3/pet/findByStatus?status=available
    method: GET
    headers:
      Accept: application/json

  response:
    records_path: ""
`
}

func apiSummaryParameters(content string, asset *pipeline.Asset, pl *pipeline.Pipeline) map[string]string {
	spec, connectionName, err := parseNativeAPIAssetSpec(content, asset, pl)
	if err != nil {
		return nil
	}
	params := map[string]string{}
	if strings.TrimSpace(spec.Request.URL) != "" {
		params["url"] = strings.TrimSpace(spec.Request.URL)
	}
	if strings.TrimSpace(connectionName) != "" {
		params["target"] = strings.TrimSpace(connectionName)
	} else if strings.TrimSpace(spec.Load.Target) != "" {
		params["target"] = strings.TrimSpace(spec.Load.Target)
	}
	if asset != nil && strings.TrimSpace(asset.Name) != "" {
		params["object"] = strings.TrimSpace(asset.Name)
	} else if strings.TrimSpace(spec.Load.Object) != "" {
		params["object"] = strings.TrimSpace(spec.Load.Object)
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func apiConnectionNameForAsset(asset *pipeline.Asset, pl *pipeline.Pipeline) (string, error) {
	if asset == nil {
		return "", errors.New("api asset is required")
	}
	content := asset.ExecutableFile.Content
	if strings.TrimSpace(content) == "" && strings.TrimSpace(asset.ExecutableFile.Path) != "" {
		bytes, err := os.ReadFile(asset.ExecutableFile.Path)
		if err != nil {
			return "", err
		}
		content = string(bytes)
	}
	_, connectionName, err := parseNativeAPIAssetSpec(content, asset, pl)
	return connectionName, err
}

func parseNativeAPIAssetSpec(content string, asset *pipeline.Asset, pl *pipeline.Pipeline) (nativeAPISpec, string, error) {
	var raw nativeAPISpec
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return nativeAPISpec{}, "", err
	}
	spec := raw
	if raw.Parameters != nil {
		spec = *raw.Parameters
		if strings.TrimSpace(spec.OpenAPI.URL) == "" && strings.TrimSpace(spec.OpenAPIURL) == "" {
			spec.OpenAPI = raw.OpenAPI
			spec.OpenAPIURL = raw.OpenAPIURL
		}
	}

	connectionName := strings.TrimSpace(raw.Connection)
	if connectionName == "" && asset != nil {
		connectionName = strings.TrimSpace(asset.Connection)
	}
	if connectionName == "" {
		connectionName = strings.TrimSpace(spec.Load.Target)
	}
	if connectionName == "" && pl != nil {
		destination := strings.TrimSpace(spec.Load.Destination)
		if destination != "" {
			if conn := strings.TrimSpace(pl.DefaultConnections[destination]); conn != "" {
				connectionName = conn
			}
		}
		if connectionName == "" {
			// Reuse Bruin's standard asset-connection resolution for the
			// pipeline's majority warehouse type. This honours the pipeline's
			// default_connections and also falls back to the built-in default
			// connection name (e.g. duckdb-default) when none are declared, so
			// an API asset materializes into the same warehouse as the SQL
			// assets without requiring an explicit connection.
			majorityType := pl.GetMajorityAssetTypesFromSQLAssets(pipeline.AssetTypeDuckDBQuery)
			if conn, connErr := pl.GetConnectionNameForAsset(&pipeline.Asset{Type: majorityType}); connErr == nil {
				connectionName = strings.TrimSpace(conn)
			}
		}
		if connectionName == "" && len(pl.DefaultConnections) == 1 {
			for _, conn := range pl.DefaultConnections {
				connectionName = strings.TrimSpace(conn)
			}
		}
	}
	return spec, connectionName, nil
}

func apiTargetObjectName(asset *pipeline.Asset, spec nativeAPISpec) string {
	if asset != nil && strings.TrimSpace(asset.Name) != "" {
		return strings.TrimSpace(asset.Name)
	}
	return strings.TrimSpace(spec.Load.Object)
}

// apiInferredColumnsForDisplay returns an API asset's spec-inferred columns for
// the workspace preview, minus any the user has explicitly dropped
// (renart_col_drop). The preview only falls back to inference when the asset
// carries no declared columns yet, so honouring drops here keeps a dropped
// column from reappearing once the file's column set is emptied out.
func apiInferredColumnsForDisplay(ctx context.Context, asset *pipeline.Asset) []pipeline.Column {
	columns := apiResponseFieldColumns(ctx, asset)
	if len(columns) == 0 || asset == nil {
		return columns
	}
	dropped := assetmeta.Parse(asset.Meta).ColDrop
	if len(dropped) == 0 {
		return columns
	}
	dropSet := make(map[string]bool, len(dropped))
	for _, name := range dropped {
		dropSet[strings.ToLower(strings.TrimSpace(name))] = true
	}
	filtered := make([]pipeline.Column, 0, len(columns))
	for _, column := range columns {
		if dropSet[strings.ToLower(strings.TrimSpace(column.Name))] {
			continue
		}
		filtered = append(filtered, column)
	}
	return filtered
}

func apiResponseFieldColumns(ctx context.Context, asset *pipeline.Asset) []pipeline.Column {
	if asset == nil {
		return nil
	}
	content := asset.ExecutableFile.Content
	if strings.TrimSpace(content) == "" {
		return nil
	}
	spec, _, err := parseNativeAPIAssetSpec(content, asset, nil)
	if err != nil {
		return nil
	}
	if columns, openAPIErr := apiOpenAPIColumns(ctx, spec); openAPIErr == nil && len(columns) > 0 {
		return columns
	}
	if len(spec.Response.Fields) == 0 {
		return nil
	}
	fieldNames := sortedFieldNames(spec.Response.Fields)
	columns := make([]pipeline.Column, 0, len(fieldNames))
	for _, fieldName := range fieldNames {
		columns = append(columns, pipeline.Column{Name: fieldName})
	}
	return columns
}

func (e *HybridBruinExecutor) runAPIAsset(ctx context.Context, pl *pipeline.Pipeline, asset *pipeline.Asset, renderer *jinja.Renderer, manager config.ConnectionGetter, onChunk func([]byte)) ([]byte, error) {
	writer := &streamCaptureWriter{buffer: bytes.NewBuffer(nil), onChunk: onChunk}
	if asset == nil {
		return nil, errors.New("api asset is required")
	}
	if renderer == nil {
		return nil, errors.New("api asset renderer is required")
	}

	specPath := asset.ExecutableFile.Path
	if strings.TrimSpace(specPath) == "" {
		specPath = asset.DefinitionFile.Path
	}
	if strings.TrimSpace(specPath) == "" {
		return nil, errors.New("api asset spec path is required")
	}

	content := asset.ExecutableFile.Content
	if strings.TrimSpace(content) == "" {
		bytes, err := os.ReadFile(specPath)
		if err != nil {
			return nil, err
		}
		content = string(bytes)
	}

	spec, connectionName, err := parseNativeAPIAssetSpec(content, asset, pl)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec.Request.URL) == "" {
		return nil, errors.New("api asset request.url is required")
	}
	if strings.TrimSpace(connectionName) == "" {
		return nil, errors.New("api asset connection is required when it cannot be inferred from pipeline default_connections")
	}
	targetObject := apiTargetObjectName(asset, spec)
	if targetObject == "" {
		return nil, errors.New("api asset target object could not be inferred from asset name")
	}
	targetConn, err := loadConnectionURI(manager, connectionName)
	if err != nil {
		return nil, err
	}

	cmdDir := e.workspaceRoot
	if pipelineRoot, err := findPipelineRootForAsset(specPath); err == nil {
		cmdDir = pipelineRoot
	}
	tmpDir, err := os.MkdirTemp("", "renart-api-asset-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	csvPath := filepath.Join(tmpDir, assetPathSafeName(asset.Name)+".csv")
	count, err := writeAPIAssetCSV(ctx, renderer, spec, csvPath, writer)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	_, _ = fmt.Fprintf(writer, "Fetched %d records from API asset %s.\n", count, asset.Name)

	args := []string{
		"run",
		"--src-stream",
		"file://" + filepath.ToSlash(csvPath),
		"--tgt-conn",
		targetConn,
		"--tgt-object",
		targetObject,
	}
	args = append(args, loadRunModeArgs(ctx)...)
	cmdName, cmdArgs, err := loadCommand(ctx, args, writer)
	if err != nil {
		return writer.buffer.Bytes(), err
	}
	cmd := newStreamingCommand(ctx, cmdName, cmdArgs, cmdDir, writer)
	if err := runStreamingCommand(cmd, writer); err != nil {
		return writer.buffer.Bytes(), err
	}
	return writer.buffer.Bytes(), nil
}

func writeAPIAssetCSV(ctx context.Context, renderer *jinja.Renderer, spec nativeAPISpec, path string, output io.Writer) (int, error) {
	file, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = file.Close() }()
	writer := csv.NewWriter(file)
	defer writer.Flush()

	items := spec.Iterate.Over
	if len(items) == 0 {
		items = []string{""}
	}
	itemName := strings.TrimSpace(spec.Iterate.As)
	if itemName == "" {
		itemName = "item"
	}

	client := &http.Client{Timeout: 30 * time.Second}
	if start, ok := ctx.Value(pipeline.RunConfigStartDate).(time.Time); ok {
		renderer.SetContextValue("start_year", start.Format("2006"))
		renderer.SetContextValue("start_month", start.Format("01"))
	}
	rows := make([]map[string]any, 0)
	openAPIValidator, err := newAPIOpenAPIValidator(ctx, spec)
	if err != nil {
		return 0, err
	}
	fieldNames := sortedFieldNames(spec.Response.Fields)
	for _, item := range items {
		renderer.SetContextValue(itemName, item)
		renderer.SetContextValue("item", item)
		url, err := renderer.Render(spec.Request.URL)
		if err != nil {
			return len(rows), err
		}
		method := strings.TrimSpace(spec.Request.Method)
		if method == "" {
			method = http.MethodGet
		}
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return len(rows), err
		}
		for key, value := range spec.Request.Headers {
			renderedValue, renderErr := renderer.Render(value)
			if renderErr != nil {
				return len(rows), renderErr
			}
			req.Header.Set(key, renderedValue)
		}

		resp, err := client.Do(req)
		if err != nil {
			return len(rows), err
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return len(rows), readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return len(rows), fmt.Errorf("api request to %s failed with status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(body)))
		}
		if output != nil {
			_, _ = fmt.Fprintf(output, "Fetched %s\n", url)
		}

		var decoded any
		if err := json.Unmarshal(body, &decoded); err != nil {
			return len(rows), err
		}
		if openAPIValidator != nil {
			if err := openAPIValidator.Validate(decoded, url); err != nil {
				return len(rows), err
			}
		}
		records := recordsAtPath(decoded, spec.Response.RecordsPath)
		for _, record := range records {
			var out map[string]any
			if len(spec.Response.Fields) > 0 {
				mapped := make(map[string]any, len(spec.Response.Fields))
				for name, fieldPath := range spec.Response.Fields {
					mapped[name] = valueAtPath(record, fieldPath)
				}
				out = mapped
			} else if object, ok := record.(map[string]any); ok {
				out = object
			} else {
				out = map[string]any{"value": record}
			}
			if len(fieldNames) == 0 {
				for key := range out {
					fieldNames = append(fieldNames, key)
				}
				sort.Strings(fieldNames)
			}
			rows = append(rows, out)
		}
	}
	if len(fieldNames) == 0 {
		return 0, nil
	}
	if err := writer.Write(fieldNames); err != nil {
		return 0, err
	}
	for _, row := range rows {
		record := make([]string, 0, len(fieldNames))
		for _, fieldName := range fieldNames {
			record = append(record, csvFieldValue(row[fieldName]))
		}
		if err := writer.Write(record); err != nil {
			return 0, err
		}
	}
	if err := writer.Error(); err != nil {
		return 0, err
	}
	return len(rows), nil
}

func sortedFieldNames(fields map[string]string) []string {
	if len(fields) == 0 {
		return nil
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func csvFieldValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64, bool:
		return fmt.Sprint(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	}
}

func recordsAtPath(input any, path string) []any {
	value := valueAtPath(input, path)
	if value == nil {
		return nil
	}
	if records, ok := value.([]any); ok {
		return records
	}
	return []any{value}
}

func valueAtPath(input any, path string) any {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return input
	}
	current := input
	for _, part := range strings.Split(trimmed, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[part]
	}
	return current
}

func assetPathSafeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "asset"
	}
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, string(filepath.Separator), "_")
	return name
}
