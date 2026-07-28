package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/bruin-data/bruin/pkg/mask"
	"renart/internal/web/secretstore"
)

var exactSecretSymbolPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// ResolvedConnectionFactory is the only production path that should turn a
// persisted connection configuration into live connection objects. It overlays
// provider-backed values on a freshly loaded config and never changes process
// environment variables or persists the resolved config.
type ResolvedConnectionFactory struct {
	workspaceRoot string
	configPath    string
	projectID     string
	resolver      *secretstore.Resolver
}

func NewResolvedConnectionFactory(
	workspaceRoot string,
	configPath string,
	projectID string,
	resolver *secretstore.Resolver,
) *ResolvedConnectionFactory {
	if resolver == nil {
		resolver = secretstore.NewDefaultResolver()
	}
	return &ResolvedConnectionFactory{
		workspaceRoot: workspaceRoot,
		configPath:    configPath,
		projectID:     projectID,
		resolver:      resolver,
	}
}

func (f *ResolvedConnectionFactory) NewConnectionManager(
	ctx context.Context,
	environment string,
) (config.ConnectionAndDetailsGetter, error) {
	cfg, err := loadSelectedConfig(f.configPath, environment)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	resolved, err := f.ResolveConfig(
		ctx,
		cfg,
		environment,
		secretstore.PurposeFromContext(ctx, secretstore.PurposeQuery),
	)
	if err != nil {
		return nil, err
	}
	defer resolved.Close(ctx)

	manager, err := newConnectionManagerFromConfig(ctx, resolved.Config)
	if err != nil {
		return nil, errors.New(resolved.Redactor.Mask(err.Error()))
	}
	return WrapConnectionManagerForWorkspace(manager, f.workspaceRoot), nil
}

type ResolvedConnectionConfig struct {
	Config   *config.Config
	Redactor *mask.Masker
	bundle   *secretstore.Bundle
}

func (r *ResolvedConnectionConfig) Close(ctx context.Context) error {
	if r == nil || r.bundle == nil {
		return nil
	}
	return r.bundle.Close(ctx)
}

func (f *ResolvedConnectionFactory) ResolveConfig(
	ctx context.Context,
	cfg *config.Config,
	environment string,
	purpose secretstore.Purpose,
) (*ResolvedConnectionConfig, error) {
	selected, err := selectConfigEnvironment(cfg, environment)
	if err != nil {
		return nil, err
	}
	if selected.SelectedEnvironment == nil || selected.SelectedEnvironment.Connections == nil {
		return &ResolvedConnectionConfig{
			Config:   selected,
			Redactor: mask.New(nil),
		}, nil
	}

	manifest, err := secretstore.LoadManifest(filepath.Join(f.workspaceRoot, ".renart", "secrets.yml"))
	if err != nil {
		return nil, err
	}
	requests, targets, err := f.connectionSecretRequests(selected, manifest, purpose)
	if err != nil {
		return nil, err
	}
	bundle, err := f.resolver.ResolveAll(ctx, requests)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		target.field.SetString(string(bundle.Value(target.name)))
	}

	redactionValues := workspaceConnectionSensitiveValues(selected)
	redactionValues = append(redactionValues, bundle.RedactionValues()...)
	return &ResolvedConnectionConfig{
		Config:   selected,
		Redactor: mask.New(redactionValues),
		bundle:   bundle,
	}, nil
}

type connectionSecretTarget struct {
	name  string
	field reflect.Value
}

func (f *ResolvedConnectionFactory) connectionSecretRequests(
	cfg *config.Config,
	manifest secretstore.Manifest,
	purpose secretstore.Purpose,
) ([]secretstore.NamedRequest, []connectionSecretTarget, error) {
	connections := reflect.ValueOf(cfg.SelectedEnvironment.Connections)
	if connections.Kind() == reflect.Pointer {
		connections = connections.Elem()
	}
	connectionType := connections.Type()
	requests := make([]secretstore.NamedRequest, 0)
	targets := make([]connectionSecretTarget, 0)

	for typeIndex := 0; typeIndex < connections.NumField(); typeIndex++ {
		items := connections.Field(typeIndex)
		if items.Kind() != reflect.Slice {
			continue
		}
		typeName := strings.Split(connectionType.Field(typeIndex).Tag.Get("yaml"), ",")[0]
		fieldDefs := workspaceConnectionFieldDefsForType(typeName)
		for itemIndex := 0; itemIndex < items.Len(); itemIndex++ {
			item := items.Index(itemIndex)
			named, ok := item.Addr().Interface().(interface{ GetName() string })
			if !ok {
				continue
			}
			connectionName := named.GetName()
			for _, fieldDef := range fieldDefs {
				if !fieldDef.IsSensitive && !fieldDef.IsSensitiveFile {
					continue
				}
				field, found := workspaceConnectionFieldValue(item, fieldDef.Name)
				if !found || field.Kind() != reflect.String || !field.CanSet() {
					continue
				}
				symbolMatch := exactSecretSymbolPattern.FindStringSubmatch(field.String())
				if len(symbolMatch) != 2 {
					continue
				}
				symbol := symbolMatch[1]
				binding, found := manifest.Binding(cfg.SelectedEnvironmentName, connectionName, fieldDef.Name)
				if !found {
					binding = secretstore.Binding{
						Symbol:    symbol,
						Reference: secretstore.Ref{Provider: "env", Key: symbol},
					}
				}
				if binding.Symbol != symbol {
					return nil, nil, fmt.Errorf(
						"secret binding for %s.%s expects ${%s}, but the connection uses ${%s}",
						connectionName,
						fieldDef.Name,
						binding.Symbol,
						symbol,
					)
				}
				if fieldDef.IsSensitiveFile && binding.Reference.Provider != "env" {
					return nil, nil, fmt.Errorf(
						"secret binding for file field %s.%s must use an env: path reference",
						connectionName,
						fieldDef.Name,
					)
				}
				name := connectionName + "." + fieldDef.Name
				requests = append(requests, secretstore.NamedRequest{
					Name: name,
					Request: secretstore.ResolveRequest{
						ProjectID:   f.projectID,
						Environment: cfg.SelectedEnvironmentName,
						Reference:   binding.Reference,
						Purpose:     purpose,
					},
				})
				targets = append(targets, connectionSecretTarget{name: name, field: field})
			}
		}
	}
	return requests, targets, nil
}
