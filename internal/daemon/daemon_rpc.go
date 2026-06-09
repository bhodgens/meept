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
	daemon  *services.DaemonService
	runtime *services.RuntimeService
}

// NewDaemonRPCHandler creates a new daemon RPC handler.
func NewDaemonRPCHandler(daemon *services.DaemonService) *DaemonRPCHandler {
	return &DaemonRPCHandler{daemon: daemon}
}

// SetRuntimeService sets the optional runtime service for enriching daemon status.
func (h *DaemonRPCHandler) SetRuntimeService(svc *services.RuntimeService) {
	h.runtime = svc
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

	// Enrich with runtime health info if available
	if h.runtime != nil {
		if rtResp, err := h.runtime.Status(ctx); err == nil && len(rtResp.Runtimes) > 0 {
			runtimeInfo := make(map[string]any)
			for _, rs := range rtResp.Runtimes {
				runtimeInfo[rs.ProviderID] = map[string]any{
					"running": rs.Running,
					"healthy": rs.Healthy,
					"pid":     rs.PID,
				}
			}
			status.Runtimes = runtimeInfo
		}
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
