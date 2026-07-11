package service

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/bruin-data/bruin/pkg/pipeline"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
	"renart/internal/web/identity"
	webmodel "renart/internal/web/model"
	"renart/internal/web/scheduler"
)

type PipelineService struct {
	workspaceRoot string
}

func NewPipelineService(workspaceRoot string) *PipelineService {
	return &PipelineService{workspaceRoot: workspaceRoot}
}

func (s *PipelineService) resolver() *WorkspaceResolver {
	return NewWorkspaceResolver(s.workspaceRoot, func(ctx context.Context, pipelinePath string) (*pipeline.Pipeline, error) {
		return s.newPipelineBuilder().CreatePipelineFromPath(ctx, pipelinePath, pipeline.WithMutate(), pipeline.WithOnlyPipeline())
	})
}

func (s *PipelineService) Create(ctx context.Context, relPath, name, content string) (string, error) {
	absPath, err := SafeJoin(s.workspaceRoot, relPath)
	if err != nil {
		return "", err
	}
	fs := afero.NewOsFs()

	if err := fs.MkdirAll(absPath, 0o755); err != nil {
		return "", err
	}

	if strings.TrimSpace(content) == "" {
		if strings.TrimSpace(name) == "" {
			name = filepath.Base(absPath)
		}
		content = fmt.Sprintf("name: %s\n", name)
	}

	pipelineYmlPath := filepath.Join(absPath, "pipeline.yml")
	if err := afero.WriteFile(fs, pipelineYmlPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	if _, _, err := identity.EnsurePipelineID(fs, pipelineYmlPath); err != nil {
		return "", err
	}

	return filepath.ToSlash(relPath), nil
}

func (s *PipelineService) Update(ctx context.Context, pipelineID, name, content string) (string, error) {
	relPath, absPath, err := s.resolver().DecodePipelineID(pipelineID)
	if err != nil {
		return "", err
	}

	if strings.TrimSpace(name) != "" && strings.TrimSpace(content) == "" {
		builder := s.newPipelineBuilder()
		parsed, err := builder.CreatePipelineFromPath(ctx, absPath, pipeline.WithMutate(), pipeline.WithOnlyPipeline())
		if err != nil {
			return "", err
		}

		parsed.Name = strings.TrimSpace(name)
		parsed.DefinitionFile.Path = filepath.Join(absPath, "pipeline.yml")

		if err := parsed.Persist(afero.NewOsFs()); err != nil {
			return "", err
		}

		return filepath.ToSlash(relPath), nil
	}

	if err := afero.WriteFile(afero.NewOsFs(), filepath.Join(absPath, "pipeline.yml"), []byte(content), 0o644); err != nil {
		return "", err
	}

	return filepath.ToSlash(relPath), nil
}

func (s *PipelineService) GetConfig(ctx context.Context, pipelineID string) (*webmodel.PipelineConfigResponse, error) {
	parsed, relPath, err := s.loadPipeline(ctx, pipelineID)
	if err != nil {
		return nil, err
	}

	resp := buildPipelineConfigResponse(pipelineID, filepath.ToSlash(relPath), parsed)
	resp.Status = "ok"
	return resp, nil
}

func (s *PipelineService) GetSchedule(ctx context.Context, pipelineID string) (scheduler.PipelineSchedule, error) {
	parsed, relPath, err := s.loadPipeline(ctx, pipelineID)
	if err != nil {
		return scheduler.PipelineSchedule{}, err
	}
	timezone, catchup := s.readScheduleExtras(relPath)
	schedule := strings.TrimSpace(string(parsed.Schedule))
	return scheduler.PipelineSchedule{
		PipelineID:   pipelineID,
		PipelineUUID: strings.TrimSpace(parsed.LegacyID),
		PipelineName: parsed.Name,
		PipelinePath: filepath.ToSlash(relPath),
		Schedule:     schedule,
		Timezone:     timezone,
		Catchup:      catchup,
		Enabled:      schedule != "",
	}, nil
}

func (s *PipelineService) ListSchedules(ctx context.Context) ([]scheduler.PipelineSchedule, error) {
	state, err := NewWorkspaceService(s.workspaceRoot, "").ComputeState(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]scheduler.PipelineSchedule, 0, len(state.Pipelines))
	for _, item := range state.Pipelines {
		pipelineSchedule, err := s.GetSchedule(ctx, item.ID)
		if err != nil {
			continue
		}
		items = append(items, pipelineSchedule)
	}
	return items, nil
}

