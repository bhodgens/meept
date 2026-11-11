package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/vector"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// vectorHybridWiring is the ShardManager-powered hybrid search surface (plan
// docs/plans/2026-06-07-sharded-vector-memory.md: "Needed: Wire ShardManager
// for hybrid search in memory backends"). The wired foundation — the live
// ShardManager, its ShardManagerVectorSearcher adapter (the manager's
// vectorStore), and the embedder — is built by components.go; this file
// composes the vector.HybridSearcher over it and registers the
// memory.search.hybrid RPC handler. Everything is gated on
// [memory.vector_hybrid].enabled (opt-in, default false): when disabled the
// legacy Manager.SearchHybrid path stays byte-identical and nothing here is
// constructed.
type vectorHybridWiring struct {
	searcher *vector.HybridSearcher
}

// newVectorHybridWiring builds the hybrid searcher over the manager's wired
// ShardManager vector adapter. It returns (nil, nil) — not an error — when
// the manager has no vector store (hybrid search needs the vector lane; the
// caller logs and continues with the legacy path). Alpha outside [0,1]
// resolves to the searcher's 0.5 default.
func newVectorHybridWiring(mgr *memory.Manager, alpha float64, multiUser bool, logger *slog.Logger) (*vectorHybridWiring, error) {
	if mgr == nil {
		return nil, fmt.Errorf("memory manager is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	adapter, ok := mgr.GetVectorSearcher().(*memory.ShardManagerVectorSearcher)
	if !ok || adapter == nil {
		return nil, fmt.Errorf("memory manager has no ShardManager vector searcher (enable memory.embeddings)")
	}
	searcher := vector.NewVectorSearcherAdapter(adapter.Manager(), nil)

	hybrid, err := vector.NewHybridSearcher(vector.HybridSearcherConfig{
		VectorSearcher:  searcher,
		KeywordSearcher: &managerKeywordSearcher{mgr: mgr},
		Alpha:           alpha,
		// bughunt C35: thread the USER-purview C2 predicate through BOTH
		// lanes (the same rule registerMemoryRPCHandlers pins for this
		// socket; f5eb3e03 purview mapping: global caller identity ->
		// user purview). The multiuser bit (Flag 3 fail-closed: multiuser
		// on + empty userID hides user rows) comes from
		// cfg.MultiUser.Enabled at the construction site.
		Scope: memoryRowScopeFilter("", "", "", multiUser),
	})
	if err != nil {
		return nil, fmt.Errorf("create hybrid searcher: %w", err)
	}
	logger.Info("vector hybrid search wired",
		"alpha", hybrid.GetAlpha(),
	)
	return &vectorHybridWiring{searcher: hybrid}, nil
}

// memoryRowScopeFilter builds the vector.ScopeFilter over
// memory.VisibleScopeRow — the committed C2 predicate (scope_filter.go),
// applied to raw row metadata. Purview mapping (f5eb3e03): a GLOBAL
// caller identity maps to USER purview (the filter hides project rows
// from a global caller that the C2 contract would otherwise show to the
// chief-of-staff — matching scopeSearchOptions in internal/tools/builtin).
func memoryRowScopeFilter(maxScope, projectID, userID string, multiUser bool) vector.ScopeFilter {
	filterScope := memory.Scope(maxScope)
	if filterScope == "" {
		// Unscoped callers (the RPC socket carries no session identity)
		// get the socket's pinned purview: USER. An empty maxScope with
		// WithScopeFilter(USER, "", "") is exactly the predicate
		// registerMemoryRPCHandlers documents for this boundary.
		filterScope = memory.ScopeUser
	}
	if filterScope == memory.ScopeGlobal {
		filterScope = memory.ScopeUser
	}
	return func(meta map[string]any) bool {
		return memory.VisibleScopeRow(meta, filterScope, projectID, userID, multiUser)
	}
}

// managerKeywordSearcher adapts Manager.Search (FTS5 keyword lane) to the
// vector.KeywordSearcher seam. internal/memory cannot import
// internal/memory/vector (the vector package is imported by memory), so the
// adapter lives on the daemon side of the seam.
type managerKeywordSearcher struct {
	mgr *memory.Manager
}

// SearchKeyword implements vector.KeywordSearcher.
func (k *managerKeywordSearcher) SearchKeyword(ctx context.Context, query string, limit int) ([]vector.KeywordResult, error) {
	if limit <= 0 {
		limit = 10
	}
	results, err := k.mgr.Search(ctx, memory.MemoryQuery{
		Query:       query,
		Limit:       limit,
		MatchAny:    true,
		PrefixTerms: true,
	})
	if err != nil {
		return nil, err
	}
	out := make([]vector.KeywordResult, 0, len(results))
	for _, r := range results {
		out = append(out, vector.KeywordResult{
			ID:             r.Memory.ID,
			Content:        r.Memory.Content,
			Metadata:       r.Memory.Metadata,
			RelevanceScore: r.RelevanceScore,
		})
	}
	return out, nil
}

// registerVectorHybridRPCHandler registers memory.search.hybrid, the RPC
// surface the plan's vector-search seam exposes over the hybrid searcher.
// wiring nil (vector_hybrid disabled) = no handler registered; calls fail
// with "method not found" rather than pretending the feature exists.
//
// bughunt D11/C35: the request limit is clamped to vector.MaxSearchLimit
// (an unbounded limit meant an unbounded FTS5 scan + embedding fan-out
// per call) and the searcher carries the caller's scope filter —
// the same USER-purview C2 predicate registerMemoryRPCHandlers pins for
// this socket (owner-trusted: project rows in purview, user rows match
// the empty owner identity when multiuser is off, multiuser-on +
// unresolved identity fails closed on user rows, global rows always
// visible). Purview mapping per f5eb3e03/scope_wiring: a GLOBAL caller
// identity maps to USER purview. The predicate is armed at construction
// (newVectorHybridWiring's multiUser bit).
func registerVectorHybridRPCHandler(server *rpc.Server, wiring *vectorHybridWiring, defaultLimit int) {
	if server == nil || wiring == nil || wiring.searcher == nil {
		return
	}
	if defaultLimit <= 0 {
		defaultLimit = 10
	}
	server.RegisterHandler("memory.search.hybrid", func(ctx context.Context, params json.RawMessage) (any, error) {
		var req services.VectorSearchRequest
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("invalid parameters: %w", err)
			}
		}
		if req.Query == "" {
			return nil, fmt.Errorf("query is required")
		}
		// D11: clamp — never forward the raw request limit to the
		// lanes (the searcher re-clamps too; this side clamps with
		// the same rule, matching the sibling surfaces'
		// clamp-and-serve posture).
		limit := vector.ClampLimit(req.Limit, defaultLimit)
		results, err := wiring.searcher.Search(ctx, req.Query, limit)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(results))
		for _, r := range results {
			out = append(out, map[string]any{
				"memory_id":      r.MemoryID,
				"content":        r.Content,
				"metadata":       r.Metadata,
				"keyword_score":  r.KeywordScore,
				"vector_score":   r.VectorScore,
				"combined_score": r.CombinedScore,
			})
		}
		return map[string]any{"results": out}, nil
	})
}
