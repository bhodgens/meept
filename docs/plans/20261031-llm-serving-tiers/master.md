# LLM Serving Tiers (4-Tier Degradation Policy + Semantic Cache + KV-Cache/TTFT Pieces) - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 6 leaf documents (R0, 01, 02, 03, 04, 05) + 1 findings doc produced by R0
- **Scope:** Consolidated delivery of proposal 9 (explicit 4-tier degradation policy with per-turn depth ceilings and user-visible degraded-tier signaling, plus an opt-in semantic cache tier behind the exact-key cache miss) and the approved small pieces of proposal 7 (`--cache-reuse 256` on llama-server spawn commands + TTFT instrumentation with an `llm_calls.ttft_ms` column).

## Goal

Meept's LLM layer already rotates aliases on failure (quota blocks, cooldowns,
empty responses) but the rotation is unbounded: a turn can silently fall any
number of tiers, and the user never learns which tier actually served the
answer. This tree adds three capabilities and two config corrections:

1. **Tier policy (01):** explicit degradation-depth limits per request and per
   turn, with user-visible "served by degraded tier" signaling in response
   metadata, layered on top of - never reordering - the FROZEN resolver
   precedence (quota blocks > endpoint cooldowns > alias blocks > stickiness).
2. **Semantic cache tier (02):** an embedding-similarity lookup that runs
   strictly ADDITIVELY behind an exact-key cache MISS in the existing cache
   path. Opt-in (`llm.cache.semantic.enabled`, default false). Hard safety
   constraints: never serve a semantic hit carrying tool calls; embedder
   failure degrades to exact-only and NEVER parks a turn; ErrEmptyResponse
   accounting is untouched.
3. **KV-cache config correction (03):** add `--cache-reuse 256` to the
   llama-server `spawn_command` entries in `config/models.json5` and fix the
   WRONG "fresh cache per call" comment (llama.cpp `--cache-prompt` is ON by
   default; prompt cache reuse is the norm, not the exception).
4. **TTFT instrumentation (04):** first-delta timestamps in streaming
   clients, a `ttft_ms` column in `llm_calls` via the columnMissingGuard
   migration pattern, surfaced in metrics.

A research leaf (R0) runs FIRST and produces the findings document that 01-04
cite for similarity thresholds, `--cache-reuse` semantics, and TTFT
conventions. An e2e leaf (05) lands the hermetic suite + manifest entry last.

## Architecture

All work lands in the existing LLM serving path:

- Tier policy: `internal/llm/resolver.go` + `internal/llm/provider_manager.go`
  alias-rotation loop gains a depth counter and a per-turn ceiling; a new
  typed floor error stops rotation beyond the ceiling. Signaling rides the
  EXISTING per-step response metadata channel (no new bus topic - pinned).
- Semantic cache: new `internal/llm/semantic_cache.go` behind the exact-key
  lookup in `internal/llm/client.go` (the cache is consulted BEFORE the
  budget gate today). The existing `TokenCacheCoordinator` (L1 + L2-SQLite)
  is untouched; the semantic tier is a second-chance lookup on miss only.
- KV-cache: config-only change plus a comment fix; no Go code.
- TTFT: streaming clients (OpenAI-compatible, Anthropic) timestamp the first
  content delta; the value flows through the existing call-record path into
  `internal/metrics/store.go` via migration version N+1 with the
  `columnMissingGuard` pattern.

## Interface Contracts

### C1: TierPolicyConfig (config schema)

```
// internal/config/schema.go - field on LLMConfig (json5 key: llm.tiers)
type TierPolicyConfig struct {
    Enabled        bool           // default false; opt-in like all serving policy
    MaxDepth       int            // max alias degradations per request; default 2 when Enabled
    TurnCeilings   map[string]int // capability -> per-turn max total degradations; absent = MaxDepth
    SignalDegraded bool           // emit llm_tier response metadata; default true when Enabled
}
// Owner: 01-tier-policy.md
// Consumers: 05-e2e-llm-serving-tiers.md (config fixture), master Integration Test Plan
```

### C2: Tier floor error (rotation stop)

