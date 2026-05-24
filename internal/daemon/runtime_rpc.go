package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// RuntimeRPCHandler handles runtime-related RPC methods.
type RuntimeRPCHandler struct {
	service *services.RuntimeService
}

// NewRuntimeRPCHandler creates a new runtime RPC handler.
func NewRuntimeRPCHandler(service *services.RuntimeService) *RuntimeRPCHandler {
	return &RuntimeRPCHandler{service: service}
}

// RegisterRuntimeMethods registers runtime RPC methods.
func (h *RuntimeRPCHandler) RegisterRuntimeMethods(server *rpc.Server) {
	server.RegisterHandler("runtime.status", h.handleStatus)
	server.RegisterHandler("runtime.start", h.handleStart)
	server.RegisterHandler("runtime.stop", h.handleStop)
	server.RegisterHandler("runtime.restart", h.handleRestart)
}

func (h *RuntimeRPCHandler) handleStatus(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("runtime service not available")
	}

	var req struct {
		Provider string `json:"provider"`
	}
	if params != nil {
		_ = json.Unmarshal(params, &req)
	}

	if req.Provider != "" {
		resp, err := h.service.StatusForProvider(ctx, req.Provider)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			rpc.RPCKeyStatus: "ok",
			"runtime":        resp,
		}, nil
	}

	resp, err := h.service.Status(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		rpc.RPCKeyStatus: "ok",
		"runtimes":       resp.Runtimes,
	}, nil
}

func (h *RuntimeRPCHandler) handleStart(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("runtime service not available")
	}

	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Provider == "" {
		req.Provider = "local"
	}

	if err := h.service.StartProvider(ctx, req.Provider); err != nil {
		return nil, err
	}
	return map[string]any{
		rpc.RPCKeyStatus: "started",
		"provider":       req.Provider,
	}, nil
}

func (h *RuntimeRPCHandler) handleStop(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("runtime service not available")
	}

	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Provider == "" {
		req.Provider = "local"
	}

	if err := h.service.StopProvider(ctx, req.Provider); err != nil {
		return nil, err
	}
	return map[string]any{
		rpc.RPCKeyStatus: "stopped",
		"provider":       req.Provider,
	}, nil
}

func (h *RuntimeRPCHandler) handleRestart(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("runtime service not available")
	}

	var req struct {
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Provider == "" {
		req.Provider = "local"
	}

	if err := h.service.RestartProvider(ctx, req.Provider); err != nil {
		return nil, err
	}
	return map[string]any{
		rpc.RPCKeyStatus: "restarted",
		"provider":       req.Provider,
	}, nil
}
