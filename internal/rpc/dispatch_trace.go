package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/metrics"
)

// RegisterDispatchHandlers exposes dispatch-log query endpoints on the RPC
// server. If store is nil the function is a no-op (the daemon may run without
// a metrics store).
func RegisterDispatchHandlers(server *Server, store *metrics.Store) {
	if store == nil {
		return
	}

	// session.dispatch_trace — routing decisions for a specific session
	server.RegisterHandler("session.dispatch_trace", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			SessionID string `json:"session_id"`
			Limit     int    `json:"limit"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
		}
		if p.SessionID == "" {
			return nil, fmt.Errorf("session_id is required")
		}
		if p.Limit <= 0 {
			p.Limit = 50
		}
		entries, err := store.QueryDispatchLogBySession(p.SessionID, p.Limit)
		if err != nil {
			return nil, fmt.Errorf("session.dispatch_trace failed: %w", err)
		}
		return map[string]any{"entries": entries, "count": len(entries)}, nil
	})
}