```
// internal/llm/errors_tier.go
var ErrTierFloor = errors.New("llm: tier degradation floor reached")
// Returned by the alias-rotation loop when the next rotation would exceed
// MaxDepth (or the capability's TurnCeiling). MUST be reachable via errors.Is.
// It is a CLASSIFIED terminal error (like ErrAllModelsQuotaBlocked) - it is
// never retried, never surfaced as a completed turn, and never parks a turn.
// Owner: 01-tier-policy.md
// Consumers: 05-e2e-llm-serving-tiers.md (asserts the classified error shape)
```

### C3: Degraded-tier signaling keys (response metadata - PINNED CHANNEL)

```
// Per-step response metadata (the existing agent-loop step metadata map;
// NOT a new bus topic, NOT a new WS type):
//   "llm_tier"           string - canonical model id that served the turn
//   "llm_tier_degraded"  bool   - true when depth > 0 (a fallback served it)
//   "llm_tier_depth"     int    - number of degradations from the primary
// Written ONLY when llm.tiers.enabled and llm.tiers.signal_degraded are true.
// Zero bytes of metadata change when the feature is off (zero-behavior rule).
// Owner: 01-tier-policy.md
// Consumers: 05-e2e-llm-serving-tiers.md (asserts keys on a degraded turn)
```

### C4: SemanticCacheConfig (config schema)

```
// internal/config/schema.go - field on LLMSimpleFeatureConfig's neighborhood;
// json5 key: llm.cache.semantic
type SemanticCacheConfig struct {
    Enabled       bool    // default FALSE - opt-in, never on by default
    Threshold     float64 // cosine similarity floor; default 0.92 (R0 findings may adjust)
    MaxEntries    int     // default 1024
    TTLMin        int     // minutes; default mirrors llm.cache.default_ttl_min
    EmbedderAlias string  // alias/capability used to resolve the embedder model
}
// Owner: 02-semantic-cache.md
// Consumers: 05-e2e-llm-serving-tiers.md (config fixture)
```

### C5: SemanticCache lookup seam

```
// internal/llm/semantic_cache.go
type Embedder interface {
    Embed(ctx context.Context, text string) ([]float32, error)
}
type SemanticCache struct { /* unexported fields */ }
func NewSemanticCache(embedder Embedder, cfg config.SemanticCacheConfig) *SemanticCache
// LookupBehindMiss consults similarity ONLY after the exact-key miss.
// Returns (response, true, nil) on a hit; (nil, false, nil) on miss;
// (nil, false, nil) on ANY embedder failure (exact-only degradation,
// never an error to the caller, never a parked turn).
// Invariants (HARD):
//   - a cached response carrying tool calls is NEVER served as a semantic
//     hit (mirror the classifyAnthropicEmpty tool_use exemption shape)
//   - does not touch ErrEmptyResponse accounting or the rotation path
//   - disabled (zero value) = structurally unreachable code path
// Owner: 02-semantic-cache.md
// Consumers: 05-e2e-llm-serving-tiers.md
```

### C6: TTFT plumbing

```
// Streaming clients timestamp the first content delta:
//   internal/llm/client.go + internal/llm/anthropic.go
//     firstDeltaAt time.Time captured on the first non-empty content delta
// Call-record + metrics row:
//   TTFTMS int64 // milliseconds from request start to first delta; -1 = not
//                // measured (non-streaming, cache hits, errors)
// Migration (internal/metrics/store.go, version = current max + 1):
//   ALTER TABLE llm_calls ADD COLUMN ttft_ms INTEGER NOT NULL DEFAULT -1
//   Guard: columnMissingGuard("llm_calls", "ttft_ms")  // same pattern as the
//          llm_calls.session_id / reasoning_tokens migrations
// Owner: 04-ttft-metrics.md
// Consumers: 05-e2e-llm-serving-tiers.md (asserts the column + populated row)
```

### C7: llama-server KV-cache spawn config

