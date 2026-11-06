package daemon

// Semantic cache tier daemon wiring (llm-serving-tiers leaf 02 T5,
// wiring-followups-final leaf 05, docs/plans/20261103-wiring-followups-
// final/05-semantic-cache-daemon-wiring.md).
//
// components.go builds the LLM client with the ONE shared embedding
// provider reused from the memory stack (never a second construction --
// the provider is hoisted above the client build so both consumers share
// it) or, when llm.cache.semantic.embedder_alias is set, resolved through
// the alias/capability machinery exactly like every auxiliary client
// (Resolver.ResolveForAlias first, then a direct provider/model-id ref).
//
// Degradation posture (identical to the token-cache posture): embedder
// resolution failure or a missing embedder logs a Warn and the client
// stays exact-only -- the tier is never armed and startup is NEVER
// blocked. Disabled (the opt-in default) means this code path returns nil
// before touching any embedder: byte-identical client behavior.

import (
	"context"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory/vector"
)

// semanticVectorEmbedder adapts the memory-stack vector.Provider onto
// the llm.Embedder seam (Contract C5: GenerateEmbedding(ctx, text) ->
// Embed(ctx, text)). One adapter type serves both resolution paths
// because the alias path also lands on a vector.Provider (the bundled
// OpenAI-compatible embeddings client) -- there is no second embedding
// path anywhere in the daemon (the "reuse, never fork" rule).
type semanticVectorEmbedder struct {
	provider vector.Provider
}

// Embed generates the prompt vector via the shared provider.
func (e semanticVectorEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return e.provider.GenerateEmbedding(ctx, text)
}

// resolveSemanticEmbedder picks the tier's embedder per the schema doc
// (docs/configuration order: alias > memory stack > none):
//   - sc.EmbedderAlias non-empty: resolve through the alias/capability
//     machinery (the same path createAuxiliaryLLMClientWithResolver uses:
//     Resolver.ResolveForAlias, then a direct provider/model-id ref).
//     The resolved endpoint's BaseURL/APIKey/ModelID are handed to the
//     bundled vector.OpenAIProvider (it speaks /embeddings against any
//     OpenAI-compatible endpoint). Resolution failure does NOT fall
//     through to the memory stack: Warn + exact-only per the leaf doc.
//   - Alias empty: reuse the memory-stack provider (the hoisted
//     `embedder` handle in components.go). nil embedder -> exact-only.
//
// Returns the embedder plus a human-readable source label for the Info
// line, or (nil, "") when nothing resolves.
func resolveSemanticEmbedder(sc config.SemanticCacheConfig, memoryEmbedder vector.Provider, logger *slog.Logger) (llm.Embedder, string) {
	if alias := sc.EmbedderAlias; alias != "" {
		providersCfg, err := llm.LoadProvidersConfigDefault()
		if err != nil {
			logger.Warn("semantic cache disabled: providers config unavailable for embedder_alias", "alias", alias, "error", err)
			return nil, ""
		}
		resolver := llm.NewResolver(providersCfg, nil)
		mc, aliasErr := resolver.ResolveForAlias(alias, "")
		if aliasErr != nil {
			// Not an alias name: accept a direct "provider/model-id" ref.
			mc = resolver.ResolveRef(alias)
		}
		if mc == nil || mc.BaseURL == "" || mc.ModelID == "" {
			logger.Warn("semantic cache disabled: no embedder", "alias", alias, "error", aliasErr)
			return nil, ""
		}
		// Reuse the ONE embeddings client shape (vector.OpenAIProvider)
		// against the resolved endpoint. Typed-nil safe: NewOpenAIProvider
		// always returns a non-nil concrete provider.
		return semanticVectorEmbedder{provider: vector.NewOpenAIProvider(vector.OpenAIProviderConfig{
			APIKey:  mc.APIKey,
			BaseURL: mc.BaseURL,
			Model:   mc.ModelID,
			Logger:  logger,
		})}, "alias:" + alias
	}

	if memoryEmbedder != nil {
		return semanticVectorEmbedder{provider: memoryEmbedder}, "memory-stack"
	}

	logger.Warn("semantic cache disabled: no embedder")
	return nil, ""
}

// semanticCacheConfigFromSchema maps config.SemanticCacheConfig onto
// llm.SemanticCacheConfig field-for-field (leaf doc T2): Threshold,
// MaxEntries and VerifyFloor pass through (the llm constructor re-clamps
// defensively; the load boundary already normalized at config load);
// TTLMin converts to a duration, with zero mirroring
// llm.cache.default_ttl_min exactly like the exact-key tier (a mirrored
// zero leaves the llm-side DefaultSemanticTTL fallback in place).
// PartitionKey is deliberately unset: the client derives the partition
// from the request's effective model id (semanticPartitionFor), so a
// fixed stamp here would only over-scope the store.
func semanticCacheConfigFromSchema(sc config.SemanticCacheConfig, defaultTTLMin int) llm.SemanticCacheConfig {
	out := llm.SemanticCacheConfig{
		Enabled:     sc.Enabled,
		Threshold:   sc.Threshold,
		MaxEntries:  sc.MaxEntries,
		VerifyFloor: sc.VerifyFloor,
	}
	ttlMin := sc.TTLMin
	if ttlMin == 0 {
		ttlMin = defaultTTLMin
	}
	if ttlMin > 0 {
		out.TTL = time.Duration(ttlMin) * time.Minute
	}
	return out
}

// buildSemanticCacheTier is the ONE construction site for the semantic
// tier (called from components.go where clientOpts are assembled).
// Returns the cache to attach via llm.WithSemanticCache, or nil when the
// feature is disabled or degrades -- nil is the exact-only posture and
// WithSemanticCache ignores it byte-identically (leaf 02 T5: "a
// nil/disabled cache must leave the client behavior identical").
//
// Embedder resolution reuses the hoisted memory-stack provider (the same
// handle the vector-shard/memory-manager blocks below consume) or an
// alias-resolved provider -- never a duplicate construction.
func buildSemanticCacheTier(cfg *config.Config, memoryEmbedder vector.Provider, logger *slog.Logger) *llm.SemanticCache {
	sc := cfg.LLM.Cache.Semantic
	if !sc.Enabled {
		// Opt-in default false: zero behavior, no embedder call, no log
		// (byte-identical to pre-leaf boot).
		return nil
	}
	embedder, source := resolveSemanticEmbedder(sc, memoryEmbedder, logger)
	if embedder == nil {
		// Warn already emitted inside resolveSemanticEmbedder. Degraded
		// posture: no tier, the client stays exact-only.
		return nil
	}
	llmCfg := semanticCacheConfigFromSchema(sc, cfg.LLM.Cache.DefaultTTLMin)
	cache := llm.NewSemanticCache(embedder, llmCfg)
	logger.Info("Semantic cache initialized",
		"threshold", llmCfg.Threshold,
		"verify_floor", llmCfg.VerifyFloor,
		"max_entries", llmCfg.MaxEntries,
		"ttl", llmCfg.TTL,
		"embedder_source", source,
	)
	return cache
}
