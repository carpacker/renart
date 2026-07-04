package service

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/spf13/afero"
	"renart/internal/web/identity"
	"renart/internal/web/policy"
)

type WorkspaceConfigFieldDef struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	DefaultValue string `json:"default_value,omitempty"`
	IsRequired   bool   `json:"is_required"`
}

type WorkspaceConfigConnectionType struct {
	TypeName string                    `json:"type_name"`
	Fields   []WorkspaceConfigFieldDef `json:"fields"`
}

type WorkspaceConfigConnection struct {
	Name   string         `json:"name"`
	Type   string         `json:"type"`
	Values map[string]any `json:"values"`
	// SlingCategory is "database", "storage", "file" for connections a Sling
	// asset can move data between, or "" for connections that are not Sling-movable
	// data stores. The asset editor's source/target pickers filter on it.
	SlingCategory string `json:"sling_category,omitempty"`
}

type WorkspaceConfigEnvironment struct {
	Name         string                      `json:"name"`
	SchemaPrefix string                      `json:"schema_prefix,omitempty"`
	Connections  []WorkspaceConfigConnection `json:"connections"`
}

type WorkspaceConfigResponse struct {
	Status              string                          `json:"status"`
	Path                string                          `json:"path"`
	WorkspacePath       string                          `json:"workspace_path,omitempty"`
	ProjectID           string                          `json:"project_id,omitempty"`
	ProjectName         string                          `json:"project_name,omitempty"`
	DefaultEnvironment  string                          `json:"default_environment,omitempty"`
	SelectedEnvironment string                          `json:"selected_environment,omitempty"`
	Environments        []WorkspaceConfigEnvironment    `json:"environments"`
	ConnectionTypes     []WorkspaceConfigConnectionType `json:"connection_types"`
	ParseError          string                          `json:"parse_error,omitempty"`
}

type WorkspaceEnvironmentPolicyResponse struct {
	Status      string                   `json:"status"`
	Environment string                   `json:"environment"`
	Policy      policy.EnvironmentPolicy `json:"policy"`
}

type UpsertWorkspaceConnectionParams struct {
	EnvironmentName string
	CurrentName     string
	Name            string
	Type            string
	Values          map[string]any
}

type TestWorkspaceConnectionParams struct {
	EnvironmentName string
	CurrentName     string
	Name            string
	Type            string
	Values          map[string]any
}

type ConfigService struct {
	workspaceRoot string
	configPath    string
}

func NewConfigService(workspaceRoot, configPath string) *ConfigService {
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(workspaceRoot, ".bruin.yml")
	}

	return &ConfigService{workspaceRoot: workspaceRoot, configPath: configPath}
}

func (s *ConfigService) ConfigPath() string {
	return s.configPath
}

func (s *ConfigService) projectYmlPath() string {
	return filepath.Join(s.workspaceRoot, ".renart", "project.yml")
}

func (s *ConfigService) defaultProjectName() string {
	return filepath.Base(filepath.Clean(s.workspaceRoot))
}

// ProjectIdentity self-assigns .renart/project.yml on first use. Errors
// degrade to a nameless-but-usable identity so a corrupt project.yml never
// takes the config API down.
func (s *ConfigService) ProjectIdentity() identity.Project {
	project, err := identity.EnsureProject(afero.NewOsFs(), s.projectYmlPath(), s.defaultProjectName())
	if err != nil {
		return identity.Project{Name: s.defaultProjectName()}
	}
	return project
}

func (s *ConfigService) RenameProject(name string) (identity.Project, error) {
	fs := afero.NewOsFs()
	project, err := identity.EnsureProject(fs, s.projectYmlPath(), s.defaultProjectName())
	if err != nil {
		return identity.Project{}, err
	}
	project.Name = name
	if err := identity.SaveProject(fs, s.projectYmlPath(), project); err != nil {
		return identity.Project{}, err
	}
	return project, nil
}

