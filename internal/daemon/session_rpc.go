package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// registerSessionRPCHandlers registers session RPC handlers directly on the
// RPC server. Handles session designation queries used by the CLI
// (`meept sessions --needs-attention`) and the menubar app.
func registerSessionRPCHandlers(server *rpc.Server, sessionSvc *services.SessionService) {
	if server == nil || sessionSvc == nil {
		return
	}

	// sessions.designated - list sessions that require attention
	server.RegisterHandler("sessions.designated", handleSessionsDesignated(sessionSvc))

	// sessions.designated_acknowledge - acknowledge a designated session
	server.RegisterHandler("sessions.designated_acknowledge", handleSessionDesignatedAcknowledge(sessionSvc))

	// sessions.archive - set or clear the archived flag on a session
	server.RegisterHandler("sessions.archive", handleSessionArchive(sessionSvc))
}

// handleSessionArchive sets or clears the archived flag on a session.
// Mirrors handleSessionDesignatedAcknowledge's closure pattern.
func handleSessionArchive(svc *services.SessionService) rpc.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ID       string `json:"id"`
			Archived bool   `json:"archived"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if req.ID == "" {
			return nil, fmt.Errorf("id is required")
		}

		if err := svc.ArchiveSession(ctx, services.ArchiveSessionRequest{ID: req.ID, Archived: req.Archived}); err != nil {
			return nil, fmt.Errorf("failed to archive session: %w", err)
		}

		status := "archived"
		if !req.Archived {
			status = "unarchived"
		}
		return map[string]any{
			"status": status,
			"id":     req.ID,
		}, nil
	}
}

// handleSessionsDesignated returns sessions whose designation is non-trivial.
func handleSessionsDesignated(svc *services.SessionService) rpc.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		count, sessions, err := svc.GetDesignated(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get designated sessions: %w", err)
		}

		return map[string]any{
			"designated_count": count,
			"sessions":         sessions,
		}, nil
	}
}

// handleSessionDesignatedAcknowledge acknowledges a designated session,
// clearing its designation status.
func handleSessionDesignatedAcknowledge(svc *services.SessionService) rpc.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if req.SessionID == "" {
			return nil, fmt.Errorf("session_id is required")
		}

		if err := svc.AcknowledgeDesignation(ctx, req.SessionID); err != nil {
			return nil, fmt.Errorf("failed to acknowledge designation: %w", err)
		}

		return map[string]any{
			"status":  "acknowledged",
			"session": req.SessionID,
			"at":      time.Now().Format(time.RFC3339),
		}, nil
	}
}
