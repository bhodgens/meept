package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/llm"
)

// RegisterRoutingHandlers exposes routing-log query endpoints on the RPC server.
// If rl is nil the function is a no-op (the daemon may run without a routing log).
func RegisterRoutingHandlers(server *Server, rl *llm.RoutingLogger) {
	if rl == nil {
		return
	}

	// routing.recent — last N decisions (default 20)
	server.RegisterHandler("routing.recent", func(ctx context.Context, params json.RawMessage) (any, error) {
		limit := 20
		if len(params) > 0 {
			var p struct {
				Limit int `json:"limit"`
			}
			if err := json.Unmarshal(params, &p); err == nil && p.Limit > 0 {
				limit = p.Limit
			}
		}
		decisions, err := rl.Recent(ctx, limit)
		if err != nil {
			return nil, fmt.Errorf("routing.recent failed: %w", err)
		}
		return map[string]any{"decisions": decisions, "count": len(decisions)}, nil
	})

	// routing.by_model — decisions filtered by chosen_model_id
	server.RegisterHandler("routing.by_model", func(ctx context.Context, params json.RawMessage) (any, error) {
		var p struct {
			ModelID string `json:"model_id"`
			Limit   int    `json:"limit"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
		}
		if p.ModelID == "" {
			return nil, fmt.Errorf("model_id is required")
		}
		if p.Limit <= 0 {
			p.Limit = 50
		}
		decisions, err := rl.ByModel(ctx, p.ModelID, p.Limit)
		if err != nil {
			return nil, fmt.Errorf("routing.by_model failed: %w", err)
		}
		return map[string]any{"decisions": decisions, "count": len(decisions)}, nil
	})
}