func (s *PipelineService) UpdateSchedule(ctx context.Context, pipelineID string, req scheduler.UpdateScheduleRequest) (string, scheduler.PipelineSchedule, error) {
	_, relPath, err := s.loadPipeline(ctx, pipelineID)
	if err != nil {
		return "", scheduler.PipelineSchedule{}, err
	}
	absPath := filepath.Join(s.workspaceRoot, relPath, "pipeline.yml")
	bytes, err := afero.ReadFile(afero.NewOsFs(), absPath)
	if err != nil {
		return "", scheduler.PipelineSchedule{}, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(bytes, &doc); err != nil {
		return "", scheduler.PipelineSchedule{}, err
	}
	root := yamlDocumentMapping(&doc)
	if root == nil {
		return "", scheduler.PipelineSchedule{}, fmt.Errorf("pipeline config must be a YAML mapping")
	}
	schedule := strings.TrimSpace(req.Schedule)
	if schedule == "" {
		schedule = strings.TrimSpace(yamlScalar(root, "schedule"))
	}
	if req.Enabled && schedule == "" {
		return "", scheduler.PipelineSchedule{}, fmt.Errorf("schedule is required when scheduling is enabled")
	}
	if schedule != "" {
		setYAMLScalar(root, "schedule", schedule)
	}
	if strings.TrimSpace(req.Timezone) != "" {
		setYAMLScalar(root, "timezone", strings.TrimSpace(req.Timezone))
	}
	setYAMLBool(root, "catchup", req.Catchup)
	formatted, err := yaml.Marshal(&doc)
	if err != nil {
		return "", scheduler.PipelineSchedule{}, err
	}
	if err := afero.WriteFile(afero.NewOsFs(), absPath, formatted, 0o644); err != nil {
		return "", scheduler.PipelineSchedule{}, err
	}
	updated, err := s.GetSchedule(ctx, pipelineID)
	if err != nil {
		return "", scheduler.PipelineSchedule{}, err
	}
	return filepath.ToSlash(relPath), updated, nil
}

func (s *PipelineService) UpdateConfig(ctx context.Context, pipelineID string, req webmodel.UpdatePipelineConfigRequest) (string, *webmodel.PipelineConfigResponse, error) {
	parsed, relPath, err := s.loadPipeline(ctx, pipelineID)
	if err != nil {
		return "", nil, err
	}

	parsed.Name = strings.TrimSpace(req.Name)
	parsed.Schedule = pipeline.Schedule(strings.TrimSpace(req.Schedule))
	parsed.StartDate = strings.TrimSpace(req.StartDate)
	parsed.Owner = strings.TrimSpace(req.Owner)
	parsed.Tags = normalizeStringArray(req.Tags)
	parsed.Domains = normalizeStringArray(req.Domains)
	parsed.DefaultConnections = buildDefaultConnections(req.DefaultConnections)
	previousCatchup := parsed.Catchup
	parsed.Catchup = pipeline.CatchupNone
	if req.Catchup {
		parsed.Catchup = pipeline.CatchupActive
		if previousCatchup == pipeline.CatchupAll {
			parsed.Catchup = pipeline.CatchupAll
		}
	}
	parsed.MetadataPush = pipeline.MetadataPush{BigQuery: req.MetadataPushBigQuery}
	parsed.Retries = nil
	if req.Retries != 0 {
		parsed.Retries = &req.Retries
	}
	parsed.Concurrency = max(req.Concurrency, 1)
	parsed.MaxActiveSteps = normalizeOptionalInt(req.MaxActiveSteps)
	parsed.Notifications = buildNotifications(req.NotificationsSlack, req.NotificationsTeams)
	defaultValues, err := buildDefaultValues(req.Defaults)
	if err != nil {
		return "", nil, err
	}
	parsed.DefaultValues = defaultValues

	variables, err := buildVariables(req.Variables)
	if err != nil {
		return "", nil, err
	}
	parsed.Variables = variables
	parsed.DefinitionFile.Path = filepath.Join(s.workspaceRoot, relPath, "pipeline.yml")

	if err := parsed.Persist(afero.NewOsFs()); err != nil {
		return "", nil, err
	}

	updated, _, err := s.loadPipeline(ctx, pipelineID)
	if err != nil {
		return "", nil, err
	}

	resp := buildPipelineConfigResponse(pipelineID, filepath.ToSlash(relPath), updated)
	resp.Status = "ok"
	return filepath.ToSlash(relPath), resp, nil
}

func (s *PipelineService) Delete(pipelineID string) (string, error) {
	relPath, absPath, err := s.resolver().DecodePipelineID(pipelineID)
	if err != nil {
		return "", err
	}

	if err := afero.NewOsFs().RemoveAll(absPath); err != nil {
		return "", err
	}

	return filepath.ToSlash(relPath), nil
}

func (s *PipelineService) loadPipeline(ctx context.Context, pipelineID string) (*pipeline.Pipeline, string, error) {
	relPath, _, parsed, err := s.resolver().LoadPipelineByID(ctx, pipelineID)
	if err != nil {
		return nil, "", err
	}

	return parsed, relPath, nil
}

func (s *PipelineService) readScheduleExtras(relPath string) (string, bool) {
	absPath := filepath.Join(s.workspaceRoot, relPath, "pipeline.yml")
	bytes, err := afero.ReadFile(afero.NewOsFs(), absPath)
	if err != nil {
		return "UTC", false
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(bytes, &doc); err != nil {
		return "UTC", false
	}
	root := yamlDocumentMapping(&doc)
	if root == nil {
		return "UTC", false
	}
	timezone := strings.TrimSpace(yamlScalar(root, "timezone"))
	if timezone == "" {
		timezone = "UTC"
	}
	return timezone, yamlBool(root, "catchup")
}

func yamlDocumentMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

func yamlScalar(root *yaml.Node, key string) string {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return root.Content[i+1].Value
		}
	}
	return ""
}

