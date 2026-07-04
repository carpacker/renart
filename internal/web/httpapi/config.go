package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/bruin-data/bruin/pkg/config"
	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/policy"
	"renart/internal/web/service"
)

type ConfigChangePublisher interface {
	ConfigChanged(ctx context.Context, relPath, eventType string)
}

type ConfigHandlers struct {
	Service   *service.ConfigService
	Policies  *policy.Loader
	Publisher ConfigChangePublisher
}

type CreateWorkspaceEnvironmentRequest struct {
	Name         string `json:"name"`
	SchemaPrefix string `json:"schema_prefix"`
	SetAsDefault bool   `json:"set_as_default"`
}

type UpdateWorkspaceEnvironmentRequest struct {
	Name         string `json:"name"`
	NewName      string `json:"new_name"`
	SchemaPrefix string `json:"schema_prefix"`
	SetAsDefault bool   `json:"set_as_default"`
}

type CloneWorkspaceEnvironmentRequest struct {
	SourceName   string `json:"source_name"`
	TargetName   string `json:"target_name"`
	SchemaPrefix string `json:"schema_prefix"`
	SetAsDefault bool   `json:"set_as_default"`
}

type DeleteWorkspaceEnvironmentRequest struct {
	Name string `json:"name"`
}

type UpsertWorkspaceConnectionRequest struct {
	EnvironmentName string         `json:"environment_name"`
	CurrentName     string         `json:"current_name,omitempty"`
	Name            string         `json:"name"`
	Type            string         `json:"type"`
	Values          map[string]any `json:"values"`
}

type DeleteWorkspaceConnectionRequest struct {
	EnvironmentName string `json:"environment_name"`
	Name            string `json:"name"`
}

type UpdateWorkspaceProjectRequest struct {
	Name string `json:"name"`
}

type TestWorkspaceConnectionRequest struct {
	EnvironmentName string         `json:"environment_name"`
	CurrentName     string         `json:"current_name,omitempty"`
	Name            string         `json:"name"`
	Type            string         `json:"type,omitempty"`
	Values          map[string]any `json:"values,omitempty"`
}

func RegisterConfigRoutes(router chi.Router, handlers *ConfigHandlers) {
	router.Get("/api/config", handlers.HandleGetWorkspaceConfig)
	router.Put("/api/config/project", handlers.HandleUpdateWorkspaceProject)
	router.Get("/api/config/environment-policies/{environment}", handlers.HandleGetEnvironmentPolicy)
	router.Put("/api/config/environment-policies/{environment}", handlers.HandleUpdateEnvironmentPolicy)
	router.Post("/api/config/environments", handlers.HandleCreateWorkspaceEnvironment)
	router.Put("/api/config/environments", handlers.HandleUpdateWorkspaceEnvironment)
	router.Post("/api/config/environments/clone", handlers.HandleCloneWorkspaceEnvironment)
	router.Delete("/api/config/environments", handlers.HandleDeleteWorkspaceEnvironment)
	router.Post("/api/config/connections", handlers.HandleCreateWorkspaceConnection)
	router.Put("/api/config/connections", handlers.HandleUpdateWorkspaceConnection)
	router.Delete("/api/config/connections", handlers.HandleDeleteWorkspaceConnection)
	router.Post("/api/config/connections/test", handlers.HandleTestWorkspaceConnection)
}

func (h *ConfigHandlers) HandleGetWorkspaceConfig(w http.ResponseWriter, _ *http.Request) {
	cfg, configPath, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteJSON(w, http.StatusOK, h.Service.BuildParseErrorResponse(err))
		return
	}

	webapi.WriteJSON(w, http.StatusOK, h.Service.BuildResponse(configPath, cfg))
}

func (h *ConfigHandlers) HandleUpdateWorkspaceProject(w http.ResponseWriter, r *http.Request) {
	var req UpdateWorkspaceProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		webapi.WriteBadRequest(w, "missing_project_name", "project name is required")
		return
	}

	if _, err := h.Service.RenameProject(name); err != nil {
		webapi.WriteInternalError(w, "project_rename_failed", err.Error())
		return
	}
	if h.Publisher != nil {
		h.Publisher.ConfigChanged(r.Context(), ".renart/project.yml", "config.updated")
	}

	cfg, configPath, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteJSON(w, http.StatusOK, h.Service.BuildParseErrorResponse(err))
		return
	}
	webapi.WriteJSON(w, http.StatusOK, h.Service.BuildResponse(configPath, cfg))
}

