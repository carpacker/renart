package service

import "github.com/bruin-data/bruin/pkg/pipeline"

// assetTypeTrinoSeed is a Renart-owned extension. Bruin does not currently
// publish a trino.seed asset type, but Renart's seed runtime is warehouse-
// generic: it loads the declared file through Sling into the resolved target
// connection. Keep the type local until Bruin exposes an equivalent constant.
const assetTypeTrinoSeed pipeline.AssetType = "trino.seed"

func init() {
	// Register the extension with Bruin's exported connection lookup so an
	// omitted asset-level connection still resolves through the pipeline's
	// `default_connections.trino` entry, just like native Bruin asset types.
	pipeline.AssetTypeConnectionMapping[assetTypeTrinoSeed] = "trino"
}