```
// config/models.json5 - BOTH llama-server spawn_command entries (ports 8080
// and 8084) gain:  "--cache-reuse", "256"
// The stale comment near line 345 ("llama.cpp builds a fresh cache per call")
// is REPLACED with the corrected statement: --cache-prompt is ON by default;
// --cache-reuse 256 enables chunk-level KV reuse across calls sharing a
// prompt prefix. Config-only; no Go code; no schema change.
// Owner: 03-cache-reuse-config.md
// Consumers: 05-e2e-llm-serving-tiers.md (config-loads assertion only)
```

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| R0 | R0-research.md | leaf (research) | none | 35K | A |
| 01 | 01-tier-policy.md | leaf | R0 (findings doc) | 95K | B |
| 02 | 02-semantic-cache.md | leaf | R0 (findings doc) | 100K | C |
| 03 | 03-cache-reuse-config.md | leaf | R0 (findings doc) | 40K | C |
| 04 | 04-ttft-metrics.md | leaf | R0 (findings doc) | 70K | B |
| 05 | 05-e2e-llm-serving-tiers.md | leaf | 01, 02, 03, 04 | 90K | D |

- Group A: R0 alone - everything cites its findings doc.
- Group B (01, 04) and Group C (02, 03) may overlap; 01 and 02 BOTH edit
  `internal/config/schema.go` (different struct blocks, append-only) - if a
  collision appears at review, stagger: dispatch 02 after 01 reaches
  REVIEWED. 02 and 04 both work near `internal/llm/client.go` but touch
  disjoint functions (cache path vs streaming delta path).
- Group D: 05 after all implementation leaves reach REVIEWED.

## Dispatch Protocol

For each concurrency group, in dependency order. Full recipe: skill
`hierarchical-plan-execution`.

### Phase 0: Test Gate

No separate 00T- test-gate leaves in this tree (see Test Gate section for the
deliberate deviation): the repo's e2e policy puts feature tests in the
hermetic e2e tier (leaf 05) and in-package unit tests inside each
implementation leaf (TDD). The gate is structural instead: an implementation
leaf may not reach REVIEWED without its unit tests passing under
`go test -p 2`, and 05 may not dispatch until every implementation leaf is
REVIEWED.

### Phase 1: Dispatch Concurrency Group A

Dispatch R0 via `delegate_task`: goal = "Execute all tasks from
R0-research.md"; context = full leaf text + conventions + the no-commit /
no-git-add / write-once rules. R0 uses `web_search` and writes the findings
doc. On return: review the findings doc in-session (are the three research
questions answered with cited sources? are threshold recommendations
actionable?), then commit it with the tree docs and mark REVIEWED.

### Phase 2: Dispatch Concurrency Groups B and C

Batch 01 + 04 (group B) and 02 + 03 (group C) via `delegate_task`, each with:
full leaf text, the contracts it consumes from this master, the findings-doc
path from R0, and the standard rules (do NOT commit, do NOT git add, do NOT
use read_file on existing sources - explore with search_files/terminal cat;
write once and stop; Go tests run with `-p 2`). Stagger 01 vs 02 only on a
schema.go collision (see Child Index).

### Phase 3: Review and Commit Each Child

Review in-session (the main model - never a delegated reviewer). Check leaf
spec, contracts, Review Checklist; run the leaf's verify commands; gate-test
diff must be empty. Pass: commit the leaf's exact file list + status
REVIEWED. Gaps: re-dispatch with findings, max 3 cycles, then escalate.

### Phase 4: Dispatch Concurrency Group D (e2e)

Dispatch 05 after ALL of 01-04 are REVIEWED. Review its return in-session;
run the full suite (`make e2e-fast` or at minimum
`go test -p 2 -tags e2e ./e2e/suites/llm-serving-tiers/...`), then commit.

### Phase 5: Integration Review

After ALL children REVIEWED: verify contracts C1-C7 across leaves, run the
full suite `go test -p 2 ./...` plus `make e2e-fast`, run
`go run ./tools/analyzers/predid/... ./...` and `make mutexio` on touched
packages, and check the zero-behavior-when-disabled rule by grepping that no
new code path executes when `llm.tiers.enabled=false` and
`llm.cache.semantic.enabled=false`. Update AGENTS.md summaries (root +
`internal/llm/AGENTS.md`) in the same commit as the code they describe.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts satisfied (signatures, types, paths)
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (`go test -p 2` on touched packages)
- [ ] Precedence invariant: no cache-affinity or tier logic reorders the
      FROZEN resolver precedence (quota blocks > cooldowns > alias blocks >
      stickiness); tier depth only BOUNDS the rotation length
