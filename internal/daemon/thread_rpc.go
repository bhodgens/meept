package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// registerThreadRPCHandlers registers thread RPC handlers directly on the
// RPC server, overriding the bus-proxy entries registered by the proxy
// handler. Lives in the daemon package to avoid an import cycle
// (rpc imports services, which imports scheduler, which imports rpc).
func registerThreadRPCHandlers(server *rpc.Server, threadSvc *services.ThreadService) {
	if server == nil || threadSvc == nil {
		return
	}

	// session.thread.new / session.thread.create - create a new thread
	server.RegisterHandler("session.thread.new", handleThreadCreate(threadSvc))
	server.RegisterHandler("session.thread.create", handleThreadCreate(threadSvc))

	// session.thread.list - list all threads for a session
	server.RegisterHandler("session.thread.list", handleThreadList(threadSvc))

	// session.thread.switch - switch to a different thread (alias for set_active)
	server.RegisterHandler("session.thread.switch", handleThreadSetActive(threadSvc))
	server.RegisterHandler("session.thread.set_active", handleThreadSetActive(threadSvc))

	// session.thread.current - get the active thread for a session
	server.RegisterHandler("session.thread.current", handleThreadGetActive(threadSvc))
	server.RegisterHandler("session.thread.get_active", handleThreadGetActive(threadSvc))

	// session.thread.delete - delete a thread
	server.RegisterHandler("session.thread.delete", handleThreadDelete(threadSvc))
}

// handleThreadCreate returns a handler for thread creation.
func handleThreadCreate(svc *services.ThreadService) rpc.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			SessionID      string `json:"session_id"`
			TopicLabel     string `json:"topic_label"`
			ConversationID string `json:"conversation_id,omitempty"`
			Summary        string `json:"summary,omitempty"`
			IsActive       bool   `json:"is_active,omitempty"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if req.SessionID == "" {
			return nil, fmt.Errorf("session_id is required")
		}
		if req.TopicLabel == "" {
			return nil, fmt.Errorf("topic_label is required")
		}

		thread, err := svc.CreateThread(ctx, services.CreateThreadRequest{
			SessionID:      req.SessionID,
			TopicLabel:     req.TopicLabel,
			ConversationID: req.ConversationID,
			Summary:        req.Summary,
			IsActive:       req.IsActive,
		})
		if err != nil {
			return nil, fmt.Errorf("create thread failed: %w", err)
		}

		return thread, nil
	}
}

// handleThreadList returns a handler for listing threads.
func handleThreadList(svc *services.ThreadService) rpc.Handler {
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

		threads, err := svc.ListThreads(ctx, services.ListThreadsRequest{
			SessionID: req.SessionID,
		})
		if err != nil {
			return nil, fmt.Errorf("list threads failed: %w", err)
		}

		return map[string]any{
			"threads": threads,
			"count":   len(threads),
		}, nil
	}
}

// handleThreadSetActive returns a handler for setting the active thread.
func handleThreadSetActive(svc *services.ThreadService) rpc.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			SessionID string `json:"session_id"`
			ThreadID  string `json:"thread_id"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if req.SessionID == "" {
			return nil, fmt.Errorf("session_id is required")
		}
		if req.ThreadID == "" {
			return nil, fmt.Errorf("thread_id is required")
		}

		thread, err := svc.SetActiveThread(ctx, services.SetActiveThreadRequest{
			SessionID: req.SessionID,
			ThreadID:  req.ThreadID,
		})
		if err != nil {
			return nil, fmt.Errorf("set active thread failed: %w", err)
		}

		return thread, nil
	}
}

// handleThreadGetActive returns a handler for getting the active thread.
func handleThreadGetActive(svc *services.ThreadService) rpc.Handler {
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

		thread, err := svc.GetActiveThread(ctx, services.GetActiveThreadRequest{
			SessionID: req.SessionID,
		})
		if err != nil {
			return nil, fmt.Errorf("get active thread failed: %w", err)
		}

		return map[string]any{
			"thread": thread,
		}, nil
	}
}

// handleThreadDelete returns a handler for deleting a thread.
func handleThreadDelete(svc *services.ThreadService) rpc.Handler {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			ThreadID string `json:"thread_id"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		if req.ThreadID == "" {
			return nil, fmt.Errorf("thread_id is required")
		}

		if err := svc.DeleteThread(ctx, services.DeleteThreadRequest{
			ThreadID: req.ThreadID,
		}); err != nil {
			return nil, fmt.Errorf("delete thread failed: %w", err)
		}

		return map[string]any{
			"status": "deleted",
		}, nil
	}
}
