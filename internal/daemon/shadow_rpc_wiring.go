package daemon

// shadow_rpc_wiring.go registers the read-only llm.route.shadow RPC
// (Contract C6, sovereign-router-parity leaf 04, R4) on the daemon's RPC
// server. Pattern: registerIntakeStatusRPC (a nil-safe wiring helper called
// from daemon Run before the listeners serve) — but the seam closes over
// the LIVE *llm.Resolver, because the shadow decision must reflect resolver
// state at call time (health filters, sticky simulation), not a stale
// snapshot.
//
// Read-only posture: the closure calls ONLY ResolveShadow, which takes
// r.mu.RLock and writes nothing — no llm_calls row, no routing_decisions
// row, no budget consumption, no alias-health or sticky-pin mutation.
// Owner-trusted transport (Unix socket, no token auth), identical to
// intake.status. Always registers: an unwired resolver answers the disabled
// envelope, never "method not found".

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/rpc"
)

// registerShadowRouteRPC registers llm.route.shadow on the RPC server.
// Nil-safe at every layer.
func registerShadowRouteRPC(c *Components, server *rpc.Server, logger *slog.Logger) {
	if server == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	var seam rpc.ShadowRouteSeam
	if c != nil && c.LLMResolver != nil {
		resolver := c.LLMResolver
		seam = func(ctx context.Context, req rpc.ShadowRouteRequest) (*rpc.ShadowRouteView, error) {
			return shadowRouteView(resolver, ctx, req), nil
		}
	}

	handler := rpc.NewShadowRouteHandler(seam)
	handler.RegisterShadowRouteMethods(server)
	if seam != nil {
		logger.Info("llm.route.shadow RPC handler registered")
	} else {
		logger.Info("llm.route.shadow RPC handler registered (resolver not wired: disabled envelope)")
	}
}

// shadowRouteView bridges the wire request onto the resolver's read-only
// ResolveShadow and maps the typed decision onto the frozen view. The
// request's model_ref (a "provider/model" ref) takes precedence over the
// alias name, mirroring ShadowRequest.Alias's dual role. Prompt text rides
// as the single user message for the token estimate; it is never logged.
func shadowRouteView(resolver *llm.Resolver, ctx context.Context, req rpc.ShadowRouteRequest) *rpc.ShadowRouteView {
	shadowReq := llm.ShadowRequest{
		Alias: req.ModelRef,
	}
	if shadowReq.Alias == "" {
		shadowReq.Alias = req.Alias
	}
	if req.Prompt != "" {
		shadowReq.Messages = []llm.ChatMessage{{Role: llm.RoleUser, Content: req.Prompt}}
	}

	dec, err := resolver.ResolveShadow(ctx, shadowReq)
	if err != nil {
		// A legitimately-unroutable request never errors out of
		// ResolveShadow; the error here is a caller mistake (unknown
		// alias/ref name). Report the named refusal in the frozen view
		// rather than an RPC error so the envelope shape stays stable.
		return &rpc.ShadowRouteView{
			Alias:      req.Alias,
			Reason:     "shadow_unknown_alias",
			Candidates: []rpc.ShadowCandidateView{},
		}
	}
	return rpc.ShadowRouteViewFromDecision(dec)
}
