package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// registerMemoryRPCHandlers registers direct RPC handlers for memory methods
// that previously relied on dead bus proxies. The proxy registered
// memory.vector.search and memory.export as bus request/response pairs, but no
// component ever subscribed to those topics, so every call timed out with
// "timeout waiting for response on memory.result" (and the daemon logged
// "bus: Publish with no subscribers").
//
// These handlers call the MemoryService directly — the same code path as the
// HTTP routes /api/v1/memory/vector/search and /api/v1/memory/export.
// Registered AFTER the proxy so these override the dead proxied versions.
func registerMemoryRPCHandlers(server *rpc.Server, memSvc *services.MemoryService) {
	if server == nil || memSvc == nil {
		return
	}

	// memory.vector.search — semantic/vector similarity search with keyword
	// fallback (Manager.SearchSemantic).
	server.RegisterHandler("memory.vector.search", func(ctx context.Context, params json.RawMessage) (any, error) {
		var req services.VectorSearchRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		results, err := memSvc.VectorSearch(ctx, req)
		if err != nil {
			return nil, err
		}
		return map[string]any{"results": results}, nil
	})

	// memory.export — export all recent memories as JSON bytes. The HTTP
	// route writes raw bytes; over RPC we wrap in a result envelope so the
	// JSON-RPC response stays structured.
	server.RegisterHandler("memory.export", func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			Format   string `json:"format"`
			Category string `json:"category"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("invalid parameters: %w", err)
			}
		}
		if req.Format == "" {
			req.Format = "json"
		}
		data, err := memSvc.Export(ctx, req.Format, req.Category)
		if err != nil {
			return nil, err
		}
		var parsed any
		if err := json.Unmarshal(data, &parsed); err != nil {
			// Non-JSON payload: return as a raw string field.
			return map[string]any{"data": string(data), "format": req.Format}, nil
		}
		return map[string]any{"data": parsed, "format": req.Format}, nil
	})

	// memory.vector.stats — vector shard statistics. Previously a dead bus
	// proxy: the request was published to the bus but nothing subscribed to
	// "memory.result", so `meept memory vector stats` (cmd/meept/memory.go)
	// blocked 10s and then failed with a timeout error. This handler calls
	// MemoryService.VectorStats directly — the same code path as the HTTP
	// route GET /api/v1/memory/vector/stats.
	server.RegisterHandler("memory.vector.stats", func(_ context.Context, _ json.RawMessage) (any, error) {
		stats, err := memSvc.VectorStats()
		if err != nil {
			return nil, err
		}
		return stats, nil
	})
}