func (h *ConfigHandlers) HandleGetEnvironmentPolicy(w http.ResponseWriter, r *http.Request) {
	environment := strings.TrimSpace(chi.URLParam(r, "environment"))
	if environment == "" {
		webapi.WriteBadRequest(w, "missing_environment", "environment is required")
		return
	}
	if h.Policies == nil {
		webapi.WriteInternalError(w, "policy_loader_missing", "environment policy loader is not configured")
		return
	}

	webapi.WriteJSON(w, http.StatusOK, service.WorkspaceEnvironmentPolicyResponse{
		Status:      "ok",
		Environment: environment,
		Policy:      h.Policies.For(environment),
	})
}

func (h *ConfigHandlers) HandleUpdateEnvironmentPolicy(w http.ResponseWriter, r *http.Request) {
	environment := strings.TrimSpace(chi.URLParam(r, "environment"))
	if environment == "" {
		webapi.WriteBadRequest(w, "missing_environment", "environment is required")
		return
	}
	if h.Policies == nil {
		webapi.WriteInternalError(w, "policy_loader_missing", "environment policy loader is not configured")
		return
	}

	var req policy.EnvironmentPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}
	if _, err := h.Policies.Set(environment, req); err != nil {
		webapi.WriteInternalError(w, "environment_policy_persist_failed", err.Error())
		return
	}
	if h.Publisher != nil {
		h.Publisher.ConfigChanged(r.Context(), ".renart/environments.yml", "config.updated")
	}

	webapi.WriteJSON(w, http.StatusOK, service.WorkspaceEnvironmentPolicyResponse{
		Status:      "ok",
		Environment: environment,
		Policy:      h.Policies.For(environment),
	})
}

func (h *ConfigHandlers) HandleCreateWorkspaceEnvironment(w http.ResponseWriter, r *http.Request) {
	var req CreateWorkspaceEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	cfg, configPath, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteInternalError(w, "config_load_failed", err.Error())
		return
	}

	if err := cfg.AddEnvironment(strings.TrimSpace(req.Name), strings.TrimSpace(req.SchemaPrefix)); err != nil {
		webapi.WriteBadRequest(w, "environment_create_failed", err.Error())
		return
	}
	if req.SetAsDefault {
		cfg.DefaultEnvironmentName = strings.TrimSpace(req.Name)
	}

	h.persistAndRespond(r.Context(), w, cfg, configPath)
}

func (h *ConfigHandlers) HandleUpdateWorkspaceEnvironment(w http.ResponseWriter, r *http.Request) {
	var req UpdateWorkspaceEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	cfg, configPath, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteInternalError(w, "config_load_failed", err.Error())
		return
	}

	currentName := strings.TrimSpace(req.Name)
	nextName := strings.TrimSpace(req.NewName)
	if nextName == "" {
		nextName = currentName
	}
	if err := cfg.UpdateEnvironment(currentName, nextName, strings.TrimSpace(req.SchemaPrefix)); err != nil {
		webapi.WriteBadRequest(w, "environment_update_failed", err.Error())
		return
	}
	if req.SetAsDefault {
		cfg.DefaultEnvironmentName = nextName
	}

	// Renaming must carry the renart policy along, otherwise the guardrails
	// silently stay behind under the old name.
	if h.Policies != nil && nextName != currentName {
		if envPolicy := h.Policies.For(currentName); !envPolicy.Zero() {
			if _, err := h.Policies.Set(nextName, envPolicy); err == nil {
				_, _ = h.Policies.Set(currentName, policy.EnvironmentPolicy{})
			}
		}
	}

	h.persistAndRespond(r.Context(), w, cfg, configPath)
}

func (h *ConfigHandlers) HandleCloneWorkspaceEnvironment(w http.ResponseWriter, r *http.Request) {
	var req CloneWorkspaceEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	cfg, configPath, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteInternalError(w, "config_load_failed", err.Error())
		return
	}

	if err := cfg.CloneEnvironment(strings.TrimSpace(req.SourceName), strings.TrimSpace(req.TargetName), strings.TrimSpace(req.SchemaPrefix)); err != nil {
		webapi.WriteBadRequest(w, "environment_clone_failed", err.Error())
		return
	}

	// Clones inherit the source's guardrails; erring on the protected side
	// beats silently dropping a protection flag.
	if h.Policies != nil {
		if envPolicy := h.Policies.For(strings.TrimSpace(req.SourceName)); !envPolicy.Zero() {
			_, _ = h.Policies.Set(strings.TrimSpace(req.TargetName), envPolicy)
		}
	}
	if req.SetAsDefault {
		cfg.DefaultEnvironmentName = strings.TrimSpace(req.TargetName)
	}

	h.persistAndRespond(r.Context(), w, cfg, configPath)
}

