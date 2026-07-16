package service

import (
	"context"
	"errors"
	"fmt"
	iofs "io/fs"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/connection"
	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
)

func loadConfigOrEmpty(configPath string) *config.Config {
	cfg, err := loadConfig(afero.NewOsFs(), configPath)
	if err != nil || cfg == nil {
		return &config.Config{}
	}
	return cfg
}

func loadSelectedConfig(configPath string, requestedEnvironment string) (*config.Config, error) {
	return loadSelectedConfigFS(afero.NewOsFs(), configPath, requestedEnvironment)
}

func loadSelectedConfigFS(fs afero.Fs, configPath string, requestedEnvironment string) (*config.Config, error) {
	cfg, err := loadConfig(fs, configPath)
	if err != nil {
		return nil, err
	}
	return selectConfigEnvironment(cfg, requestedEnvironment)
}

// loadSelectedConfigReadOnlyFS parses an existing Bruin configuration without
// invoking LoadOrCreate. Render/plan paths must not create .bruin.yml or edit
// .gitignore merely because a user asks to preview an asset.
func loadSelectedConfigReadOnlyFS(fs afero.Fs, configPath string, requestedEnvironment string) (*config.Config, error) {
	if strings.TrimSpace(configPath) == "" {
		return selectConfigEnvironment(&config.Config{}, requestedEnvironment)
	}
	if fs == nil {
		fs = afero.NewOsFs()
	}
	cfg, err := config.LoadFromFileOrEnv(fs, configPath)
	if errors.Is(err, iofs.ErrNotExist) {
		cfg = &config.Config{}
	} else if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	return selectConfigEnvironment(cfg, requestedEnvironment)
}

func loadConfig(fs afero.Fs, configPath string) (*config.Config, error) {
	if strings.TrimSpace(configPath) == "" {
		return &config.Config{}, nil
	}
	if fs == nil {
		fs = afero.NewOsFs()
	}
	cfg, err := config.LoadOrCreate(fs, configPath)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	return cfg, nil
}

func selectConfigEnvironment(cfg *config.Config, requestedEnvironment string) (*config.Config, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}

	if strings.TrimSpace(cfg.SelectedEnvironmentName) == "" {
		cfg.SelectedEnvironmentName = cfg.DefaultEnvironmentName
	}

	environmentName := strings.TrimSpace(requestedEnvironment)
	if environmentName == "" {
		environmentName = strings.TrimSpace(cfg.SelectedEnvironmentName)
	}
	if environmentName == "" {
		environmentName = strings.TrimSpace(cfg.DefaultEnvironmentName)
	}

	if environmentName == "" {
		return cfg, nil
	}

	if err := cfg.SelectEnvironment(environmentName); err != nil {
		return nil, err
	}

	return cfg, nil
}

func selectedEnvironmentRestrictsFullRefresh(cfg *config.Config) bool {
	return cfg != nil && cfg.SelectedEnvironment != nil && cfg.SelectedEnvironment.Config != nil && cfg.SelectedEnvironment.Config.RefreshRestricted
}

func applySelectedEnvironmentRefreshRestriction(cfg *config.Config, assets []*pipeline.Asset) {
	if !selectedEnvironmentRestrictsFullRefresh(cfg) {
		return
	}
	restricted := true
	for _, asset := range assets {
		if asset != nil {
			asset.RefreshRestricted = &restricted
		}
	}
}

func newConnectionManagerFromConfig(ctx context.Context, cfg *config.Config) (config.ConnectionAndDetailsGetter, error) {
	manager, errs := connection.NewManagerFromConfigWithContext(ctx, cfg)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	return manager, nil
}

func selectConfigAndCreateConnectionManager(ctx context.Context, configPath string, requestedEnvironment string) (*config.Config, config.ConnectionAndDetailsGetter, error) {
	cfg, err := loadSelectedConfig(configPath, requestedEnvironment)
	if err != nil {
		return nil, nil, err
	}

	manager, err := newConnectionManagerFromConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}

	return cfg, manager, nil
}

func requireEnvironmentName(cfg *config.Config, requestedEnvironment string) (string, error) {
	selected, err := selectConfigEnvironment(cfg, requestedEnvironment)
	if err != nil {
		return "", err
	}
	if selected == nil || strings.TrimSpace(selected.SelectedEnvironmentName) == "" {
		return "", fmt.Errorf("no environment selected")
	}
	return selected.SelectedEnvironmentName, nil
}
