package service

import "github.com/bruin-data/bruin/pkg/pipeline"

// Some Bruin multi-statement materializers predate the shared pipeline
// materializer's per-asset refresh restriction. Keep both run-scoped variants
// and select at Render time so a pipeline can full-refresh unrestricted assets
// while preserving the configured strategy for restricted siblings.
type queryBatchMaterializer interface {
	Render(*pipeline.Asset, string) ([]string, error)
	LogIfFullRefreshAndDDL(interface{}, *pipeline.Asset) error
}

type refreshRestrictedQueryBatchMaterializer struct {
	configured queryBatchMaterializer
	full       queryBatchMaterializer
}

func (m refreshRestrictedQueryBatchMaterializer) selected(asset *pipeline.Asset) queryBatchMaterializer {
	if assetRefreshRestricted(asset) {
		return m.configured
	}
	return m.full
}

func (m refreshRestrictedQueryBatchMaterializer) Render(asset *pipeline.Asset, query string) ([]string, error) {
	return m.selected(asset).Render(asset, query)
}

func (m refreshRestrictedQueryBatchMaterializer) LogIfFullRefreshAndDDL(writer interface{}, asset *pipeline.Asset) error {
	return m.selected(asset).LogIfFullRefreshAndDDL(writer, asset)
}

type athenaBatchMaterializer interface {
	Render(*pipeline.Asset, string, string) ([]string, error)
	LogIfFullRefreshAndDDL(interface{}, *pipeline.Asset) error
}

type refreshRestrictedAthenaMaterializer struct {
	configured athenaBatchMaterializer
	full       athenaBatchMaterializer
}

func (m refreshRestrictedAthenaMaterializer) selected(asset *pipeline.Asset) athenaBatchMaterializer {
	if assetRefreshRestricted(asset) {
		return m.configured
	}
	return m.full
}

func (m refreshRestrictedAthenaMaterializer) Render(asset *pipeline.Asset, query, location string) ([]string, error) {
	return m.selected(asset).Render(asset, query, location)
}

func (m refreshRestrictedAthenaMaterializer) LogIfFullRefreshAndDDL(writer interface{}, asset *pipeline.Asset) error {
	return m.selected(asset).LogIfFullRefreshAndDDL(writer, asset)
}

func assetRefreshRestricted(asset *pipeline.Asset) bool {
	return asset != nil && asset.RefreshRestricted != nil && *asset.RefreshRestricted
}
