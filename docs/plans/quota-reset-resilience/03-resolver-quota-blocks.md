# Resolver quota blocking - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Per-credential-key quota blocking on the `Resolver` (the
  production alias rotation component), ProviderManager health checks, and
  the exported block-status readers. The broker gets a stub (no-op) so
  existing callers don't break, but real blocking lives on Resolver.
- **Dependencies:** 01 (QuotaResetError, QuotaCredentialKey), 02 (config)
- **Estimated Context:** 75K
- **Concurrency Group:** B

## Goal

When a provider credential hits a quota window, the Resolver blocks that
credential across all aliases that use it, rotates away from blocked
candidates within `ResolveForAlias`, and clears blocks after the reset time
via health-check probes. Two-level blocking:
- **Per-entry**: a specific `provider/model` is in cooldown (existing
  AliasHealth.CooldownUntil; we extend with quota-aware blocks).
- **Per-credential**: if models A and B share one key (e.g. two OpenRouter
  models on the same key), blocking A blocks B too.

The ProviderManager gets the same semantics for direct-provider aliases.

## Context

Production wiring (`internal/daemon/components.go:747-776`): when >1
provider is configured, `NewProviderManager` is used; when 1 provider,
`c.LLMClient` (single `llm.Client`) is used; the agent loop calls
`l.resolver.ResolveForAlias` / `RotateToNextModel` / `RecordAliasFailure`
/ `RecordAliasSuccess` (`loop.go:4219-4295`). Resolver state:
- `aliases map[string]*AliasEntry` keyed by alias name, each holding
  `Models []*ModelConfig`, `Timeout`, `MaxFails`.
- `health map[string]*AliasHealth` keyed by alias name, holding
  `CurrentIndex`, `ConsecutiveFails`, `CooldownUntil`, `LastFailure`.
- `hasHealthyModels` at `resolver.go:454` considers non-current models
  always available (so quota-blocked candidates must override this rule).

The agent loop at `loop.go:4226` already has a `RateLimitError` branch
calling `RecordAliasFailure` → `RotateToNextModel` → `SwitchModel` → retry.
Quota errors need their own branch BEFORE this handler (otherwise they
enter the existing 30s/60s/... backoff).

Key files:
- `internal/llm/resolver.go` — alias rotation, health tracking,
  `ResolveForAlias`, `RotateToNextModel`, `HasHealthyModels`.
- `internal/llm/provider_manager.go` — the multi-provider failover path
  (`runHealthCheck` at ~line 877, error switch at ~line 263).
- `internal/agent/loop.go:4226-4295` — the agent-loop rate-limit handler.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// File: internal/llm/resolver.go (modifications)

// AliasHealth extended with quota blocks:
type AliasHealth struct {
    CurrentIndex     int
    ConsecutiveFails int
    CooldownUntil    time.Time
    LastFailure      time.Time
    // NEW per-entry blocks: entry -> until (key = "provider/model")
    entryBlocks      map[string]time.Time
    // NEW per-credential block (shared pool): credentialKey -> until
    credentialBlock  map[string]time.Time
}

// On QuotaResetError during ResolveForAlias or RotateToNextModel:
//   - compute wait = min(resetHorizon, cfg.MaxWait) or cfg.DefaultEstimate
//   - set entryBlocks[entryKey] = now+wait AND credentialBlocks[key] = now+wait
//   - the entry is excluded from candidate selection in ResolveForAlias
//     (affects HasHealthyModels and ResolveForAlias candidate scan)
//   - log Warn with provider/model/key/unblock_at (NO raw key material)
//   - continue rotation (do NOT call RecordAliasFailure — quota is not
//     a health failure; the alias cooldown is orthogonal)
//
// Provider-key identity uses QuotaCredentialKey from leaf 01.
//
// On success after rotating past a quota-blocked entry:
//   - clear the entry's block AND the credential block (if this was the
//     last blocked entry under that key) — OR lazily: leave the block,
//     let the next probe reset it (lazy-clear is simpler; do lazy).
//
// New public methods:
func (r *Resolver) QuotaBlockedUntil(credentialKey string) time.Time
func (r *Resolver) ActiveQuotaBlocks() []QuotaBlockStatus

// QuotaBlockStatus is a value-type status record for UI/status reporting.
type QuotaBlockStatus struct {
    AliasName     string
    ProviderID    string
    ModelID       string
    CredentialKey string
    BlockedUntil  time.Time
}

// Health-check probe integration (in runHealthCheck or equivalent):
//   providers whose entryBlocks or credentialBlocks expired are probe
//   candidates; a probe returning QuotaResetError re-blocks with a fresh
//   estimate; a probe succeeding clears the expired block.
```

### What This Leaf Consumes

```
// From 01: QuotaResetError, IsQuotaResetError, AsQuotaResetError,
//          QuotaCredentialKey(cfg *ModelConfig) string
// From 02: config.QuotaRetryConfig (Enabled, MaxWait, DefaultEstimate)
//          reach via the Resolver config (add the field; wire from daemon
//          in leaf 06 — zero-value-safe behavior required here)
```

## Tasks

### Task 1: Quota-block state on AliasHealth + skip logic

**Objective:** Blocked entries/candidates are excluded from rotation.

**Files:**
- Modify: `internal/llm/resolver.go` (AliasHealth extension + candidate
  exclusion)
- Test: `internal/llm/resolver_quota_test.go`

**Step 1: Write failing test**

Two-alias setup: alias A has models m1 (provider P1), m2 (provider P2).
Simulate quota error on P1/m1: (a) `ResolveForAlias("A")` returns m2
(skips blocked m1); (b) `HasHealthyModels("A") == true`; (c)
`ActiveQuotaBlocks()` reports one block with credential key and horizon;
(d) after expiry + successful Chat on m1, block clears.

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestResolverQuotaBlock -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add `entryBlocks` + `credentialBlock` maps to AliasHealth (sync.Once or
lazy-init). Extend candidate scan in `ResolveForAlias` and
`HasHealthyModels` to skip blocked entries. Compute wait per contract.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestResolverQuotaBlock -v`
Expected: PASS

