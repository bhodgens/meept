package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// ModelRPCHandler handles model RPC methods.
type ModelRPCHandler struct {
	model *services.ModelService
}

// NewModelRPCHandler creates a new model RPC handler.
func NewModelRPCHandler(model *services.ModelService) *ModelRPCHandler {
	return &ModelRPCHandler{model: model}
}

// RegisterModelMethods registers all model RPC methods.
func (h *ModelRPCHandler) RegisterModelMethods(server *rpc.Server) {
	server.RegisterHandler("models.list", h.handleList)
	server.RegisterHandler("models.providers", h.handleProviders)
	server.RegisterHandler("models.get_default", h.handleGetDefault)
	server.RegisterHandler("models.set_default", h.handleSetDefault)
	server.RegisterHandler("models.remove", h.handleRemove)
	server.RegisterHandler("models.get_credential", h.handleGetCredential)
	server.RegisterHandler("models.set_credential", h.handleSetCredential)
	server.RegisterHandler("models.delete_credential", h.handleDeleteCredential)
}

// handleList returns all configured models.
func (h *ModelRPCHandler) handleList(ctx context.Context, params json.RawMessage) (any, error) {
	if h.model == nil {
		return nil, fmt.Errorf("model service not available")
	}

	models, err := h.model.List(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"models": models,
		"count":  len(models),
	}, nil
}

// handleProviders returns all available providers.
func (h *ModelRPCHandler) handleProviders(ctx context.Context, params json.RawMessage) (any, error) {
	if h.model == nil {
		return nil, fmt.Errorf("model service not available")
	}

	providers, err := h.model.Providers(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"providers": providers,
		"count":     len(providers),
	}, nil
}

// handleGetDefault returns the default model.
func (h *ModelRPCHandler) handleGetDefault(ctx context.Context, params json.RawMessage) (any, error) {
	if h.model == nil {
		return nil, fmt.Errorf("model service not available")
	}

	model, err := h.model.GetDefault(ctx)
	if err != nil {
		return nil, err
	}

	return model, nil
}

// handleSetDefault sets the default model.
func (h *ModelRPCHandler) handleSetDefault(ctx context.Context, params json.RawMessage) (any, error) {
	if h.model == nil {
		return nil, fmt.Errorf("model service not available")
	}

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if err := h.model.SetDefault(ctx, req.Provider, req.Model); err != nil {
		return nil, err
	}

	return map[string]string{"status": "updated"}, nil
}

// handleRemove removes a model.
func (h *ModelRPCHandler) handleRemove(ctx context.Context, params json.RawMessage) (any, error) {
	if h.model == nil {
		return nil, fmt.Errorf("model service not available")
	}

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if err := h.model.Remove(ctx, req.Provider, req.Model); err != nil {
		return nil, err
	}

	return map[string]string{"status": "removed"}, nil
}

// handleGetCredential returns a masked credential.
func (h *ModelRPCHandler) handleGetCredential(ctx context.Context, params json.RawMessage) (any, error) {
	if h.model == nil {
		return nil, fmt.Errorf("model service not available")
	}

	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	cred, err := h.model.GetCredential(ctx, req.Provider)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"provider":   req.Provider,
		"credential": cred,
	}, nil
}

// handleSetCredential sets a credential.
func (h *ModelRPCHandler) handleSetCredential(ctx context.Context, params json.RawMessage) (any, error) {
	if h.model == nil {
		return nil, fmt.Errorf("model service not available")
	}

	var req struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if err := h.model.SetCredential(ctx, req.Provider, req.APIKey); err != nil {
		return nil, err
	}

	return map[string]string{"status": "updated"}, nil
}

// handleDeleteCredential deletes a credential.
func (h *ModelRPCHandler) handleDeleteCredential(ctx context.Context, params json.RawMessage) (any, error) {
	if h.model == nil {
		return nil, fmt.Errorf("model service not available")
	}

	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if err := h.model.DeleteCredential(ctx, req.Provider); err != nil {
		return nil, err
	}

	return map[string]string{"status": "deleted"}, nil
}
