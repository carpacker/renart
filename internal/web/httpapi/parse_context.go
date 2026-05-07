package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	webapi "renart/internal/web/api"
	"renart/internal/web/service"
)

type ParseContextHandlers interface {
	ParseContext(ctx context.Context, assetID, content string, schema []service.ParseContextSchemaTable) (service.ParseContextResult, *service.ParseContextAPIError)
}

type ParseContextAPI struct {
	Service ParseContextHandlers
}

type ParseContextRequest struct {
	AssetID string                            `json:"asset_id"`
	Content string                            `json:"content"`
	Schema  []service.ParseContextSchemaTable `json:"schema"`
}

func RegisterParseContextRoutes(router chi.Router, handlers *ParseContextAPI) {
	router.Post("/api/sql/parse-context", handlers.HandleParseContext)
}

func (h *ParseContextAPI) HandleParseContext(w http.ResponseWriter, r *http.Request) {
	var req ParseContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		webapi.WriteBadRequest(w, "invalid_request_body", err.Error())
		return
	}

	assetID := strings.TrimSpace(req.AssetID)
	if assetID == "" {
		webapi.WriteBadRequest(w, "asset_id_required", "asset_id is required")
		return
	}

	result, apiErr := h.Service.ParseContext(r.Context(), assetID, req.Content, req.Schema)
	if apiErr != nil {
		webapi.WriteJSON(w, apiErr.Status, map[string]any{
			"status": "error",
			"error":  map[string]string{"code": apiErr.Code, "message": apiErr.Message},
		})
		return
	}

	webapi.WriteJSON(w, http.StatusOK, result)
}