func (s *ConfigService) LoadForEditing() (*config.Config, string, error) {
	cfg, err := config.LoadOrCreateWithoutPathAbsolutization(afero.NewOsFs(), s.configPath)
	if err != nil {
		return nil, s.configPath, err
	}

	return cfg, s.configPath, nil
}

func (s *ConfigService) Persist(cfg *config.Config) (string, error) {
	if err := afero.NewOsFs().MkdirAll(filepath.Dir(s.configPath), 0o755); err != nil {
		return "", err
	}
	if err := cfg.Persist(); err != nil {
		return "", err
	}

	relPath, err := filepath.Rel(s.workspaceRoot, s.configPath)
	if err != nil {
		relPath = filepath.Base(s.configPath)
	}

	return filepath.ToSlash(relPath), nil
}

func (s *ConfigService) BuildResponse(configPath string, cfg *config.Config) WorkspaceConfigResponse {
	project := s.ProjectIdentity()
	response := WorkspaceConfigResponse{
		Status:              "ok",
		Path:                filepath.Base(configPath),
		WorkspacePath:       filepath.Clean(s.workspaceRoot),
		ProjectID:           project.ID,
		ProjectName:         project.Name,
		DefaultEnvironment:  cfg.DefaultEnvironmentName,
		SelectedEnvironment: cfg.SelectedEnvironmentName,
		Environments:        []WorkspaceConfigEnvironment{},
		ConnectionTypes:     BuildWorkspaceConfigConnectionTypes(),
	}

	environmentNames := cfg.GetEnvironmentNames()
	sort.Strings(environmentNames)
	for _, envName := range environmentNames {
		env := cfg.Environments[envName]
		response.Environments = append(response.Environments, WorkspaceConfigEnvironment{
			Name:         envName,
			SchemaPrefix: env.SchemaPrefix,
			Connections:  buildWorkspaceConfigConnections(env.Connections),
		})
	}

	return response
}

func (s *ConfigService) BuildParseErrorResponse(parseErr error) WorkspaceConfigResponse {
	project := s.ProjectIdentity()
	return WorkspaceConfigResponse{
		Status:          "ok",
		Path:            filepath.Base(s.configPath),
		WorkspacePath:   filepath.Clean(s.workspaceRoot),
		ProjectID:       project.ID,
		ProjectName:     project.Name,
		Environments:    []WorkspaceConfigEnvironment{},
		ConnectionTypes: BuildWorkspaceConfigConnectionTypes(),
		ParseError:      parseErr.Error(),
	}
}

