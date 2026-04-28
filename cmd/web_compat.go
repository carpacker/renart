package cmd

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"renart/internal/web/service"
)

func replaceAssetNameReferences(content, oldName, newName string) string {
	return service.ReplaceAssetNameReferences(content, oldName, newName)
}

func buildSchemaTableSuggestionItems(tables map[string][]string, prefix string) []ingestrSuggestionItem {
	items := service.BuildSchemaTableSuggestionItems(tables, prefix)
	result := make([]ingestrSuggestionItem, 0, len(items))
	for _, item := range items {
		result = append(result, ingestrSuggestionItem(item))
	}
	return result
}

func buildSQLDiscoveryTableItems(databaseName string, tables map[string][]string) []sqlDiscoveryTableItem {
	items := service.BuildSQLDiscoveryTableItems(databaseName, tables)
	result := make([]sqlDiscoveryTableItem, 0, len(items))
	for _, item := range items {
		result = append(result, sqlDiscoveryTableItem(item))
	}
	return result
}

func buildSQLDiscoveryTableItemsWithoutSchemas(databaseName string, tables []string) []sqlDiscoveryTableItem {
	items := service.BuildSQLDiscoveryTableItemsWithoutSchemas(databaseName, tables)
	result := make([]sqlDiscoveryTableItem, 0, len(items))
	for _, item := range items {
		result = append(result, sqlDiscoveryTableItem(item))
	}
	return result
}

func buildDuckDBSuggestionItems(
	ctx context.Context,
	fetcher interface {
		GetDatabases(ctx context.Context) ([]string, error)
		GetTables(ctx context.Context, databaseName string) ([]string, error)
	},
	prefix string,
) ([]ingestrSuggestionItem, error) {
	items, err := service.BuildDuckDBSuggestionItems(ctx, fetcher, prefix)
	if err != nil {
		return nil, err
	}
	result := make([]ingestrSuggestionItem, 0, len(items))
	for _, item := range items {
		result = append(result, ingestrSuggestionItem(item))
	}
	return result, nil
}

func buildS3SuggestionItems(
	ctx context.Context,
	conn interface {
		ListBuckets(ctx context.Context) ([]string, error)
		ListEntries(ctx context.Context, bucketName, prefix string) ([]string, error)
	},
	prefix string,
	connectionDetails any,
) ([]ingestrSuggestionItem, error) {
	items, err := service.BuildS3SuggestionItems(ctx, conn, prefix, connectionDetails)
	if err != nil {
		return nil, err
	}
	result := make([]ingestrSuggestionItem, 0, len(items))
	for _, item := range items {
		result = append(result, ingestrSuggestionItem(item))
	}
	return result, nil
}

func s3SuggestionContext(connectionDetails any) (string, string) {
	return service.S3SuggestionContext(connectionDetails)
}

func buildS3EntrySuggestionItems(items []string) []ingestrSuggestionItem {
	suggestions := service.BuildS3EntrySuggestionItems(items)
	result := make([]ingestrSuggestionItem, 0, len(suggestions))
	for _, item := range suggestions {
		result = append(result, ingestrSuggestionItem(item))
	}
	return result
}

func buildWorkspacePathSuggestionItems(workspaceRoot string, prefix string) ([]ingestrSuggestionItem, error) {
	items, err := service.BuildWorkspacePathSuggestionItems(workspaceRoot, prefix)
	if err != nil {
		return nil, err
	}
	result := make([]ingestrSuggestionItem, 0, len(items))
	for _, item := range items {
		result = append(result, ingestrSuggestionItem(item))
	}
	return result, nil
}

func buildAbsolutePathSuggestionItems(prefix string) ([]ingestrSuggestionItem, error) {
	items, err := service.BuildAbsolutePathSuggestionItems(prefix)
	if err != nil {
		return nil, err
	}
	result := make([]ingestrSuggestionItem, 0, len(items))
	for _, item := range items {
		result = append(result, ingestrSuggestionItem(item))
	}
	return result, nil
}

func buildS3EntrySuggestionItemsWithBucket(bucketName string, items []string) []ingestrSuggestionItem {
	suggestions := service.BuildS3EntrySuggestionItemsWithBucket(bucketName, items)
	result := make([]ingestrSuggestionItem, 0, len(suggestions))
	for _, item := range suggestions {
		result = append(result, ingestrSuggestionItem(item))
	}
	return result
}

func limitSuggestionItems(items []ingestrSuggestionItem, max int) []ingestrSuggestionItem {
	converted := make([]service.SuggestionItem, 0, len(items))
	for _, item := range items {
		converted = append(converted, service.SuggestionItem(item))
	}
	limited := service.LimitSuggestionItems(converted, max)
	result := make([]ingestrSuggestionItem, 0, len(limited))
	for _, item := range limited {
		result = append(result, ingestrSuggestionItem(item))
	}
	return result
}

// assetEntry is retained as a compatibility test helper for graph utility tests.
type assetEntry struct {
	id        string
	name      string
	path      string
	upstreams []string
}

func buildDownstreamIndex(assets []assetEntry, nameToID map[string]string) map[string][]string {
	downstream := make(map[string][]string)
	for _, a := range assets {
		for _, upName := range a.upstreams {
			if upID, ok := nameToID[upName]; ok {
				downstream[upID] = append(downstream[upID], a.id)
			}
		}
	}
	return downstream
}

func pathContains(eventPath, assetPath string) bool {
	return service.PathContains(eventPath, assetPath)
}

func databaseNameForConnectionDetails(details any) string {
	return service.DatabaseNameForConnectionDetails(details)
}

func quoteQualifiedIdentifier(value string) string {
	return service.QuoteQualifiedIdentifier(value)
}

func readStringField(row map[string]any, keys ...string) string {
	return service.ReadStringField(row, keys...)
}

func assetTypeToDialect(assetType pipeline.AssetType) (string, error) {
	return service.AssetTypeToDialect(assetType)
}

func resolvePipelineRunTarget(pipelineID string) (string, error) {
	relPath, err := decodeID(pipelineID)
	if err != nil {
		return "", err
	}

	cleaned := filepath.Clean(relPath)
	base := strings.ToLower(filepath.Base(cleaned))
	if base == "pipeline.yml" || base == "pipeline.yaml" || base == ".pipeline.yml" || base == ".pipeline.yaml" {
		dir := filepath.Dir(cleaned)
		if dir == "." {
			return ".", nil
		}
		return filepath.ToSlash(dir), nil
	}

	return filepath.ToSlash(cleaned), nil
}

func stripAssetContent(state workspaceState) workspaceState {
	return workspaceStateFromCoord(service.StripAssetContent(workspaceCoordStateFromWeb(state)))
}

func stripAssetContentKeepingIDs(state workspaceState, keepIDs []string) workspaceState {
	return workspaceStateFromCoord(service.StripAssetContentKeepingIDs(workspaceCoordStateFromWeb(state), keepIDs))
}
