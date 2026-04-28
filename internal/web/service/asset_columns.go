package service

import (
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

func BuildInferAssetColumnsQuery(parsedPipeline *pipeline.Pipeline, asset *pipeline.Asset, environment string) (QueryConnectionRequest, error) {
	if parsedPipeline == nil || asset == nil {
		return QueryConnectionRequest{}, fmt.Errorf("asset context is required")
	}

	connectionName, err := parsedPipeline.GetConnectionNameForAsset(asset)
	if err != nil {
		return QueryConnectionRequest{}, fmt.Errorf("failed to resolve asset connection: %w", err)
	}

	targetTableName := strings.TrimSpace(asset.Name)
	if targetTableName == "" {
		return QueryConnectionRequest{}, fmt.Errorf("asset name is required to infer columns")
	}

	query := fmt.Sprintf("select * from %s limit 1", QuoteQualifiedIdentifier(targetTableName))
	return QueryConnectionRequest{
		ConnectionName: connectionName,
		Query:          query,
		Environment:    environment,
		Output:         "json",
	}, nil
}
