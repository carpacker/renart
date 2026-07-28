package secretstore

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ManifestVersion = 1
	maxManifestSize = 1 << 20
)

type Binding struct {
	Symbol    string `yaml:"symbol" json:"symbol"`
	Reference Ref    `yaml:"ref" json:"ref"`
}

type ConnectionBindings map[string]Binding

type EnvironmentBindings struct {
	Connections map[string]ConnectionBindings `yaml:"connections,omitempty" json:"connections,omitempty"`
}

type Manifest struct {
	Version      int                            `yaml:"version" json:"version"`
	Environments map[string]EnvironmentBindings `yaml:"environments,omitempty" json:"environments,omitempty"`
}

func NewManifest() Manifest {
	return Manifest{
		Version:      ManifestVersion,
		Environments: make(map[string]EnvironmentBindings),
	}
}

func LoadManifest(path string) (Manifest, error) {
	handle, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewManifest(), nil
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("read secret bindings: %w", err)
	}
	defer handle.Close()
	data, err := io.ReadAll(io.LimitReader(handle, maxManifestSize+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("read secret bindings: %w", err)
	}
	if len(data) > maxManifestSize {
		return Manifest{}, fmt.Errorf("secret bindings exceed the %d byte limit", maxManifestSize)
	}

	var manifest Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse secret bindings: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple YAML documents")
		}
		return Manifest{}, fmt.Errorf("parse secret bindings: %w", err)
	}
	if manifest.Environments == nil {
		manifest.Environments = make(map[string]EnvironmentBindings)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.Version != ManifestVersion {
		return fmt.Errorf("unsupported secret bindings version %d", m.Version)
	}
	for environmentName, environment := range m.Environments {
		if strings.TrimSpace(environmentName) == "" {
			return fmt.Errorf("secret binding environment name is required")
		}
		for connectionName, fields := range environment.Connections {
			if strings.TrimSpace(connectionName) == "" {
				return fmt.Errorf("secret binding connection name is required")
			}
			for fieldName, binding := range fields {
				if strings.TrimSpace(fieldName) == "" {
					return fmt.Errorf("secret binding field name is required")
				}
				if !ValidSymbol(binding.Symbol) {
					return fmt.Errorf("secret binding %s.%s.%s has invalid symbol %q", environmentName, connectionName, fieldName, binding.Symbol)
				}
				if err := binding.Reference.Validate(); err != nil {
					return fmt.Errorf("secret binding %s.%s.%s: %w", environmentName, connectionName, fieldName, err)
				}
			}
		}
	}
	return nil
}

func (m Manifest) Binding(environment, connection, field string) (Binding, bool) {
	environmentBindings, found := m.Environments[environment]
	if !found {
		return Binding{}, false
	}
	fields := environmentBindings.Connections[connection]
	binding, found := fields[field]
	return binding, found
}

func (m *Manifest) SetBinding(environment, connection, field string, binding Binding) error {
	if m == nil {
		return errors.New("secret binding manifest is nil")
	}
	environment = strings.TrimSpace(environment)
	connection = strings.TrimSpace(connection)
	field = strings.TrimSpace(field)
	if environment == "" || connection == "" || field == "" {
		return errors.New("secret binding environment, connection, and field are required")
	}
	if m.Version == 0 {
		m.Version = ManifestVersion
	}
	if m.Environments == nil {
		m.Environments = make(map[string]EnvironmentBindings)
	}
	environmentBindings := m.Environments[environment]
	if environmentBindings.Connections == nil {
		environmentBindings.Connections = make(map[string]ConnectionBindings)
	}
	if environmentBindings.Connections[connection] == nil {
		environmentBindings.Connections[connection] = make(ConnectionBindings)
	}
	environmentBindings.Connections[connection][field] = binding
	m.Environments[environment] = environmentBindings
	return m.Validate()
}

func (m *Manifest) RemoveBinding(environment, connection, field string) {
	if m == nil {
		return
	}
	environmentBindings, found := m.Environments[environment]
	if !found {
		return
	}
	fields := environmentBindings.Connections[connection]
	delete(fields, field)
	if len(fields) == 0 {
		delete(environmentBindings.Connections, connection)
	}
	if len(environmentBindings.Connections) == 0 {
		delete(m.Environments, environment)
	} else {
		m.Environments[environment] = environmentBindings
	}
}

func SaveManifest(path string, manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	if len(manifest.Environments) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove empty secret bindings: %w", err)
		}
		directory := filepath.Dir(path)
		handle, err := os.Open(directory)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("open secret binding directory: %w", err)
		}
		defer handle.Close()
		if err := handle.Sync(); err != nil {
			return fmt.Errorf("sync secret binding directory: %w", err)
		}
		return nil
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode secret bindings: %w", err)
	}
	if len(data) > maxManifestSize {
		return fmt.Errorf("secret bindings exceed the %d byte limit", maxManifestSize)
	}

	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create secret binding directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".secrets-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary secret bindings: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write secret bindings: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync secret bindings: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close secret bindings: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace secret bindings: %w", err)
	}
	handle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open secret binding directory: %w", err)
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("sync secret binding directory: %w", err)
	}
	return nil
}