- [ ] Zero behavior change when `llm.tiers.enabled=false` and
      `llm.cache.semantic.enabled=false`
- [ ] Semantic-cache hard constraints hold: no tool-call-bearing semantic
      hits; embedder failure never errors/parks a turn; ErrEmptyResponse
      accounting untouched
- [ ] Code follows project conventions (typed-nil guards, no mutex I/O, no
      predictable IDs)
- [ ] No scope creep; no obvious bugs or security issues
- [ ] No debug artifacts (print debugging, TODOs, placeholders,
      commented-out code)
- [ ] No line-number corruption (`     N|` prefixes) in source files
- [ ] ASCII only in every file the leaf touched

Output: APPROVED or a list of specific gaps.

## Coding Conventions

- **Language/Framework:** Go (module `meept`); JSON5 config; SQLite metrics store.
- **Naming:** exported only where consumed cross-package; unexported otherwise.
- **Imports:** stdlib, then module-internal; no unused imports (pre-commit enforced).
- **Error handling:** wrap with `%w`; sentinels checked with `errors.Is`;
  no ignored errors, no bare `panic(err)`.
- **IDs:** never `time.Now().UnixNano()` / `math/rand` - `pkg/id.Generate()` only.
- **Mutexes:** never hold across I/O (mutexio analyzer enforced).
- **Testing:** new feature tests in the hermetic e2e tier
  (`e2e/suites/llm-serving-tiers/`, `//go:build e2e`, manifest entry);
  unit tests in-package (`foo_test.go` next to the code); run everything
  with `-p 2` on macOS.
- **Config:** JSON5 with quoted keys; new config defaults OFF unless this
  master pins otherwise (only `signal_degraded` defaults true, and only when
  `tiers.enabled` is true).
- **ASCII only:** every authored file; no unicode punctuation in code, docs,
  or comments.
- **Formatting:** `gofmt` before reporting; no git commit/add by leaf agents.

## Requirement Traceability

| Req | Source (user words, verbatim when possible) | Plan element (leaf/task/contract) | Verified by |
|-----|---------------------------------------------|-----------------------------------|-------------|
| R1 | "tier policy semantics: explicit degradation-depth limits + per-turn tier ceilings" | 01 T1-T3 / C1, C2 | e2e: tier depth ceiling test |
| R2 | "user-visible 'served by degraded tier' signaling (bus event or response metadata - pin which)" | C3 (PINNED: response metadata, no new bus topic) | e2e: degraded-turn metadata assertion |
| R3 | "SEMANTIC CACHE tier: embedding-similarity lookup strictly ADDITIVE behind the exact-key miss" | 02 T1-T4 / C4, C5 | unit: miss-then-similarity order test; e2e: semantic hit on paraphrase |
| R4 | "a semantic hit must NEVER serve a response carrying tool calls" | 02 T3 / C5 invariant 1 | unit: tool-call exemption test |
| R5 | "embedder failure degrades to exact-only and NEVER parks a turn" | 02 T4 / C5 invariant 2 | unit: embedder-error test |
| R6 | "config llm.cache.semantic opt-in default false" | 02 T1 / C4 | unit: config-defaults test |
| R7 | "add --cache-reuse 256 to llama spawn_command entries" | 03 T1 / C7 | unit: TestConfigLoads collision-guarded; e2e: config-loads |
| R8 | "the 'fresh cache per call' comment ... is WRONG - --cache-prompt is ON by default" | 03 T2 / C7 | review checklist: comment corrected |
| R9 | "TTFT instrumentation: first-delta timestamp in streaming clients, ttft_ms column in llm_calls ... surfaced in metrics" | 04 T1-T3 / C6 | unit: migration guard + first-delta test; e2e: ttft row populated |
| R10 | "optional n_past/cache-hit parsing from llama-server logs as stretch" | 04 Notes (stretch only; skip if leaf exceeds budget) | n/a (deferred, named here) |
| R11 | "any cache-affinity preference sits strictly BELOW the frozen block precedence" | 01 T2 invariant; master Review Checklist | review checklist + e2e regression: quota-blocked endpoint still skipped first |
| R12 | "zero behavior change when semantic cache disabled" | master Review Checklist; 02 T1 default | unit: disabled-path test; e2e: disabled run identical |
| R13 | research leaf FIRST; findings doc cited by later leaves | R0 / findings doc | orchestrator review of R0 return |

