// Context-warm dispatch (plan 20261113-filler-clips-and-context-warm
// phase 2, root Contract C4, hard rules 1 and 5).
//
// ProviderManager.Warm mirrors ProviderManager.Chat's selection lane
// ONLY: the per-request alias resolution (WithResolvedModel, or a
// caller's WithModelOverride for parity with the real turn) reorders the
// ordered provider list for THIS call, a gated prefix-warm request is
// dispatched to the FIRST provider in that order, and that's all. It
// deliberately does NOT fail over to a second provider on error — the
// warm is a throwaway that exists only when one specific endpoint serves
// the turn. It writes NOTHING: no health counters, no tier signal, no
// quota block, no session-endpoint preference, no llm_calls row, no
// metrics.Store record. Failures are the callee's to classify; the
// daemon closure owns the DEBUG line.

package llm

import (
	"context"
	"fmt"
)

// Warm implements the warmDispatcher seam for the ProviderManager: the
// alias resolution the real turn would ride (WithResolvedModel) gets
// re-derived for THIS call, the request lands on the provider that
// would serve the alias, and Warm on that provider's client fire-and-
// forgets. No providers available for the selection -> a DEBUG-typed
// error (never surfaced; the caller logs at DEBUG).
func (pm *ProviderManager) Warm(ctx context.Context, messages []ChatMessage, opts ...ChatOption) error {
	if pm == nil {
		return fmt.Errorf("llm: ProviderManager is nil")
	}
	pm.mu.RLock()
	if !pm.initialized || len(pm.providers) == 0 {
		pm.mu.RUnlock()
		return fmt.Errorf("llm: provider manager not initialized or no providers configured")
	}
	ordered := pm.orderedProvidersForRequest(opts)
	pm.mu.RUnlock()

	for _, entry := range ordered {
		if entry == nil || entry.Config == nil || entry.Chatter == nil {
			continue
		}
		if entry.Health != nil && entry.Health.Status == ProviderStatusDisabled {
			continue
		}
		if warm, ok := entry.Chatter.(WarmChatter); ok {
			return warm.Warm(ctx, messages, opts...)
		}
		// A class with no Warm (custom Chatter implementations) is a
		// fail-soft skip for THIS provider; the daemon falls through the
		// remaining ordered list.
		continue
	}
	return fmt.Errorf("llm: warm skipped — no warm-capable provider in the ordered list")
}
