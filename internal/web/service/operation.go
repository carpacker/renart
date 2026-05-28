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

func scopedRunOperation(target, pipelineID, assetPath, environment, runScope string, assetPaths []string) webmodel.OperationMetadata {
	operation := runOperation(target, pipelineID, assetPath, environment)
	operation.RunScope = runScope
	operation.AssetPaths = append([]string(nil), assetPaths...)
	return operation
}

func withOperationTimeWindow(operation webmodel.OperationMetadata, timeWindow ExecutionTimeWindow) webmodel.OperationMetadata {
	operation.StartDate = timeWindow.StartRFC3339()
	operation.EndDate = timeWindow.EndRFC3339()
	return operation
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
