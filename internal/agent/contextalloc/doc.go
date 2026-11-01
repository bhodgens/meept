// Package contextalloc implements the context-memory-engine allocator core
// (plan tree 20261031-context-memory-engine, leaf
// 01-context-assembler/01-allocator-core.md).
//
// # Purpose
//
// Meept budgets context today with scattered static constants
// (IterationTokenBudget=30000, ToolResultMaxTokens=3000,
// DefaultConversationTokenBudget=50000 in internal/agent/loop.go, plus
// MaxMemoryContextTokens=2000 in internal/agent/conversation.go). None of
// them derive from the model's real context window. This package replaces
// them with ONE deterministic window-fraction split when discovery resolves
// a real context_limit; the statics stay untouched as fallbacks.
//
// # Contracts
//
// This package defines the shared master contracts:
//
//   - C1 (tier.go): TierName / RecallFunc / TierBudget /
//     TierBudgetProvider. Branch B's memory stores feed the provider seam;
//     the adapter is authored in leaf 03-integration. nil (or typed-nil)
//     provider = undivided memory, exactly today's behavior.
//   - C2 (allocator.go): the Allocation struct, whose five section fields
//     always sum EXACTLY to WindowTokens.
//   - C3 is the e2e suite mapping owned by leaf 04-e2e.
//
// # Findings basis
//
// Every default cites docs/plans/20261031-context-memory-engine/
// 01-context-assembler/findings-context-budgeting.md:
//
//   - R-A2: output reserve = max(5% fraction, absolute floor 4096) --
//     never shrinks proportionally on small windows (R-A10 keeps it
//     absolute).
//   - R-A3: ~3% tokenizer-disagreement margin, folded into the default
//     reserve fraction (0.09 total) because the C2 contract pins the
//     Allocation shape with no separate margin field.
//   - R-A4: fraction shape (small fixed sections; docs and memory capped;
//     history flexes; memory tiers capped at ~5% with a 512-token floor
//     analogue at the section level).
//   - R-A5: floors before the proportional split; overflow reclaimed from
//     the lowest-priority section; system + user turn never reduced.
//   - R-A7: the allocator never mutates stable sections mid-session; the
//     AssembleOrdered/stablePrefixHash primitive (internal/agent/prompt.go)
//     stays the single source of prefix stability.
//   - R-A10: the prior-art 0.7 small-model factor is NOT silently copied
//     into the allocator: it scaled the reserve too, violating R-A2.
//     Flexible-section scaling stays proportional by construction.
//
// # Purity
//
// The allocator is pure: no logging, no bus, no config reads, no wall
// clock, no randomness, no map-order float accumulation. Identical inputs
// produce a bit-identical Allocation. Memory-tier splits canonicalize the
// provider's slice by tier name before splitting, so C1's "slice order is
// insignificant" holds bit-exactly.
package contextalloc
