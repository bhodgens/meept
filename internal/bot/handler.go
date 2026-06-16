package bot

import (
	"context"
	"encoding/json"
	"fmt"
)

// RPCHandler provides JSON-RPC handlers for bot management.
type RPCHandler struct {
	manager *Manager
}

// NewRPCHandler creates a new RPC handler for bot operations.
func NewRPCHandler(manager *Manager) *RPCHandler {
	return &RPCHandler{manager: manager}
}

// Handlers returns a map of RPC method names to handler functions.
func (h *RPCHandler) Handlers() map[string]func(context.Context, json.RawMessage) (any, error) {
	return map[string]func(context.Context, json.RawMessage) (any, error){
		"bot.create": h.handleCreate,
		"bot.get":    h.handleGet,
		"bot.list":   h.handleList,
		"bot.update": h.handleUpdate,
		"bot.delete": h.handleDelete,
		"bot.pause":  h.handlePause,
		"bot.resume": h.handleResume,
		"bot.status": h.handleStatus,
	}
}

func (h *RPCHandler) handleCreate(ctx context.Context, raw json.RawMessage) (any, error) {
	var def BotDefinition
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("invalid bot definition: %w", err)
	}
	if err := h.manager.CreateBot(ctx, def); err != nil {
		return nil, err
	}
	return map[string]any{"id": def.ID, "status": "created"}, nil
}

func (h *RPCHandler) handleGet(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	return h.manager.GetBot(ctx, req.ID)
}

func (h *RPCHandler) handleList(ctx context.Context, raw json.RawMessage) (any, error) {
	bots, err := h.manager.ListBots(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"bots": bots}, nil
}

func (h *RPCHandler) handleUpdate(ctx context.Context, raw json.RawMessage) (any, error) {
	var def BotDefinition
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, fmt.Errorf("invalid bot definition: %w", err)
	}
	if err := h.manager.UpdateBot(ctx, def); err != nil {
		return nil, err
	}
	return map[string]any{"id": def.ID, "status": "updated"}, nil
}

func (h *RPCHandler) handleDelete(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if err := h.manager.DeleteBot(ctx, req.ID); err != nil {
		return nil, err
	}
	return map[string]any{"id": req.ID, "status": "deleted"}, nil
}

func (h *RPCHandler) handlePause(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if err := h.manager.PauseBot(ctx, req.ID); err != nil {
		return nil, err
	}
	return map[string]any{"id": req.ID, "status": "paused"}, nil
}

func (h *RPCHandler) handleResume(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	if err := h.manager.ResumeBot(ctx, req.ID); err != nil {
		return nil, err
	}
	return map[string]any{"id": req.ID, "status": "resumed"}, nil
}

func (h *RPCHandler) handleStatus(ctx context.Context, raw json.RawMessage) (any, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	return h.manager.GetBotStatus(ctx, req.ID)
}