func yamlBool(root *yaml.Node, key string) bool {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			return strings.EqualFold(root.Content[i+1].Value, "true")
		}
	}
	return false
}

func setYAMLScalar(root *yaml.Node, key, value string) {
	setYAMLNode(root, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func setYAMLBool(root *yaml.Node, key string, value bool) {
	setYAMLNode(root, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(value)})
}

func setYAMLNode(root *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1] = value
			return
		}
	}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func removeYAMLKey(root *yaml.Node, key string) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return
		}
	}
}

func buildPipelineConfigResponse(pipelineID, relPath string, parsed *pipeline.Pipeline) *webmodel.PipelineConfigResponse {
	defaultConnections := make([]webmodel.PipelineConfigConnection, 0, len(parsed.DefaultConnections))
	for platform, name := range parsed.DefaultConnections {
		if strings.TrimSpace(platform) == "" || strings.TrimSpace(name) == "" {
			continue
		}
		defaultConnections = append(defaultConnections, webmodel.PipelineConfigConnection{
			Platform: platform,
			Name:     name,
		})
	}
	sort.Slice(defaultConnections, func(i, j int) bool {
		return defaultConnections[i].Platform < defaultConnections[j].Platform
	})

	variables := make([]webmodel.PipelineConfigVariable, 0, len(parsed.Variables))
	variableNames := make([]string, 0, len(parsed.Variables))
	for name := range parsed.Variables {
		variableNames = append(variableNames, name)
	}
	sort.Strings(variableNames)
	for _, name := range variableNames {
		definition := parsed.Variables[name]
		extra := make(map[string]any)
		var variableType string
		var defaultValue any
		var description string
		for key, value := range definition {
			switch key {
			case "type":
				variableType, _ = value.(string)
			case "default":
				defaultValue = value
			case "description":
				description, _ = value.(string)
			default:
				extra[key] = value
			}
		}
		if len(extra) == 0 {
			extra = nil
		}
		variables = append(variables, webmodel.PipelineConfigVariable{
			Name:         name,
			Type:         variableType,
			DefaultValue: defaultValue,
			Description:  description,
			Extra:        extra,
		})
	}

	defaults := webmodel.PipelineConfigDefaults{}
	if parsed.DefaultValues != nil {
		defaults.RerunCooldown = parsed.DefaultValues.RerunCooldown
		defaults.StartOffsetRaw = timeModifierString(parsed.DefaultValues.IntervalModifiers.Start)
		defaults.EndOffsetRaw = timeModifierString(parsed.DefaultValues.IntervalModifiers.End)
	}

	formattedContent, err := parsed.FormatContent()
	if err != nil {
		return nil
	}

	return &webmodel.PipelineConfigResponse{
		ID:                   pipelineID,
		Path:                 relPath,
		Name:                 parsed.Name,
		Schedule:             string(parsed.Schedule),
		StartDate:            parsed.StartDate,
		Owner:                parsed.Owner,
		Tags:                 []string(parsed.Tags),
		Domains:              []string(parsed.Domains),
		DefaultConnections:   defaultConnections,
		Catchup:              parsed.Catchup != pipeline.CatchupNone,
		MetadataPushBigQuery: parsed.MetadataPush.BigQuery,
		Retries:              optionalIntValue(parsed.Retries),
		Concurrency:          parsed.Concurrency,
		MaxActiveSteps:       parsed.MaxActiveSteps,
		NotificationsSlack:   buildSlackNotificationResponse(parsed.Notifications),
		NotificationsTeams:   buildTeamsNotificationResponse(parsed.Notifications),
		Defaults:             defaults,
		Variables:            variables,
		YAML:                 string(formattedContent),
	}
}

