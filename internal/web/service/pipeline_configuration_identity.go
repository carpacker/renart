package service

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/pipeline"

	"renart/internal/web/runcontext"
	"renart/internal/web/secretstore"
)

// selectedPipelineConfigurationIdentity projects exactly the selected assets'
// environment controls and connections through the same secret-free identity
// used by asset rendering. Planning and the pre-execution target snapshot both
// call this helper, so confirmation compares like with like.
func selectedPipelineConfigurationIdentity(
	workspaceRoot string,
	cfg *config.Config,
	pl *pipeline.Pipeline,
	assets []*pipeline.Asset,
) runcontext.Identity {
	if cfg == nil {
		cfg = &config.Config{}
	}
	connectionNames := make([]string, 0, len(assets))
	for _, asset := range assets {
		if asset == nil {
			return runcontext.Identity{
				Fidelity: runcontext.IdentityFidelityRuntimeOnly,
				Message:  "selected asset is nil",
			}
		}
		info := &directPipelineInfo{Pipeline: pl, Asset: asset, Config: cfg}
		primary, err := assetRenderConnectionName(info)
		if err != nil && !assetRenderAssetIsConnectionless(info) {
			return runcontext.Identity{
				Fidelity: runcontext.IdentityFidelityRuntimeOnly,
				Message:  fmt.Sprintf("asset %q connection configuration could not be resolved", asset.Name),
			}
		}
		connectionNames = append(connectionNames, assetRenderConfigurationConnectionNames(info, primary)...)
	}
	return selectedConfigurationIdentityWithBindings(workspaceRoot, cfg, connectionNames)
}

type configurationBindingIdentity struct {
	Connection string `mapstructure:"connection"`
	Field      string `mapstructure:"field"`
	Symbol     string `mapstructure:"symbol"`
	Reference  string `mapstructure:"reference"`
}

type configurationWithBindingsIdentity struct {
	BaseDigest string                         `mapstructure:"base_digest"`
	Bindings   []configurationBindingIdentity `mapstructure:"bindings"`
}

func selectedConfigurationIdentityWithBindings(
	workspaceRoot string,
	cfg *config.Config,
	connectionNames []string,
) runcontext.Identity {
	if cfg == nil {
		cfg = &config.Config{}
	}
	base := runcontext.SelectedConfigurationIdentity(
		strings.TrimSpace(cfg.SelectedEnvironmentName),
		cfg.SelectedEnvironment,
		connectionNames,
	)
	if base.Fidelity != runcontext.IdentityFidelityExact || base.Digest == "" {
		return base
	}
	if cfg.SelectedEnvironment == nil || cfg.SelectedEnvironment.Connections == nil {
		return base
	}

	manifest := secretstore.NewManifest()
	if strings.TrimSpace(workspaceRoot) != "" {
		loaded, err := secretstore.LoadManifest(filepath.Join(workspaceRoot, ".renart", "secrets.yml"))
		if err != nil {
			return runcontext.Identity{
				Fidelity: runcontext.IdentityFidelityRuntimeOnly,
				Message:  "tracked secret bindings could not be parsed",
			}
		}
		manifest = loaded
	}

	bindings := make([]configurationBindingIdentity, 0)
	for _, connectionName := range uniqueSortedConfigurationNames(connectionNames) {
		connectionType := cfg.SelectedEnvironment.Connections.ConnectionsSummaryList()[connectionName]
		connection := cfg.SelectedEnvironment.Connections.GetConnection(connectionName)
		if connection == nil || connectionType == "" {
			return runcontext.Identity{
				Fidelity: runcontext.IdentityFidelityRuntimeOnly,
				Message:  fmt.Sprintf("connection %q is not present in the selected environment", connectionName),
			}
		}
		rawValues := buildWorkspaceConfigConnectionRawValues(connection, connectionType)
		for _, field := range workspaceConnectionFieldDefsForType(connectionType) {
			if !field.IsSensitive && !field.IsSensitiveFile {
				continue
			}
			rawValue, _ := rawValues[field.Name].(string)
			symbolMatch := exactSecretSymbolPattern.FindStringSubmatch(rawValue)
			binding, found := manifest.Binding(
				cfg.SelectedEnvironmentName,
				connectionName,
				field.Name,
			)
			if !found && len(symbolMatch) == 2 {
				binding = secretstore.Binding{
					Symbol:    symbolMatch[1],
					Reference: secretstore.Ref{Provider: "env", Key: symbolMatch[1]},
				}
				found = true
			}
			if !found {
				continue
			}
			if len(symbolMatch) != 2 || binding.Symbol != symbolMatch[1] {
				return runcontext.Identity{
					Fidelity: runcontext.IdentityFidelityRuntimeOnly,
					Message:  fmt.Sprintf("connection %q secret binding does not match its placeholder", connectionName),
				}
			}
			bindings = append(bindings, configurationBindingIdentity{
				Connection: connectionName,
				Field:      field.Name,
				Symbol:     binding.Symbol,
				Reference:  binding.Reference.String(),
			})
		}
	}
	sort.Slice(bindings, func(i, j int) bool {
		if bindings[i].Connection != bindings[j].Connection {
			return bindings[i].Connection < bindings[j].Connection
		}
		return bindings[i].Field < bindings[j].Field
	})
	return runcontext.SecretFreeCanonicalIdentity(
		"renart-selected-configuration-bindings-v1",
		configurationWithBindingsIdentity{
			BaseDigest: base.Digest,
			Bindings:   bindings,
		},
	)
}

func uniqueSortedConfigurationNames(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
