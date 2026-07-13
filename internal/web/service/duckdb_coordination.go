package service

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"

	"renart/internal/web/duckcoord"
)

func (e *HybridBruinExecutor) acquireDuckDBConnections(
	ctx context.Context,
	manager config.ConnectionGetter,
	connectionNames []string,
	owner duckcoord.Owner,
	output io.Writer,
) (*duckcoord.Lease, error) {
	if e == nil || e.duckDBCoordinator == nil || manager == nil {
		return &duckcoord.Lease{}, nil
	}
	details, ok := manager.(config.ConnectionDetailsGetter)
	if !ok {
		return &duckcoord.Lease{}, nil
	}

	paths := make([]string, 0, len(connectionNames))
	for _, name := range connectionNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		rawPath := ""
		switch connection := details.GetConnectionDetails(name).(type) {
		case *config.DuckDBConnection:
			if connection != nil {
				rawPath = connection.Path
			}
		case config.DuckDBConnection:
			rawPath = connection.Path
		default:
			continue
		}
		path, err := duckcoord.CanonicalPath(e.workspaceRoot, rawPath)
		if err != nil {
			return nil, err
		}
		if path != "" {
			paths = append(paths, path)
		}
	}

	if owner.OnWait == nil && output != nil {
		owner.OnWait = func(databasePath string) {
			_, _ = fmt.Fprintf(output, "Waiting for DuckDB database %s to become available...\n", filepath.Base(databasePath))
		}
	}
	return e.duckDBCoordinator.Acquire(ctx, paths, owner)
}

func duckDBConnectionNamesForAsset(pl *pipeline.Pipeline, asset *pipeline.Asset) []string {
	if asset == nil {
		return nil
	}
	// Python execution can spend most of its time computing without touching
	// the warehouse. Its operator acquires the destination only for the final
	// Parquet load, keeping unrelated work concurrent and avoiding a nested
	// lease.
	if asset.Type == pipeline.AssetTypePython {
		return nil
	}
	if isAPIAsset(asset) {
		name, err := apiConnectionNameForAsset(asset, pl)
		if err == nil && strings.TrimSpace(name) != "" {
			return []string{name}
		}
		return nil
	}
	if isLoadAsset(asset) {
		params := loadParamsFromAsset(asset)
		return []string{params.SourceConnection, params.DestinationConnection}
	}

	names := make([]string, 0, 2)
	if asset.Type == pipeline.AssetTypeIngestr {
		if source, ok := asset.Parameters.GetString("source_connection"); ok {
			names = append(names, strings.TrimSpace(source))
		}
	}
	if pl != nil {
		if destination, err := pl.GetConnectionNameForAsset(asset); err == nil {
			names = append(names, destination)
		}
	}
	return names
}

func directTaskLeaseOwner(ctx context.Context, pl *pipeline.Pipeline, asset *pipeline.Asset) duckcoord.Owner {
	owner := duckcoord.Owner{Operation: "run asset"}
	if pl != nil {
		owner.Pipeline = pl.Name
	}
	if asset != nil {
		owner.Asset = asset.Name
	}
	if runID, ok := ctx.Value(pipeline.RunConfigRunID).(string); ok {
		owner.RunID = runID
	}
	return owner
}