func buildSlackNotificationResponse(notifications pipeline.Notifications) webmodel.PipelineConfigNotification {
	if len(notifications.Slack) == 0 {
		return webmodel.PipelineConfigNotification{Success: true, Failure: true}
	}
	item := notifications.Slack[0]
	return webmodel.PipelineConfigNotification{
		Enabled: true,
		Channel: item.Channel,
		Success: item.Success.Bool(),
		Failure: item.Failure.Bool(),
	}
}

func buildTeamsNotificationResponse(notifications pipeline.Notifications) webmodel.PipelineConfigNotification {
	if len(notifications.MSTeams) == 0 {
		return webmodel.PipelineConfigNotification{Success: true, Failure: true}
	}
	item := notifications.MSTeams[0]
	return webmodel.PipelineConfigNotification{
		Enabled:    true,
		Connection: item.Connection,
		Success:    item.Success.Bool(),
		Failure:    item.Failure.Bool(),
	}
}

func normalizeStringArray(values []string) pipeline.EmptyStringArray {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func buildDefaultConnections(input []webmodel.PipelineConfigConnection) pipeline.EmptyStringMap {
	result := make(map[string]string)
	for _, item := range input {
		platform := strings.TrimSpace(item.Platform)
		name := strings.TrimSpace(item.Name)
		if platform == "" || name == "" {
			continue
		}
		result[platform] = name
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func buildNotifications(slack, teams webmodel.PipelineConfigNotification) pipeline.Notifications {
	result := pipeline.Notifications{}
	if slack.Enabled && strings.TrimSpace(slack.Channel) != "" {
		result.Slack = []pipeline.SlackNotification{{
			Channel: strings.TrimSpace(slack.Channel),
			NotificationCommon: pipeline.NotificationCommon{
				Success: boolToDefaultTrue(slack.Success),
				Failure: boolToDefaultTrue(slack.Failure),
			},
		}}
	}
	if teams.Enabled && strings.TrimSpace(teams.Connection) != "" {
		result.MSTeams = []pipeline.MSTeamsNotification{{
			Connection: strings.TrimSpace(teams.Connection),
			NotificationCommon: pipeline.NotificationCommon{
				Success: boolToDefaultTrue(teams.Success),
				Failure: boolToDefaultTrue(teams.Failure),
			},
		}}
	}
	return result
}

func boolToDefaultTrue(value bool) pipeline.DefaultTrueBool {
	copyValue := value
	return pipeline.DefaultTrueBool{Value: &copyValue}
}

func buildDefaultValues(input webmodel.PipelineConfigDefaults) (*pipeline.DefaultValues, error) {
	var values *pipeline.DefaultValues
	if input.RerunCooldown != nil {
		values = &pipeline.DefaultValues{RerunCooldown: normalizeOptionalInt(input.RerunCooldown)}
	}

	intervals := pipeline.IntervalModifiers{}
	start, ok, err := parseTimeModifierString(input.StartOffsetRaw)
	if err != nil {
		return nil, err
	}
	if ok {
		intervals.Start = start
		if values == nil {
			values = &pipeline.DefaultValues{}
		}
	}
	end, ok, err := parseTimeModifierString(input.EndOffsetRaw)
	if err != nil {
		return nil, err
	}
	if ok {
		intervals.End = end
		if values == nil {
			values = &pipeline.DefaultValues{}
		}
	}
	if values != nil {
		values.IntervalModifiers = intervals
	}

	if values == nil {
		return nil, nil
	}
	return values, nil
}

func buildVariables(input []webmodel.PipelineConfigVariable) (pipeline.Variables, error) {
	if len(input) == 0 {
		return nil, nil
	}

	result := make(pipeline.Variables)
	for _, variable := range input {
		name := strings.TrimSpace(variable.Name)
		variableType := strings.TrimSpace(variable.Type)
		if name == "" {
			continue
		}
		if variableType == "" {
			return nil, fmt.Errorf("variable %q must have a type", name)
		}
		definition := map[string]any{
			"type":    variableType,
			"default": variable.DefaultValue,
		}
		if description := strings.TrimSpace(variable.Description); description != "" {
			definition["description"] = description
		}
		for key, value := range variable.Extra {
			trimmedKey := strings.TrimSpace(key)
			if trimmedKey == "" || trimmedKey == "type" || trimmedKey == "default" || trimmedKey == "description" {
				continue
			}
			definition[trimmedKey] = value
		}
		result[name] = definition
	}

	if len(result) == 0 {
		return nil, nil
	}
	if err := (&result).Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

func parseTimeModifierString(raw string) (pipeline.TimeModifier, bool, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return pipeline.TimeModifier{}, false, nil
	}

	var modifier pipeline.TimeModifier
	if strings.Contains(trimmed, "{{") || strings.Contains(trimmed, "{%") {
		modifier.Template = trimmed
		return modifier, true, nil
	}

	parts := strings.Fields(trimmed)
	if len(parts) > 1 {
		return pipeline.TimeModifier{}, false, fmt.Errorf("invalid interval modifier %q", trimmed)
	}

	suffix := trimmed[len(trimmed)-1]
	numeric := trimmed[:len(trimmed)-1]
	if len(trimmed) >= 3 {
		twoCharSuffix := trimmed[len(trimmed)-2:]
		if twoCharSuffix == "ms" || twoCharSuffix == "ns" {
			numeric = trimmed[:len(trimmed)-2]
			suffix = 0
			switch twoCharSuffix {
			case "ms":
				if value, err := parseNumericModifier(numeric); err == nil {
					modifier.Milliseconds = value
					return modifier, true, nil
				}
			case "ns":
				if value, err := parseNumericModifier(numeric); err == nil {
					modifier.Nanoseconds = value
					return modifier, true, nil
				}
			}
			return pipeline.TimeModifier{}, false, fmt.Errorf("invalid interval modifier %q", trimmed)
		}
	}

	value, err := parseNumericModifier(numeric)
	if err != nil {
		return pipeline.TimeModifier{}, false, fmt.Errorf("invalid interval modifier %q", trimmed)
	}
	switch suffix {
	case 'h':
		modifier.Hours = value
	case 'm':
		modifier.Minutes = value
	case 's':
		modifier.Seconds = value
	case 'd':
		modifier.Days = value
	case 'M':
		modifier.Months = value
	default:
		return pipeline.TimeModifier{}, false, fmt.Errorf("invalid interval modifier %q", trimmed)
	}
	return modifier, true, nil
}

func parseNumericModifier(raw string) (int, error) {
	return strconv.Atoi(strings.TrimSpace(raw))
}

func timeModifierString(modifier pipeline.TimeModifier) string {
	if modifier.Template != "" {
		return modifier.Template
	}
	if modifier.Months != 0 {
		return fmt.Sprintf("%dM", modifier.Months)
	}
	if modifier.Days != 0 {
		return fmt.Sprintf("%dd", modifier.Days)
	}
	if modifier.Hours != 0 {
		return fmt.Sprintf("%dh", modifier.Hours)
	}
	if modifier.Minutes != 0 {
		return fmt.Sprintf("%dm", modifier.Minutes)
	}
	if modifier.Seconds != 0 {
		return fmt.Sprintf("%ds", modifier.Seconds)
	}
	if modifier.Milliseconds != 0 {
		return fmt.Sprintf("%dms", modifier.Milliseconds)
	}
	if modifier.Nanoseconds != 0 {
		return fmt.Sprintf("%dns", modifier.Nanoseconds)
	}
	return ""
}

func normalizeOptionalInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func optionalIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func max(value, floor int) int {
	if value < floor {
		return floor
	}
	return value
}

func (s *PipelineService) newPipelineBuilder() *pipeline.Builder {
	return NewRenartPipelineBuilder(afero.NewOsFs())
}