Rule (canon C1-Trace): every user requirement maps to a plan element. R10 is
explicitly DEFERRED (stretch); silent omission is a defect.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| R0-research | PENDING | 0 | |
| 01-tier-policy | TESTS_VALIDATED | 0 | SetTierPolicy daemon call site LANDED (internal/daemon/tierpolicy_wiring.go, issue #82 arm-up) - rotation depth bound now arms from llm.tiers config |
| 02-semantic-cache | TESTS_VALIDATED | 0 | T5 daemon wiring LANDED (internal/daemon/semantic_cache_wiring.go per docs/plans/20261103-wiring-followups-final/05): embedder reuse > alias > none, Warn + exact-only degraded posture, WithSemanticCache attach in components.go |
| 03-cache-reuse-config | PENDING | 0 | |
| 04-ttft-metrics | PENDING | 0 | |
| 05-e2e-llm-serving-tiers | PENDING | 0 | |

Status values: PENDING | TESTS_IN_PROGRESS | TESTS_VALIDATED | IN_PROGRESS |
IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

Gate rule: 05 stays PENDING until 01-04 all reach REVIEWED (Phase 0 note).

## Integration Test Plan

1. **Tier depth ceiling + signaling (e2e, leaf 05):** stub multi-alias
   topology where alias members 1..N fail (empty responses) and member N+1
   succeeds. With `llm.tiers.enabled=true, max_depth=2`: the turn fails
   classified with `ErrTierFloor` shape at depth 2, never reaching member 3;
   with `max_depth` raised, the succeeding member serves and the step
   metadata carries `llm_tier`, `llm_tier_degraded=true`, `llm_tier_depth`.
   Quota-blocked first member is STILL skipped before any depth counting
   (precedence regression).
2. **Semantic cache (e2e, leaf 05):** stub OpenAI-compatible endpoint +
   stub embedder. Paraphrased repeat of a prior prompt misses the exact key
   and hits semantically above threshold; a prior response carrying tool
   calls is never served; embedder 500s fall through to the live call with
   no error and no park; feature disabled = exact-only behavior unchanged.
3. **Config loads (e2e, leaf 05):** a models.json5 carrying
   `--cache-reuse 256` spawns cleanly through the harness config loader
   (collision-guarded by the existing TestConfigLoads pattern in-package).
4. **TTFT (unit + e2e, leaf 04 + 05):** migration guard test proves
   `ttft_ms` exists on a pre-registry DB after open; streaming stub produces
   a populated `ttft_ms >= 0` row in llm_calls; non-streaming rows carry -1.
5. **Full gates:** `go test -p 2 ./...`, `make e2e-fast`,
   `go run ./tools/analyzers/predid/... ./...`, `make mutexio` on touched
   packages.

## Test Gate

Deliberate deviation: this tree ships NO separate 00T-/01T- test-gate
leaves. The repo's e2e testing policy requires new feature tests in the
hermetic e2e tier and allows in-package unit tests; each implementation leaf
authors its own unit tests TDD-first, and leaf 05 authors the full e2e
suite. Coverage matrix (orchestrator validates at review that every row's
tests exist and pass before the owning leaf reaches REVIEWED; leaf 05 rows
must pass before COMPLETE):

| # | Feature (from plan) | Source (leaf/spec section) | Unit tests | E2E tests | Status |
|---|---------------------|----------------------------|-----------|-----------|--------|
| F1 | degradation depth limit + per-turn ceilings | 01 T1-T2 | internal/llm: tier floor + ceiling tests; daemon: TestTierPolicyWiringEnabled pins the SetTierPolicy call site | llm-serving-tiers: tier-depth scenario | VALIDATED |
| F2 | degraded-tier metadata signaling | 01 T3 | internal/llm or internal/agent: metadata keys test | llm-serving-tiers: metadata scenario | VALIDATED |
| F3 | semantic cache lookup behind exact miss | 02 T2 | internal/llm: semantic_cache_test.go; daemon: TestSemanticCacheWiring* pin the construction site (T5 landed) | llm-serving-tiers: paraphrase-hit scenario | VALIDATED |
| F4 | tool-call semantic exemption + embedder degradation | 02 T3-T4 | internal/llm: exemption + embedder-error tests; daemon: degraded-no-embedder wiring test | llm-serving-tiers: tool-call + embedder-fail scenarios + live-daemon Warn-line pin (TestConfigTierSemanticKeysAccepted) | VALIDATED |
| F5 | --cache-reuse 256 + comment correction | 03 T1-T2 | internal/llm: TestConfigLoads (collision-guarded; the F5 pin landed in providers_config_test.go, not internal/config) | llm-serving-tiers: config-loads scenario | VALIDATED |
| F6 | TTFT column + migration guard + first-delta capture | 04 T1-T3 | internal/metrics: guard test; internal/llm: first-delta test | llm-serving-tiers: ttft-row scenario | VALIDATED (closeout audit 2026-11-05: 04 landed at 0827f72c - migration v9 llm_calls.ttft_ms via columnMissingGuard (store.go:531), captureFirstDelta openai+anthropic (client.go:2187), AvgTTFTMs live path; unit pins green; e2e ttft_ms value assertions deferred-to-fake per suite header) |

Statuses: PENDING | DELTA (mismatch, resolution in progress) | VALIDATED

Delta log: none yet.

## Frozen Documents

Leaf documents freeze at dispatch. Corrections ride dispatch context as
dated addenda. Never edit a dispatched leaf document mid-execution.

## Open Questions

- R0 may recommend a similarity threshold other than the pinned 0.92
  default. If so, the findings value wins and C4's default is corrected via
  a dated addendum BEFORE 02 dispatches (02 depends on R0 for exactly this).
- Tier counting semantics: does a quota-block skip (cheap, pre-request)
  count as a degradation depth step? Pinned: NO - only rotations that
  actually DISPATCH a request and fail (empty response, provider error)
  consume depth. R0 does not need to resolve this; it is a design decision
  recorded here.

## Notes

- VERIFIED ANCHORS (grep-confirmed 2026-10-31) all leaves may rely on:
  alias failover mixing cloud+local exists (config/models.json5:117-118
  coder alias); resolver precedence FROZEN at
  internal/llm/resolver.go:616,659 (see internal/llm/AGENTS.md
  session-affinity section); TokenCacheCoordinator L1+L2-SQLite at
  internal/llm/token_cache.go:88,99 over token_cache.db wired at
  components.go:943-993; ResponseCache interface token_cache.go:70; cache
  consulted BEFORE budget gate client.go:649-657; prefix-aware key
  BuildPrefixAwareKeyWithFiles cache_key_builder.go:249,320 LIVE on all
  four client cache paths client.go:652,849,909,1104 (the old "test-only"
  claim is stale); metrics store built BEFORE coordinator
  components.go:940-990 (the old "nil store" claim is stale); typed
  ContextOverflowError internal/llm/errors_overflow.go:25; ErrEmptyResponse
  sentinel rotation client.go:1979; TTFT measured NOWHERE today (grep
  ttft/first_token = zero hits); llm_calls.latency_ms is full-call duration
  only internal/metrics/store.go:260; tokens_cached/cache_creation_tokens
  columns exist and populate client.go:2006, anthropic.go:308;
  --cache-reuse absent from config/models.json5 (0 hits, verified); KV
  teardown route POST /slots/0?action=erase (internal/llm/AGENTS.md);
  health-checker canary never routes through the LLM client.
- The llama-server spawn entries are at config/models.json5:282 (port 8084)
  and :340 (port 8080, --jinja); the stale "fresh cache per call" comment
  is at ~:345.
- Invariant (GAP-A): any cache-affinity preference outranking quota/cooldown
  re-creates the dead-endpoint ping-pong pinned in internal/llm/AGENTS.md.
  Nothing in this tree reorders precedence - reviewers check this on EVERY
  leaf that touches resolver/provider_manager/client.
- Template-port sweep rules: NOT APPLICABLE - no port edits expected in
  this tree.
- Sizing: 01-04 each stay under 15 files / 110K context; 05 owns the single
  new e2e suite dir + manifest entry.