func mapsClone(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func (s *ConfigService) AddConnection(cfg *config.Config, params UpsertWorkspaceConnectionParams) error {
	environmentName := strings.TrimSpace(params.EnvironmentName)
	name := strings.TrimSpace(params.Name)
	typeName := strings.TrimSpace(params.Type)
	if environmentName == "" || name == "" || typeName == "" {
		return fmt.Errorf("environment, name, and type are required")
	}

	if _, exists := cfg.Environments[environmentName]; !exists {
		if err := cfg.AddEnvironment(environmentName, ""); err != nil {
			return err
		}
		if strings.TrimSpace(cfg.DefaultEnvironmentName) == "" {
			cfg.DefaultEnvironmentName = environmentName
		}
		if strings.TrimSpace(cfg.SelectedEnvironmentName) == "" {
			cfg.SelectedEnvironmentName = environmentName
		}
	}

	values, err := normalizeWorkspaceConnectionValues(typeName, params.Values)
	if err != nil {
		return err
	}

	return cfg.AddConnection(environmentName, name, typeName, values)
}

func (s *ConfigService) UpdateConnection(cfg *config.Config, params UpsertWorkspaceConnectionParams) error {
	environmentName := strings.TrimSpace(params.EnvironmentName)
	currentName := strings.TrimSpace(params.CurrentName)
	if currentName == "" {
		currentName = strings.TrimSpace(params.Name)
	}

	if err := cfg.DeleteConnection(environmentName, currentName); err != nil {
		return err
	}

	return s.AddConnection(cfg, params)
}

func (s *ConfigService) TestConnection(ctx context.Context, cfg *config.Config, params TestWorkspaceConnectionParams) (string, error) {
	environmentName, err := requireEnvironmentName(cfg, params.EnvironmentName)
	if err != nil {
		return "", err
	}

	connectionName := strings.TrimSpace(params.Name)
	if connectionName == "" {
		return "", fmt.Errorf("connection name is required")
	}

	if strings.TrimSpace(params.Type) != "" {
		if err := s.prepareDraftConnection(cfg, TestWorkspaceConnectionParams{
			EnvironmentName: environmentName,
			CurrentName:     params.CurrentName,
			Name:            connectionName,
			Type:            params.Type,
			Values:          params.Values,
		}); err != nil {
			return "", err
		}
	}

	selectedCfg, err := selectConfigEnvironment(cfg, environmentName)
	if err != nil {
		return "", err
	}

	manager, err := newConnectionManagerFromConfig(ctx, selectedCfg)
	if err != nil {
		return "", err
	}

	conn := manager.GetConnection(connectionName)
	if conn == nil {
		return "", fmt.Errorf("connection %q not found", connectionName)
	}

	tester, ok := conn.(interface{ Ping(context.Context) error })
	if !ok {
		return fmt.Sprintf("Connection '%s' does not support validation yet.", connectionName), nil
	}

	if err := tester.Ping(ctx); err != nil {
		return "", fmt.Errorf("failed to test connection '%s': %w", connectionName, err)
	}

	return fmt.Sprintf("Successfully validated connection '%s' in environment %s.", connectionName, environmentName), nil
}

func (s *ConfigService) prepareDraftConnection(cfg *config.Config, params TestWorkspaceConnectionParams) error {
	environmentName := strings.TrimSpace(params.EnvironmentName)
	name := strings.TrimSpace(params.Name)
	typeName := strings.TrimSpace(params.Type)
	if environmentName == "" || name == "" || typeName == "" {
		return fmt.Errorf("environment, name, and type are required")
	}

	if _, exists := cfg.Environments[environmentName]; !exists {
		if err := cfg.AddEnvironment(environmentName, ""); err != nil {
			return err
		}
		if strings.TrimSpace(cfg.DefaultEnvironmentName) == "" {
			cfg.DefaultEnvironmentName = environmentName
		}
		if strings.TrimSpace(cfg.SelectedEnvironmentName) == "" {
			cfg.SelectedEnvironmentName = environmentName
		}
	}

	currentName := strings.TrimSpace(params.CurrentName)
	if currentName == "" {
		currentName = name
	}

	if err := cfg.DeleteConnection(environmentName, currentName); err != nil && !strings.Contains(err.Error(), "does not exist") {
		return err
	}

	values, err := normalizeWorkspaceConnectionValues(typeName, params.Values)
	if err != nil {
		return err
	}

	return cfg.AddConnection(environmentName, name, typeName, values)
}

func BuildWorkspaceConfigConnectionTypes() []WorkspaceConfigConnectionType {
	connectionsType := reflect.TypeFor[config.Connections]()
	items := make([]WorkspaceConfigConnectionType, 0, connectionsType.NumField())
	for index := 0; index < connectionsType.NumField(); index++ {
		structField := connectionsType.Field(index)
		if !structField.IsExported() || structField.Type.Kind() != reflect.Slice {
			continue
		}

		typeName := structField.Tag.Get("yaml")
		if separator := strings.Index(typeName, ","); separator >= 0 {
			typeName = typeName[:separator]
		}
		if typeName == "" {
			continue
		}

		elementType := structField.Type.Elem()
		if elementType.Kind() == reflect.Pointer {
			elementType = elementType.Elem()
		}
		if elementType.Kind() != reflect.Struct {
			continue
		}

		items = append(items, WorkspaceConfigConnectionType{
			TypeName: typeName,
			Fields:   buildWorkspaceConfigFieldDefs(elementType),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].TypeName < items[j].TypeName
	})

	return items
}

func buildWorkspaceConfigFieldDefs(connectionType reflect.Type) []WorkspaceConfigFieldDef {
	fields := make([]WorkspaceConfigFieldDef, 0, connectionType.NumField())
	for index := 0; index < connectionType.NumField(); index++ {
		structField := connectionType.Field(index)
		if !structField.IsExported() {
			continue
		}

		mapstructureTag := structField.Tag.Get("mapstructure")
		if separator := strings.Index(mapstructureTag, ","); separator >= 0 {
			mapstructureTag = mapstructureTag[:separator]
		}
		if mapstructureTag == "" || mapstructureTag == "name" {
			continue
		}

		fieldType := buildWorkspaceConfigFieldType(structField.Type)
		if fieldType == "" {
			continue
		}

		defaultValues := make([]string, 0)
		if jsonschemaTag := structField.Tag.Get("jsonschema"); jsonschemaTag != "" {
			for part := range strings.SplitSeq(jsonschemaTag, ",") {
				part = strings.TrimSpace(part)
				if value, ok := strings.CutPrefix(part, "default="); ok {
					defaultValues = append(defaultValues, value)
				}
			}
		}
		defaultValue := strings.Join(defaultValues, ",")
		if defaultValue == "" {
			defaultValue = structField.Tag.Get("default")
		}

		yamlTag := structField.Tag.Get("yaml")
		fields = append(fields, WorkspaceConfigFieldDef{
			Name:         mapstructureTag,
			Type:         fieldType,
			DefaultValue: defaultValue,
			IsRequired:   !strings.Contains(yamlTag, "omitempty"),
		})
	}

	return fields
}

func buildWorkspaceConfigFieldType(fieldType reflect.Type) string {
	switch fieldType.Kind() { //nolint:exhaustive
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Bool:
		return "bool"
	case reflect.Slice:
		if fieldType.Elem().Kind() == reflect.String {
			return "string_array"
		}
		return ""
	default:
		return ""
	}
}

func buildWorkspaceConfigConnections(connections *config.Connections) []WorkspaceConfigConnection {
	if connections == nil {
		return []WorkspaceConfigConnection{}
	}

	value := reflect.ValueOf(connections)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return []WorkspaceConfigConnection{}
	}

	valueType := value.Type()
	items := make([]WorkspaceConfigConnection, 0)
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		structField := valueType.Field(index)
		if field.Kind() != reflect.Slice {
			continue
		}

		typeName := structField.Tag.Get("yaml")
		if separator := strings.Index(typeName, ","); separator >= 0 {
			typeName = typeName[:separator]
		}
		if typeName == "" {
			continue
		}

		for itemIndex := 0; itemIndex < field.Len(); itemIndex++ {
			connectionValue := field.Index(itemIndex)
			connectionInterface := connectionValue.Interface()
			named, ok := connectionInterface.(interface{ GetName() string })
			if !ok {
				continue
			}

			items = append(items, WorkspaceConfigConnection{
				Name:          named.GetName(),
				Type:          typeName,
				Values:        buildWorkspaceConfigConnectionValues(connectionInterface, typeName),
				SlingCategory: slingConnectionCategory(typeName),
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Type == items[j].Type {
			return items[i].Name < items[j].Name
		}
		return items[i].Type < items[j].Type
	})

	return items
}

func buildWorkspaceConfigConnectionValues(connectionValue any, typeName string) map[string]any {
	result := make(map[string]any)
	fieldDefs := workspaceConnectionFieldDefsForType(typeName)
	if len(fieldDefs) == 0 {
		return result
	}

	value := reflect.ValueOf(connectionValue)
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return result
	}

	valueType := value.Type()
	for _, fieldDef := range fieldDefs {
		for index := 0; index < value.NumField(); index++ {
			structField := valueType.Field(index)
			mapstructureTag := structField.Tag.Get("mapstructure")
			if separator := strings.Index(mapstructureTag, ","); separator >= 0 {
				mapstructureTag = mapstructureTag[:separator]
			}
			if mapstructureTag != fieldDef.Name {
				continue
			}

			fieldValue := value.Field(index)
			switch fieldValue.Kind() { //nolint:exhaustive
			case reflect.String:
				result[fieldDef.Name] = fieldValue.String()
			case reflect.Bool:
				result[fieldDef.Name] = fieldValue.Bool()
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				result[fieldDef.Name] = fieldValue.Int()
			case reflect.Slice:
				if fieldValue.Type().Elem().Kind() != reflect.String {
					continue
				}
				values := make([]string, 0, fieldValue.Len())
				for itemIndex := 0; itemIndex < fieldValue.Len(); itemIndex++ {
					values = append(values, fieldValue.Index(itemIndex).String())
				}
				result[fieldDef.Name] = values
			}
			break
		}
	}

	return result
}

