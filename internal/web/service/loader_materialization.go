package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
)

// validateLoaderMaterialization keeps file-editing APIs aligned with the
// execution contract for Load and HTTP API assets. Raw YAML saves can still be
// temporarily incomplete while the user types; execution repeats this check.
func validateLoaderMaterialization(asset *pipeline.Asset) error {
	if asset == nil || (!isAPIAsset(asset) && !isLoadAsset(asset)) {
		return nil
	}
	materializationType := strings.ToLower(strings.TrimSpace(string(asset.Materialization.Type)))
	if materializationType != "" && materializationType != "table" {
		return fmt.Errorf("materialization type %q is not supported for %s assets", materializationType, asset.Type)
	}
	_, err := slingMaterializationArgs(context.Background(), asset)
	return err
}

func loaderMaterializationAPIError(asset *pipeline.Asset) *APIError {
	if err := validateLoaderMaterialization(asset); err != nil {
		return badRequestError("unsupported_materialization", err.Error())
	}
	return nil
}
