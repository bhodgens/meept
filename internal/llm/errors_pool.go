package llm

import "errors"

// ErrAllPoolsBlocked is returned by ResolveForAlias when every candidate
// model in the alias is blocked at POOL scope (Contract C5, leaf 06 T6).
// It is DISTINCT from ErrAllModelsQuotaBlocked and ErrAllEndpointsBlocked
// (the failure families have different horizons and clearing rules) —
// check with errors.Is, never string matching.
var ErrAllPoolsBlocked = errors.New("llm: every candidate pool is blocked")
