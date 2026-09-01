# Endpoint-Level Timeout Cooldowns - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Resolver-side endpoint cooldown blocks (shared fate for
  models on one base endpoint) + alias-level `timeout:` honored only
  when explicitly configured, firing on consistent member failure; docs.
- **Dependencies:** 01-classification.md
- **Estimated Context:** 65K
- **Concurrency Group:** C (dispatch AFTER leaf 03 merges — overlapping files)
- **Decision references:** D10

## Goal

Timeout/cooldown granularity today is alias-level only (one CooldownUntil
per AliasHealth). D10 fixes the semantics:

1. **Endpoint shared fate (primary, CROSS-ALIAS).** A timeout (or
   FailureThrottle verdict) on ANY model of a base endpoint blocks the
   ENDPOINT: `openai/model-1 (medium)` timing out skips `openai/model-2
   (thinkhard)` too — INCLUDING across aliases (the medium alias and
   the thinkhard alias are different aliases; the block must be
   visible to both). ⚠️ AUDIT B3: this state therefore CANNOT live on
   `AliasHealth` (which is per-alias, resolver.go getOrCreateHealth) —
   it lives on the **Resolver struct**: `endpointBlocks
   map[string]time.Time` keyed by `EndpointKey(cfg)`, guarded by the
   single Resolver.mu regime, checked in the same rotation walk as
   isQuotaBlocked (resolver.go:894+).
2. **Endpoint key scope (audit R2).** `EndpointKey(cfg)` = endpoint
   URL host + credential fingerprint (`QuotaCredentialKey`'s provider
   portion, models.go:356-358 precedent). Host-only is WRONG in config
   practice: gala-mlx and gala-llama are distinct runtimes sharing host
   `gala` (config/models.json5:186,205); xai (API key) and xai-oauth
   (subscription) share api.x.ai (:251,:275) — a host-only key would
   block the unrelated credential. Host+credential delivers D10's
   model-1/model-2 shared fate (same provider, same key, same host)
   without cross-credential over-blocking.
3. **Alias-level timeout (explicit opt-in only).** When an alias entry
   declares `timeout:` (> 0) in its alias config, N CONSECUTIVE failures
   of the SAME member model block the whole ALIAS for that duration
   (incremental backoff: each additional consistent failure doubles the
   block, capped at the alias horizon). Without an explicit
   `timeout:`, an alias-level block NEVER applies — member/endpoint
   blocks carry the load.
4. Rotation treats endpoint-blocked and alias-blocked models like
   quota-blocked ones: skipped; all-blocked → the distinct
   `ErrAllEndpointsBlocked` (new) mirroring ErrAllModelsQuotaBlocked.
   Clearing: lazily after expiry + a successful call (same pattern as
   quota blocks — reuse is mandatory, do not invent a second clearing
   mechanism).

## Context

Key files:
- `internal/llm/models.go:322-328` — AliasEntry (Timeout field at :324 —
  exists but is treated as a generic cooldown base) and AliasHealth
  (models.go:337-359; entryBlocks/credentialBlock maps = the per-entry
  precedent; FailedProviderID/FailedModelID identity fields support the
  consistent-member rule).
- `internal/llm/resolver.go` — ResolveForAlias rotation (~line 406-433:
  cooldown advance, quota-block skip, all-blocked error),
  RecordAliasFailure, isQuotaBlocked. The new isEndpointBlocked/
  isAliasBlocked checks sit in the same walk.
- `internal/llm/providers.go:54` — provider ModelConfig.MaxConcurrency
  (0 = unlimited) — reconcile: provider max-concurrency config bounds the
  model slot gate; document the precedence in the docs task. NOTE: this
  is a ModelConfig field, NOT provider Options — the plan's earlier
  "Options.Timeout (cooldown seconds)" reading was wrong; Options has no
  Timeout field (grep providers.go:269 for the overlay handling).
- Where alias config parses `timeout:` — locate the alias config struct
  (grep `Timeout` in config schema near alias definitions) to add the
  doc note; the FIELD exists already, so this leaf changes SEMANTICS +
  DOCS, not the field.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 3:

```go
// internal/llm/resolver.go (+ models.go)
// EndpointKey: url host + credential fingerprint (audit R2 — host-only
// would cross-block gala-mlx/gala-llama and xai/xai-oauth; see Goal §2).
func EndpointKey(cfg *ModelConfig) string
// ⚠️ AUDIT B3: endpointBlocks lives on the RESOLVER struct (NOT
// AliasHealth — AliasHealth is per-alias and cannot deliver D10's
// cross-alias shared fate):
//   Resolver adds: endpointBlocks map[string]time.Time (Resolver.mu regime)
// Resolver adds rotation skips + ErrAllEndpointsBlocked (distinct error)
// Alias-level: AliasEntry.Timeout > 0 arms alias blocks on consistent
// same-member consecutive failures; incremental doubling capped by
// 4× the alias's own Timeout base (documented — there is no provider
// Options.Timeout to use as the ceiling).
```

### What This Leaf Consumes

```go
// From 01-classification.md
FailureClass, FailureThrottle // verdicts feed RecordAliasFailure paths
```

## Tasks

### Task 1: EndpointKey + endpointBlocks state

**Objective:** Block map + key derivation + tests.

