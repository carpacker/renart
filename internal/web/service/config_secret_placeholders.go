package service

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"reflect"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

type rawConnectionConfig struct {
	Environments map[string]rawConnectionEnvironment `yaml:"environments"`
}

type rawConnectionEnvironment struct {
	Connections map[string][]map[string]any `yaml:"connections"`
}

// restoreManagedSecretPlaceholders undoes Bruin's eager ${NAME} expansion for
// tagged secret fields. Renart must see the symbolic value so its provider
// binding, rather than an ambient process variable with the same name, remains
// authoritative. Non-secret interpolation continues to use Bruin semantics.
func restoreManagedSecretPlaceholders(
	fs afero.Fs,
	configPath string,
	cfg *config.Config,
	allowEnvironmentConfig bool,
) error {
	if cfg == nil {
		return nil
	}
	var contents []byte
	if allowEnvironmentConfig {
		if environmentConfig := os.Getenv("BRUIN_CONFIG_FILE_CONTENT"); environmentConfig != "" {
			contents = []byte(environmentConfig)
		}
	}
	if contents == nil {
		var err error
		contents, err = afero.ReadFile(fs, configPath)
		if errors.Is(err, iofs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read raw connection configuration: %w", err)
		}
	}

	var raw rawConnectionConfig
	if err := yaml.Unmarshal(contents, &raw); err != nil {
		return fmt.Errorf("parse raw connection configuration: %w", err)
	}
	for environmentName, rawEnvironment := range raw.Environments {
		environment, found := cfg.Environments[environmentName]
		if !found || environment.Connections == nil {
			continue
		}
		restoreEnvironmentSecretPlaceholders(environment.Connections, rawEnvironment.Connections)
	}
	return nil
}

func restoreEnvironmentSecretPlaceholders(
	connections *config.Connections,
	rawConnections map[string][]map[string]any,
) {
	value := reflect.ValueOf(connections)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return
	}
	valueType := value.Type()
	for typeIndex := 0; typeIndex < value.NumField(); typeIndex++ {
		items := value.Field(typeIndex)
		if items.Kind() != reflect.Slice {
			continue
		}
		typeName := strings.Split(valueType.Field(typeIndex).Tag.Get("yaml"), ",")[0]
		rawItems := rawConnections[typeName]
		for itemIndex := 0; itemIndex < items.Len() && itemIndex < len(rawItems); itemIndex++ {
			item := items.Index(itemIndex)
			for _, fieldDef := range workspaceConnectionFieldDefsForType(typeName) {
				if !fieldDef.IsSensitive && !fieldDef.IsSensitiveFile {
					continue
				}
				rawValue, ok := rawItems[itemIndex][fieldDef.Name].(string)
				if !ok || !exactSecretSymbolPattern.MatchString(rawValue) {
					continue
				}
				field, found := workspaceConnectionFieldValue(item, fieldDef.Name)
				if !found || field.Kind() != reflect.String || !field.CanSet() {
					continue
				}
				field.SetString(rawValue)
			}
		}
	}
}