func (h *ConfigHandlers) HandleDeleteWorkspaceEnvironment(w http.ResponseWriter, r *http.Request) {
	var req DeleteWorkspaceEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	cfg, configPath, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteInternalError(w, "config_load_failed", err.Error())
		return
	}

	if err := cfg.DeleteEnvironment(strings.TrimSpace(req.Name)); err != nil {
		webapi.WriteBadRequest(w, "environment_delete_failed", err.Error())
		return
	}
	if h.Policies != nil && !h.Policies.For(strings.TrimSpace(req.Name)).Zero() {
		_, _ = h.Policies.Set(strings.TrimSpace(req.Name), policy.EnvironmentPolicy{})
	}

	h.persistAndRespond(r.Context(), w, cfg, configPath)
}

func (h *ConfigHandlers) HandleCreateWorkspaceConnection(w http.ResponseWriter, r *http.Request) {
	var req UpsertWorkspaceConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	cfg, configPath, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteInternalError(w, "config_load_failed", err.Error())
		return
	}

	if err := h.Service.AddConnection(cfg, service.UpsertWorkspaceConnectionParams{
		EnvironmentName: req.EnvironmentName,
		CurrentName:     req.CurrentName,
		Name:            req.Name,
		Type:            req.Type,
		Values:          req.Values,
	}); err != nil {
		webapi.WriteBadRequest(w, "connection_create_failed", err.Error())
		return
	}

	h.persistAndRespond(r.Context(), w, cfg, configPath)
}

func (h *ConfigHandlers) HandleUpdateWorkspaceConnection(w http.ResponseWriter, r *http.Request) {
	var req UpsertWorkspaceConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	cfg, configPath, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteInternalError(w, "config_load_failed", err.Error())
		return
	}

	if err := h.Service.UpdateConnection(cfg, service.UpsertWorkspaceConnectionParams{
		EnvironmentName: req.EnvironmentName,
		CurrentName:     req.CurrentName,
		Name:            req.Name,
		Type:            req.Type,
		Values:          req.Values,
	}); err != nil {
		webapi.WriteBadRequest(w, "connection_update_failed", err.Error())
		return
	}

	h.persistAndRespond(r.Context(), w, cfg, configPath)
}

func (h *ConfigHandlers) HandleDeleteWorkspaceConnection(w http.ResponseWriter, r *http.Request) {
	var req DeleteWorkspaceConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	cfg, configPath, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteInternalError(w, "config_load_failed", err.Error())
		return
	}

	if err := cfg.DeleteConnection(strings.TrimSpace(req.EnvironmentName), strings.TrimSpace(req.Name)); err != nil {
		webapi.WriteBadRequest(w, "connection_delete_failed", err.Error())
		return
	}

	h.persistAndRespond(r.Context(), w, cfg, configPath)
}

func (h *ConfigHandlers) HandleTestWorkspaceConnection(w http.ResponseWriter, r *http.Request) {
	var req TestWorkspaceConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	cfg, _, err := h.Service.LoadForEditing()
	if err != nil {
		webapi.WriteInternalError(w, "config_load_failed", err.Error())
		return
	}

	message, err := h.Service.TestConnection(r.Context(), cfg, service.TestWorkspaceConnectionParams{
		EnvironmentName: req.EnvironmentName,
		CurrentName:     req.CurrentName,
		Name:            req.Name,
		Type:            req.Type,
		Values:          req.Values,
	})
	if err != nil {
		trimmed := strings.TrimSpace(err.Error())
		switch {
		case trimmed == "no environment selected":
			webapi.WriteBadRequest(w, "missing_environment", trimmed)
		case trimmed == "connection name is required":
			webapi.WriteBadRequest(w, "missing_connection_name", trimmed)
		case strings.Contains(trimmed, "not found"):
			webapi.WriteBadRequest(w, "missing_connection", trimmed)
		case strings.Contains(trimmed, "failed to test connection"):
			webapi.WriteBadRequest(w, "connection_test_failed", trimmed)
		default:
			webapi.WriteInternalError(w, "connection_manager_failed", trimmed)
		}
		return
	}

	webapi.WriteJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": message,
	})
}

func (h *ConfigHandlers) persistAndRespond(ctx context.Context, w http.ResponseWriter, cfg *config.Config, configPath string) {
	relPath, err := h.Service.Persist(cfg)
	if err != nil {
		webapi.WriteInternalError(w, "config_persist_failed", err.Error())
		return
	}
	if h.Publisher != nil {
		h.Publisher.ConfigChanged(ctx, relPath, "config.updated")
	}
	webapi.WriteJSON(w, http.StatusOK, h.Service.BuildResponse(configPath, cfg))
}
