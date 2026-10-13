# L02 — Response-Cache Metrics Store Wiring (P2)

**Proposal:** P2. **Depends on:** none. **Files (2):**
- Modify: `internal/daemon/components.go` (~:756-790 cache construction block)
- Test: `internal/daemon/components_test.go` (add case) or new `internal/daemon/components_cache_metrics_test.go`

**Problem:** Production cache wiring passes a NIL metrics store:
`llm.NewTokenCacheCoordinatorWithMetrics(cacheCfg, nil)` (components.go:770), so
`cache.hit` / `cache.miss` / `cache.eviction` / `cache.entry_count`
(token_cache.go:158-197, l1:353-369, l2:498-505) never reach metrics.db. A
stock deployment cannot measure hit rates — the dashboard that justifies every
other proposal in this tree is dark.

**Contract (root #2/Knob):** no config change; pass the existing metrics store
that components.go already opens (locate it — the audit notes a metrics store
construction around components.go:640). If the store is constructed after the
cache block, reorder construction or add a `SetMetricsStore` call after both
exist (the coordinator already supports attachment via its constructor —
verify whether a setter exists in token_cache.go before adding one).

### Tasks

**Task 1 — failing test:** `TestCacheCoordinatorGetsMetricsStore`: build the
components stack (or the minimal subset) and assert the coordinator's metrics
store is non-nil and that a Put+Get records cache.hit/cache.miss into the
store. Run: `go test -p 2 ./internal/daemon/ -run TestCacheCoordinatorGetsMetricsStore`
— expect FAIL.

**Task 2 — wire:** components.go cache construction: pass the real store
(reorder construction if needed). If a setter must be added, add
`func (c *TokenCacheCoordinator) SetMetricsStore(m metrics.Store)` guarded
nil-safe, call it right after both are built.

**Task 3 — verify end to end:** `go test -p 2 ./internal/daemon/`; then boot
the daemon briefly (local config) and confirm `~/.meept/metrics.db` llm-side
metrics table gains cache.hit/cache.miss rows after two chats with cache
enabled (manual probe; include commands in report).

**Task 4 — report.** Do NOT commit.

## Self-Verification Checklist
- [ ] Metrics store non-nil at coordinator construction or attached immediately after
- [ ] cache.hit/miss/eviction observable in metrics.db in a live probe
- [ ] No behavior change when `[llm.cache] enabled=false` (default)

## Review Checklist (for orchestrator)
- [ ] No nil-store path remains on the stock wiring
- [ ] Reordering (if any) does not move cache construction before its config is loaded