### Task 2: Shared-pool credential blocking

**Objective:** Two models under one credential block together.

**Files:**
- Modify: `internal/llm/resolver.go`
- Test: same test file

**Step 1: Write failing test**

Alias has m1 (provider P1, key K) and m2 (provider P1, same key K).
Quota on m1 → block credential key K → `ResolveForAlias` fails with
"all candidates blocked" (both m1 and m2 excluded). m2 is NOT tried.

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestResolverQuotaSharedPool -v`
Expected: FAIL

**Step 3: Write minimal implementation**

credentialBlocks checked before entry-level; if credential blocked, skip
all entries under that key.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run TestResolverQuotaSharedPool -v`
Expected: PASS

### Task 3: Exported status readers

**Objective:** `QuotaBlockedUntil` + `ActiveQuotaBlocks` for UX/status.

**Files:**
- Modify: `internal/llm/resolver.go`
- Test: same test file

**Step 1: Write failing test**

After a block: `QuotaBlockedUntil(key)` returns the horizon;
`ActiveQuotaBlocks()` returns statuses for each alias+model combo still
blocked; RLock-safe concurrent access; no key material in log output.

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestResolverQuotaStatus -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Straightforward readers under the existing mutex.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run 'TestResolverQuota' -count=1 -v`
Also: `go test -race ./internal/llm/ -run TestResolverQuota -count=1`

### Task 4: ProviderManager probe integration

**Objective:** Expired blocks get probed; failed probes re-block.

**Files:**
- Modify: `internal/llm/provider_manager.go` (error switch ~263-310,
  `runHealthCheck` ~877+)
- Test: `internal/llm/provider_manager_quota_test.go`

**Step 1: Write failing test**

ProviderManager with two entries (same shape as Task 1): quota on entry 1
-> resolve skips it; entry 2 succeeds. Probe tick after expiry: entry 1
probe succeeds -> block clears; probe fails again -> re-block. Quota
errors must NOT call `recordFailure` (alias health stays Healthy).

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run TestProviderManagerQuota -v`
Expected: FAIL

**Step 3: Write minimal implementation**

Add a `case IsQuotaResetError(err):` branch in the error switch (before
`isAuthError`). Reuse the same wait-computation helper (exported unexported
func in resolver.go or a shared helper in quota_block.go).

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -run 'TestResolverQuota|TestProviderManagerQuota' -count=1 -v`

### Task 5: Broker stub (no-op, existing tests pass)

**Objective:** Keep `internal/llm/broker.go` compile-clean with the new
types. No production behavior (broker is not wired).

**Files:**
- Modify: `internal/llm/broker.go` (add QuotaBlockedUntil/ActiveQuotaBlocks
  as no-ops; add isRetryableError branch for QuotaResetError to match
  ProviderManager semantics)
- Test: `internal/llm/broker_quota_test.go` (regression only)

**Step 1: Write failing test**

Existing broker tests still pass; new stub tests verify no-op behavior
(return empty blocks, return false on isRetryableError for non-quota
errors).

**Step 2: Run test to verify failure**

Run: `go test ./internal/llm/ -run 'TestBroker' -count=1 -v`
Expected: PASS after implementation (this task verifies no regressions).

**Step 3: Write minimal implementation**

No-op methods + the isRetryableError extension.

**Step 4: Run test to verify pass**

Run: `go test ./internal/llm/ -count=1` (full llm suite, scope with -run
if long).

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No mutex held across HTTP calls
- [ ] No key material in logs
- [ ] Zero-value ResolverConfig behaves as enabled-with-defaults (existing
      constructor sites unaffected)
- [ ] HasHealthyModels excludes quota-blocked candidates (the key semantic
      change; existing callers that depend on "non-current always available"
      may need updating — document the behavior)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match exactly (signatures, types, fields)
- [ ] Shared-pool blocks work (same credential -> same block)
- [ ] ProviderManager quota branch tested AND no recordFailure on quota
- [ ] Race-clean under `go test -race`
- [ ] Existing Resolver tests still pass (the hasHealthyModels behavior
      change must not regress)

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- **Critical semantic shift**: the existing `HasHealthyModels` considers
  non-current models always available. With quota blocking, a non-current
  model whose credential is blocked MUST also be considered unavailable.
  This is intentional — the whole point is to not burn a request on a
  blocked credential. Document this in the leaf report; existing tests
  that assert "non-current always available" will need updating.
- Wait computation: `wait = ResetAt.Sub(now)`, clamp to MaxWait; unknown
  ResetAt -> DefaultEstimate. If computed wait <= 0, no block (window
  already passed).
- The Resolver's existing cooldown (AliasHealth.CooldownUntil) is for
  transient failures (auth, 5xx); quota blocks are separate (entryBlocks
  / credentialBlock). Do not conflate the two.
- `internal/llm/broker.go` is dead code in production — keeping the
  stub avoids breaking test consumers but the real work is in Resolver.