**Files:**
- Modify: `internal/llm/resolver.go` (EndpointKey + endpointBlocks map ON
  THE RESOLVER STRUCT + block set/get — audit B3; NOT on AliasHealth)
- Test: `internal/llm/resolver_endpoint_test.go` (new file — note:
  resolver_escalation_test.go does NOT exist today; existing resolver test
  files are resolver_test.go, resolver_direct_test.go, resolver_quota_test.go,
  resolver_caller_test.go)

**Step 1:** Failing tests: EndpointKey derives host+credential from
models sharing an endpoint (two fixture models, same host AND same
credential, different model ids → same key); distinct hosts → distinct
keys; SAME host but DIFFERENT credentials (xai vs xai-oauth fixture) →
distinct keys; empty/nil endpoint → stable fallback key. **Step 2:**
FAIL. **Step 3:** Implement. **Step 4:** PASS.

### Task 2: Rotation integration

**Objective:** ResolveForAlias skips endpoint-blocked models; distinct
all-blocked error.

**Files:**
- Modify: `internal/llm/resolver.go` (rotation walk + error)
- Test: resolver_endpoint_test.go

**Step 1:** Failing tests: model-1 fails with a timeout → RecordAliasFailure
marks the endpoint (Resolver-level map) → next ResolveForAlias on the
SAME alias returns model-2 ONLY if it lives on a DIFFERENT endpoint
(fixture with a second endpoint); same-endpoint model-2 → skipped; AND a
SECOND alias containing the same-endpoint model also skips it (the
cross-alias B3 test); every candidate endpoint-blocked →
error is ErrAllEndpointsBlocked (errors.Is distinct from
ErrAllModelsQuotaBlocked); expiry clears lazily (advance the injected
clock). **Step 2:** FAIL. **Step 3:** Implement mirroring the quota
walk (resolver.go:406-428). **Step 4:** PASS.

⚠️ AUDIT M4 — TRANSPORT-TIMEOUT PATH (mandatory scope for this leaf):
transport-level timeouts (ctx deadline, connection refused) reach
RecordAliasFailure WITHOUT an HTTP status/body, so Classify never sees
them. This leaf adds the classification seam: RecordAliasFailure accepts
a failure-kind input (status-based verdict OR a transport-timeout
marker) and the transport path maps to the SAME endpoint-block outcome.
Without this, endpoint cooldowns fire on 429s but never on true
timeouts — inverting D10's purpose. Test: a timeout-only failure (no
HTTP response) marks the endpoint.

### Task 3: Alias-level explicit timeout (consistent-member rule)

**Objective:** `timeout:` opt-in semantics with incremental backoff.

**Files:**
- Modify: `internal/llm/resolver.go` (RecordAliasFailure path)
- Test: resolver_endpoint_test.go

**Step 1:** Failing tests: alias WITH Timeout=60: same member fails
MaxFails times → alias blocked 60s; next consistent failure → 120s
(doubling, cap 4×base); DIFFERENT member failing resets the consecutive
counter (uses FailedProviderID/FailedModelID identity, mirroring issue
#30's misattribution guard); alias WITHOUT Timeout → repeated failures
never alias-block (endpoint blocks still apply). **Step 2:** FAIL.
**Step 3:** Implement. **Step 4:** PASS.

### Task 4: Docs (D10 requires this explicitly)

**Objective:** The semantics are documented where users configure them.

**Files:**
- Modify: `docs/configuration/` models/aliases page: a "timeouts and
  cooldowns" section stating — (a) endpoint shared fate with the
  model-1/model-2 example; (b) alias `timeout:` is opt-in, fires on
  CONSISTENT same-member failure, incremental doubling, cap; (c)
  provider `max_concurrency` bounds the slot gate; endpoint-block
  DURATION comes from the alias `timeout:` base or the 30s default;
  (d) precedence: quota blocks > endpoint blocks > alias blocks.
- Modify: `AGENTS.md` Critical Invariants — add one bullet: endpoint-
  level cooldown identity + the distinct all-blocked error (same-commit
  rule).

**Verify:** docs reviewed for accuracy against the tests.

## Self-Verification Checklist

- [ ] Same-endpoint models share timeout fate (tested with fixtures)
- [ ] Alias timeout applies ONLY when explicitly configured (tested)
- [ ] Consistent-member identity check prevents cross-member misattribution
- [ ] ErrAllEndpointsBlocked distinct via errors.Is
- [ ] Lazy clearing reuses the quota-block pattern (no second mechanism)
- [ ] gofmt/vet/analyzers clean; `go test ./internal/llm/ -count=1` green

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Existing alias rotation tests green (no regression in default cooldown behavior)
- [ ] Docs section matches implemented semantics exactly (precedence list included)
- [ ] Resolver.mu single-lock regime preserved; no new mutex on AliasHealth

Output: APPROVED or specific gaps with file + line references.

## Notes

- Injected clock: the resolver tests already have a time-injection
  pattern for cooldown expiry — find and reuse it; if none exists, add a
  `now func() time.Time` field defaulting to time.Now (matches
  SHARED-CONVENTIONS §5).
- Do NOT change ProviderManager health-check behavior in this leaf; the
  ProviderManager mirror (if desired) is a follow-up — record in
  Deviations if its alias path diverges.