func normalizeWorkspaceConnectionValues(typeName string, values map[string]any) (map[string]any, error) {
	result := make(map[string]any)
	fieldDefs := workspaceConnectionFieldDefsForType(typeName)
	for _, fieldDef := range fieldDefs {
		rawValue, exists := values[fieldDef.Name]
		if !exists {
			continue
		}

		switch fieldDef.Type {
		case "string":
			result[fieldDef.Name] = strings.TrimSpace(fmt.Sprint(rawValue))
		case "bool":
			boolValue, err := normalizeWorkspaceBoolValue(rawValue)
			if err != nil {
				return nil, fmt.Errorf("invalid value for %s: %w", fieldDef.Name, err)
			}
			result[fieldDef.Name] = boolValue
		case "int":
			intValue, err := normalizeWorkspaceIntValue(rawValue)
			if err != nil {
				return nil, fmt.Errorf("invalid value for %s: %w", fieldDef.Name, err)
			}
			result[fieldDef.Name] = intValue
		case "string_array":
			result[fieldDef.Name] = normalizeWorkspaceStringArrayValue(rawValue)
		}
	}

	return result, nil
}

func workspaceConnectionFieldDefsForType(typeName string) []WorkspaceConfigFieldDef {
	for _, connectionType := range BuildWorkspaceConfigConnectionTypes() {
		if connectionType.TypeName == typeName {
			return connectionType.Fields
		}
	}
	return nil
}

