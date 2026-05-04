package service

import (
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

func sqlAssetTypeForIngestrDestination(destination string) (string, bool) {
	assetType, ok := pipeline.IngestrTypeConnectionMapping[normalizeConnectionType(destination)]
	if !ok {
		return "", false
	}

	return string(assetType), true
}

func sqlAssetTypeForConnectionName(parsedPipeline *pipeline.Pipeline, connectionName string) (string, bool) {
	if parsedPipeline == nil || strings.TrimSpace(connectionName) == "" {
		return "", false
	}

	for connectionType, configuredName := range parsedPipeline.DefaultConnections {
		if strings.EqualFold(strings.TrimSpace(configuredName), strings.TrimSpace(connectionName)) {
			return sqlAssetTypeForConnectionType(connectionType)
		}
	}

	return "", false
}

func sqlAssetTypeForConnectionType(connectionType string) (string, bool) {
	assetType, ok := queryAssetTypeForConnectionType(connectionType)
	if !ok {
		return "", false
	}
	return string(assetType), true
}

func queryAssetTypeForConnectionType(connectionType string) (pipeline.AssetType, bool) {
	canonical := normalizeConnectionType(connectionType)
	if canonical == "" {
		return "", false
	}

	for assetType, mappedConnectionType := range pipeline.AssetTypeConnectionMapping {
		if normalizeConnectionType(mappedConnectionType) == canonical && isQueryAssetType(assetType) {
			return assetType, true
		}
	}

	return "", false
}

func sourceAssetTypeForConnectionType(connectionType string) (pipeline.AssetType, bool) {
	canonical := normalizeConnectionType(connectionType)
	if canonical == "" {
		return "", false
	}

	for assetType, mappedConnectionType := range pipeline.AssetTypeConnectionMapping {
		if normalizeConnectionType(mappedConnectionType) == canonical && isSourceAssetType(assetType) {
			return assetType, true
		}
	}

	return "", false
}

func convertDirectSourceTypeToQueryType(sourceType pipeline.AssetType) pipeline.AssetType {
	connectionType, ok := pipeline.AssetTypeConnectionMapping[sourceType]
	if !ok {
		return sourceType
	}
	queryType, ok := queryAssetTypeForConnectionType(connectionType)
	if !ok {
		return sourceType
	}
	return queryType
}

func isQueryAssetType(assetType pipeline.AssetType) bool {
	return strings.HasSuffix(string(assetType), ".sql")
}

func isSourceAssetType(assetType pipeline.AssetType) bool {
	return strings.HasSuffix(string(assetType), ".source")
}

func normalizeConnectionType(connectionType string) string {
	switch strings.ToLower(strings.TrimSpace(connectionType)) {
	case "bigquery", "gcp":
		return "google_cloud_platform"
	case "sqlserver", "ms":
		return "mssql"
	case "pg":
		return "postgres"
	case "rs":
		return "redshift"
	case "sf":
		return "snowflake"
	case "bq":
		return "google_cloud_platform"
	default:
		return strings.ToLower(strings.TrimSpace(connectionType))
	}
}
