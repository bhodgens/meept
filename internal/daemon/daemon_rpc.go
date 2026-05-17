package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// DaemonRPCHandler handles daemon RPC methods.
type DaemonRPCHandler struct {
	daemon *services.DaemonService
}

// NewDaemonRPCHandler creates a new daemon RPC handler.
func NewDaemonRPCHandler(daemon *services.DaemonService) *DaemonRPCHandler {
	return &DaemonRPCHandler{daemon: daemon}
}

// RegisterDaemonMethods registers all daemon RPC methods.
func (h *DaemonRPCHandler) RegisterDaemonMethods(server *rpc.Server) {
	server.RegisterHandler("daemon.status", h.handleStatus)
	server.RegisterHandler("daemon.start", h.handleStart)
	server.RegisterHandler("daemon.stop", h.handleStop)
	server.RegisterHandler("daemon.restart", h.handleRestart)
}

// handleStatus returns daemon status.
func (h *DaemonRPCHandler) handleStatus(ctx context.Context, params json.RawMessage) (any, error) {
	if h.daemon == nil {
		return nil, fmt.Errorf("daemon service not available")
	}

	status, err := h.daemon.Status(ctx)
	if err != nil {
		return nil, err
	}

	return status, nil
}

// handleStart starts the daemon.
func (h *DaemonRPCHandler) handleStart(ctx context.Context, params json.RawMessage) (any, error) {
	if h.daemon == nil {
		return nil, fmt.Errorf("daemon service not available")
	}

	if err := h.daemon.Start(ctx); err != nil {
		return nil, err
	}

	return map[string]string{"status": "started"}, nil
}

// handleStop stops the daemon.
func (h *DaemonRPCHandler) handleStop(ctx context.Context, params json.RawMessage) (any, error) {
	if h.daemon == nil {
		return nil, fmt.Errorf("daemon service not available")
	}

	if err := h.daemon.Stop(ctx); err != nil {
		return nil, err
	}

	return map[string]string{"status": "stopped"}, nil
}

// handleRestart restarts the daemon.
func (h *DaemonRPCHandler) handleRestart(ctx context.Context, params json.RawMessage) (any, error) {
	if h.daemon == nil {
		return nil, fmt.Errorf("daemon service not available")
	}

	if err := h.daemon.Restart(ctx); err != nil {
		return nil, err
	}

	return map[string]string{"status": "restarted"}, nil
}
