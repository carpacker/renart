package service

import webmodel "renart/internal/web/model"

type OperationMetadata = webmodel.OperationMetadata

func runOperation(target, pipelineID, assetPath, environment string) webmodel.OperationMetadata {
	return webmodel.OperationMetadata{
		Type:        "run",
		Target:      target,
		PipelineID:  pipelineID,
		AssetPath:   assetPath,
		Environment: environment,
	}
}

func queryAssetOperation(assetPath, limit, environment, configFile string) webmodel.OperationMetadata {
	return webmodel.OperationMetadata{
		Type:        "query_asset",
		AssetPath:   assetPath,
		Target:      assetPath,
		Limit:       limit,
		Environment: environment,
		ConfigFile:  configFile,
	}
}

func queryConnectionOperation(connectionName, query, environment string) webmodel.OperationMetadata {
	return webmodel.OperationMetadata{
		Type:           "query_connection",
		ConnectionName: connectionName,
		Query:          query,
		Environment:    environment,
	}
}

func patchOperation(operation, targetPath string) webmodel.OperationMetadata {
	return webmodel.OperationMetadata{
		Type:       "patch",
		Operation:  operation,
		Target:     targetPath,
		TargetPath: targetPath,
	}
}

func importDatabaseOperation(pipelinePath, connectionName, environment string) webmodel.OperationMetadata {
	return webmodel.OperationMetadata{
		Type:           "import_database",
		Target:         pipelinePath,
		TargetPath:     pipelinePath,
		ConnectionName: connectionName,
		Environment:    environment,
	}
}
