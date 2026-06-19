package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// registerSearchRPCHandlers registers search-related RPC handlers directly on
// the RPC server. Lives in the daemon package to avoid an import cycle
// (rpc → services → scheduler → rpc).
func registerSearchRPCHandlers(server *rpc.Server, searchSvc *services.SearchService) {
	if server == nil || searchSvc == nil {
		return
	}

	// search.semantic - cross-scope semantic search with keyword fallback
	server.RegisterHandler("search.semantic", func(ctx context.Context, params json.RawMessage) (any, error) {
		var req services.SemanticSearchRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		return searchSvc.SearchSemantic(ctx, req)
	})

	// search.keyword - explicit keyword-only search (delegates to Search)
	server.RegisterHandler("search.keyword", func(ctx context.Context, params json.RawMessage) (any, error) {
		var req services.SearchRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		results, err := searchSvc.Search(ctx, req)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"results":   results,
			"count":     len(results),
			"mode":      "keyword",
		}, nil
	})
}
