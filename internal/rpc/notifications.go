package rpc

import (
	"context"
	"encoding/json"
	"fmt"
)

// DNDController is the minimal interface the notifications RPC handler needs
// from the daemon's EventEmitter so this file does not import the daemon
// package (which would create an import cycle).
type DNDController interface {
	SetDoNotDisturb(dnd bool)
	IsDoNotDisturb() bool
}

// NotificationsHandler provides notification RPC methods for runtime control
// of daemon-side notification suppression (Do Not Disturb). CLI, TUI, and
// HTTP clients can all reach it through the RPC server.
type NotificationsHandler struct {
	emitter DNDController
}

// NewNotificationsHandler creates a new handler. If emitter is nil the
// registered methods return "notifications not available" errors.
func NewNotificationsHandler(emitter DNDController) *NotificationsHandler {
	return &NotificationsHandler{emitter: emitter}
}

// RegisterNotificationsHandlers registers the notifications.* RPC methods.
func (h *NotificationsHandler) RegisterNotificationsHandlers(server *Server) {
	server.RegisterHandler("notifications.set_dnd", h.handleSetDND)
	server.RegisterHandler("notifications.get_dnd", h.handleGetDND)
}

type setDNDParams struct {
	Enabled bool `json:"enabled"`
}

// handleSetDND sets daemon-side Do Not Disturb suppression.
func (h *NotificationsHandler) handleSetDND(ctx context.Context, params json.RawMessage) (any, error) {
	if h.emitter == nil {
		return nil, fmt.Errorf("notifications not available")
	}
	var p setDNDParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("invalid params: %w", err)
		}
	}
	h.emitter.SetDoNotDisturb(p.Enabled)
	return map[string]any{"enabled": h.emitter.IsDoNotDisturb()}, nil
}

// handleGetDND returns the current daemon-side DND state.
func (h *NotificationsHandler) handleGetDND(ctx context.Context, params json.RawMessage) (any, error) {
	if h.emitter == nil {
		return nil, fmt.Errorf("notifications not available")
	}
	return map[string]any{"enabled": h.emitter.IsDoNotDisturb()}, nil
}
