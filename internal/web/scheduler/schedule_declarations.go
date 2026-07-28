package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	"renart/internal/web/secretstore"
)

const (
	scheduleDeclarationsVersion = 1
	maxScheduleDeclarationsSize = 1 << 20
	storedSecretReferenceKey    = "$renart_secret_ref"
)

// ScheduleDeclaration is the version-controlled desired state for one
// (pipeline UUID, environment). Deployment pins, watermarks, run history, and
// derived next-run timestamps intentionally remain local runtime state.
type ScheduleDeclaration struct {
	Cron          string            `yaml:"cron" json:"cron"`
	Timezone      string            `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	CatchupPolicy CatchupPolicy     `yaml:"catchup_policy,omitempty" json:"catchup_policy,omitempty"`
	Paused        bool              `yaml:"paused,omitempty" json:"paused,omitempty"`
	Variables     map[string]any    `yaml:"variables,omitempty" json:"variables,omitempty"`
	SecretRefs    map[string]string `yaml:"secret_refs,omitempty" json:"secret_refs,omitempty"`
}

// ScheduleDeclarations is the on-disk .renart/schedules.yml schema. Both map
// levels are stable identities rather than presentation names.
type ScheduleDeclarations struct {
	Version   int                                       `yaml:"version" json:"version"`
	Schedules map[string]map[string]ScheduleDeclaration `yaml:"schedules,omitempty" json:"schedules,omitempty"`
}

type DesiredEnvSchedule struct {
	PipelineUUID string
	Environment  string
	Declaration  ScheduleDeclaration
}

type ScheduleDeclarationStore struct {
	path string
	mu   sync.Mutex
}

func NewScheduleDeclarationStore(path string) *ScheduleDeclarationStore {
	return &ScheduleDeclarationStore{path: path}
}

func (s *ScheduleDeclarationStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *ScheduleDeclarationStore) Load() (ScheduleDeclarations, error) {
	if s == nil {
		return ScheduleDeclarations{Version: scheduleDeclarationsVersion}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return loadScheduleDeclarations(s.path)
}

func (s *ScheduleDeclarationStore) List() ([]DesiredEnvSchedule, error) {
	cfg, err := s.Load()
	if err != nil {
		return nil, err
	}
	result := make([]DesiredEnvSchedule, 0)
	for pipelineUUID, environments := range cfg.Schedules {
		for environment, declaration := range environments {
			result = append(result, DesiredEnvSchedule{
				PipelineUUID: pipelineUUID,
				Environment:  environment,
				Declaration:  declaration,
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].PipelineUUID != result[j].PipelineUUID {
			return result[i].PipelineUUID < result[j].PipelineUUID
		}
		return result[i].Environment < result[j].Environment
	})
	return result, nil
}

func (s *ScheduleDeclarationStore) Get(
	pipelineUUID string,
	environment string,
) (ScheduleDeclaration, bool, error) {
	cfg, err := s.Load()
	if err != nil {
		return ScheduleDeclaration{}, false, err
	}
	declaration, found := cfg.Schedules[strings.TrimSpace(pipelineUUID)][strings.TrimSpace(environment)]
	return declaration, found, nil
}

func (s *ScheduleDeclarationStore) Set(
	pipelineUUID string,
	environment string,
	declaration ScheduleDeclaration,
) error {
	if s == nil {
		return errors.New("schedule declaration store is unavailable")
	}
	pipelineUUID = strings.TrimSpace(pipelineUUID)
	environment = strings.TrimSpace(environment)
	if err := validateScheduleDeclarationIdentity(pipelineUUID, environment); err != nil {
		return err
	}
	declaration = normalizeScheduleDeclaration(declaration)
	if err := validateScheduleDeclaration(declaration); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := loadScheduleDeclarations(s.path)
	if err != nil {
		return err
	}
	if cfg.Schedules == nil {
		cfg.Schedules = make(map[string]map[string]ScheduleDeclaration)
	}
	if cfg.Schedules[pipelineUUID] == nil {
		cfg.Schedules[pipelineUUID] = make(map[string]ScheduleDeclaration)
	}
	cfg.Version = scheduleDeclarationsVersion
	cfg.Schedules[pipelineUUID][environment] = declaration
	return saveScheduleDeclarations(s.path, cfg)
}

func (s *ScheduleDeclarationStore) Remove(pipelineUUID, environment string) error {
	if s == nil {
		return errors.New("schedule declaration store is unavailable")
	}
	pipelineUUID = strings.TrimSpace(pipelineUUID)
	environment = strings.TrimSpace(environment)
	if err := validateScheduleDeclarationIdentity(pipelineUUID, environment); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := loadScheduleDeclarations(s.path)
	if err != nil {
		return err
	}
	if environments := cfg.Schedules[pipelineUUID]; environments != nil {
		delete(environments, environment)
		if len(environments) == 0 {
			delete(cfg.Schedules, pipelineUUID)
		}
	}
	if len(cfg.Schedules) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return syncScheduleDeclarationDirectory(filepath.Dir(s.path))
	}
	return saveScheduleDeclarations(s.path, cfg)
}

func loadScheduleDeclarations(path string) (ScheduleDeclarations, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ScheduleDeclarations{Version: scheduleDeclarationsVersion}, nil
		}
		return ScheduleDeclarations{}, fmt.Errorf("read schedule declarations: %w", err)
	}
	if len(data) > maxScheduleDeclarationsSize {
		return ScheduleDeclarations{}, fmt.Errorf("schedule declarations exceed the %d byte limit", maxScheduleDeclarationsSize)
	}

	var cfg ScheduleDeclarations
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return ScheduleDeclarations{}, fmt.Errorf("parse schedule declarations: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple YAML documents")
		}
		return ScheduleDeclarations{}, fmt.Errorf("parse schedule declarations: %w", err)
	}
	if cfg.Version != scheduleDeclarationsVersion {
		return ScheduleDeclarations{}, fmt.Errorf("unsupported schedule declaration version %d", cfg.Version)
	}
	if cfg.Schedules == nil {
		cfg.Schedules = make(map[string]map[string]ScheduleDeclaration)
	}
	for pipelineUUID, environments := range cfg.Schedules {
		if len(environments) == 0 {
			return ScheduleDeclarations{}, fmt.Errorf("schedule declarations for pipeline %q are empty", pipelineUUID)
		}
		for environment, declaration := range environments {
			if err := validateScheduleDeclarationIdentity(pipelineUUID, environment); err != nil {
				return ScheduleDeclarations{}, err
			}
			declaration = normalizeScheduleDeclaration(declaration)
			if err := validateScheduleDeclaration(declaration); err != nil {
				return ScheduleDeclarations{}, fmt.Errorf("schedule %s/%s: %w", pipelineUUID, environment, err)
			}
			environments[environment] = declaration
		}
	}
	return cfg, nil
}

func normalizeScheduleDeclaration(declaration ScheduleDeclaration) ScheduleDeclaration {
	declaration.Cron = strings.TrimSpace(declaration.Cron)
	declaration.Timezone = strings.TrimSpace(declaration.Timezone)
	if declaration.Timezone == "" {
		declaration.Timezone = "UTC"
	}
	if declaration.CatchupPolicy == "" {
		declaration.CatchupPolicy = CatchupSkip
	}
	if len(declaration.Variables) == 0 {
		declaration.Variables = nil
	}
	if len(declaration.SecretRefs) == 0 {
		declaration.SecretRefs = nil
	}
	return declaration
}

func validateScheduleDeclarationIdentity(pipelineUUID, environment string) error {
	for name, value := range map[string]string{
		"pipeline UUID": pipelineUUID,
		"environment":   environment,
	} {
		if value == "" {
			return fmt.Errorf("schedule declaration %s is required", name)
		}
		if strings.TrimSpace(value) != value || strings.ContainsRune(value, 0) || len(value) > 256 {
			return fmt.Errorf("schedule declaration %s must be canonical and at most 256 bytes", name)
		}
	}
	return nil
}

func validateScheduleDeclaration(declaration ScheduleDeclaration) error {
	if declaration.Cron == "" {
		return errors.New("cron is required")
	}
	if _, err := parseSchedule(declaration.Cron, declaration.Timezone); err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	switch declaration.CatchupPolicy {
	case CatchupSkip, CatchupRunOnce, CatchupBackfill:
	default:
		return fmt.Errorf("invalid catchup policy %q", declaration.CatchupPolicy)
	}
	if err := validateScheduleVariableOverridesShape(declaration.Variables); err != nil {
		return err
	}
	if err := validateRunVariableReferences(declaration.Variables, declaration.SecretRefs); err != nil {
		return errors.New(strings.Replace(err.Error(), "run spec variable", "schedule variable", 1))
	}
	return nil
}

func validateScheduleVariableOverridesShape(variables map[string]any) error {
	for name := range variables {
		if err := validateScheduleVariableName(name); err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(variables)
	if err != nil {
		return errors.New("schedule variable overrides must contain JSON-compatible values")
	}
	if len(encoded) > maxRunVariableOverridesBytes {
		return fmt.Errorf("schedule variable overrides exceed the %d byte limit", maxRunVariableOverridesBytes)
	}
	return nil
}

func validateScheduleVariableName(name string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(name) != name || strings.ContainsRune(name, 0) || len(name) > 256 {
		return errors.New("schedule variable names must be canonical and at most 256 bytes")
	}
	return nil
}

func validateScheduleSecretReference(reference string) error {
	parsed, err := secretstore.ParseRef(reference)
	if err != nil {
		return fmt.Errorf("invalid secret reference: %w", err)
	}
	if parsed.Provider != "env" && parsed.Provider != "local" && parsed.Provider != "local-vault" {
		return fmt.Errorf("invalid secret reference: provider %q is not available", parsed.Provider)
	}
	return nil
}

func storedScheduleVariables(variables map[string]any, secretRefs map[string]string) map[string]any {
	if len(variables) == 0 && len(secretRefs) == 0 {
		return nil
	}
	stored := make(map[string]any, len(variables)+len(secretRefs))
	for name, value := range variables {
		stored[name] = value
	}
	for name, reference := range secretRefs {
		stored[name] = map[string]any{storedSecretReferenceKey: reference}
	}
	return stored
}

func splitStoredScheduleVariables(stored map[string]any) (map[string]any, map[string]string) {
	variables := make(map[string]any)
	secretRefs := make(map[string]string)
	for name, value := range stored {
		marker, ok := value.(map[string]any)
		if ok && len(marker) == 1 {
			if reference, referenceOK := marker[storedSecretReferenceKey].(string); referenceOK {
				secretRefs[name] = reference
				continue
			}
		}
		variables[name] = value
	}
	if len(variables) == 0 {
		variables = nil
	}
	if len(secretRefs) == 0 {
		secretRefs = nil
	}
	return variables, secretRefs
}

func resolveEnvironmentScheduleSecrets(
	ctx context.Context,
	environment string,
	references map[string]string,
) (map[string]any, error) {
	if len(references) == 0 {
		return nil, nil
	}
	resolver, err := secretstore.NewResolver(secretstore.NewEnvironmentProvider())
	if err != nil {
		return nil, err
	}
	requests := make([]secretstore.NamedRequest, 0, len(references))
	for variable, reference := range references {
		parsed, err := secretstore.ParseRef(reference)
		if err != nil {
			return nil, fmt.Errorf("schedule variable %q: %w", variable, err)
		}
		requests = append(requests, secretstore.NamedRequest{
			Name: variable,
			Request: secretstore.ResolveRequest{
				Environment: environment,
				Reference:   parsed,
				Purpose: secretstore.PurposeFromContext(
					ctx,
					secretstore.PurposeScheduleValidation,
				),
			},
		})
	}
	bundle, err := resolver.ResolveAll(ctx, requests)
	if err != nil {
		return nil, err
	}
	defer bundle.Close(ctx)
	resolved := make(map[string]any, len(references))
	for variable := range references {
		resolved[variable] = string(bundle.Value(variable))
	}
	return resolved, nil
}

func saveScheduleDeclarations(path string, cfg ScheduleDeclarations) error {
	cfg.Version = scheduleDeclarationsVersion
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode schedule declarations: %w", err)
	}
	if len(data) > maxScheduleDeclarationsSize {
		return fmt.Errorf("schedule declarations exceed the %d byte limit", maxScheduleDeclarationsSize)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create schedule declaration directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".schedules-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary schedule declarations: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write schedule declarations: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync schedule declarations: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close schedule declarations: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace schedule declarations: %w", err)
	}
	return syncScheduleDeclarationDirectory(directory)
}

func syncScheduleDeclarationDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}
