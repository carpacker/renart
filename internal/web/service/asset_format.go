package service

import (
	"context"
	"strings"

	"github.com/spf13/afero"
)

func (s *AssetService) FormatSQL(ctx context.Context, assetID string, req FormatSQLAssetRequest) (FormatSQLAssetResponse, *ServiceAPIError) {
	relAssetPath, err := DecodeID(assetID)
	if err != nil {
		return FormatSQLAssetResponse{}, newServiceAPIError(400, "invalid_asset_id", "invalid asset id")
	}
	if !strings.HasSuffix(strings.ToLower(relAssetPath), ".sql") {
		return FormatSQLAssetResponse{}, newServiceAPIError(400, "invalid_asset_type", "only SQL assets can be formatted")
	}
	absAssetPath, err := s.resolver().JoinPath(relAssetPath)
	if err != nil {
		return FormatSQLAssetResponse{}, newServiceAPIError(400, "invalid_asset_path", err.Error())
	}
	fs := s.fs()
	originalBytes, err := afero.ReadFile(fs, absAssetPath)
	if err != nil {
		return FormatSQLAssetResponse{}, newServiceAPIError(500, "asset_read_failed", err.Error())
	}
	mergedContent := MergeExecutableContent(string(originalBytes), req.Content)
	if err := afero.WriteFile(fs, absAssetPath, []byte(mergedContent), 0o644); err != nil {
		return FormatSQLAssetResponse{}, newServiceAPIError(500, "asset_write_failed", err.Error())
	}
	output, err := s.deps.Executor.FormatAsset(ctx, FormatAssetRequest{AssetPath: relAssetPath, UseSQLFluff: true})
	if err != nil {
		return FormatSQLAssetResponse{Status: "error", AssetID: assetID, Content: req.Content, Error: strings.TrimSpace(string(output))}, nil
	}
	formattedBytes, err := afero.ReadFile(fs, absAssetPath)
	if err != nil {
		return FormatSQLAssetResponse{}, newServiceAPIError(500, "asset_read_failed", err.Error())
	}
	s.deps.SuppressWatcher(relAssetPath)
	s.deps.PushWorkspaceUpdateImmediateWithChangedIDs(ctx, "asset.updated", relAssetPath, []string{assetID})
	return FormatSQLAssetResponse{Status: "ok", AssetID: assetID, Content: ExtractExecutableContent(string(formattedBytes))}, nil
}
