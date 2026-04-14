package service

import (
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

func BuildInferAssetColumnsCommand(parsedPipeline *pipeline.Pipeline, asset *pipeline.Asset) ([]string, error) {
	if parsedPipeline == nil || asset == nil {
		return nil, fmt.Errorf("asset context is required")
	}

	connectionName, err := parsedPipeline.GetConnectionNameForAsset(asset)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve asset connection: %w", err)
	}

	targetTableName := strings.TrimSpace(asset.Name)
	if targetTableName == "" {
		return nil, fmt.Errorf("asset name is required to infer columns")
	}

	query := fmt.Sprintf("select * from %s limit 1", QuoteQualifiedIdentifier(targetTableName))
	return BuildRemoteTableColumnsCommand(connectionName, query, ""), nil
}

func BuildRemoteTableColumnsCommand(connectionName, query, environment string) []string {
	args := []string{
		"query",
		"--connection",
		connectionName,
		"--query",
		query,
		"--output",
		"json",
	}

	if strings.TrimSpace(environment) != "" {
		args = append(args, "--environment", environment)
	}

	return args
}