func normalizeWorkspaceStringArrayValue(rawValue any) []string {
	switch value := rawValue.(type) {
	case []string:
		return compactWorkspaceStringArray(value)
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			items = append(items, fmt.Sprint(item))
		}
		return compactWorkspaceStringArray(items)
	case string:
		return compactWorkspaceStringArray(strings.Split(value, ","))
	default:
		return compactWorkspaceStringArray([]string{fmt.Sprint(value)})
	}
}

func compactWorkspaceStringArray(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func normalizeWorkspaceBoolValue(rawValue any) (bool, error) {
	switch value := rawValue.(type) {
	case bool:
		return value, nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return false, nil
		}
		if strings.EqualFold(trimmed, "true") {
			return true, nil
		}
		if strings.EqualFold(trimmed, "false") {
			return false, nil
		}
	}

	return false, fmt.Errorf("expected boolean")
}

func normalizeWorkspaceIntValue(rawValue any) (int, error) {
	switch value := rawValue.(type) {
	case int:
		return value, nil
	case int8:
		return int(value), nil
	case int16:
		return int(value), nil
	case int32:
		return int(value), nil
	case int64:
		return int(value), nil
	case float64:
		return int(value), nil
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0, nil
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	}

	return 0, fmt.Errorf("expected integer")
}
