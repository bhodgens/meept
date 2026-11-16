# 00T Gate Tests - Implementation Leaf (Tests Only)

> **For the implementing agent:** You are the TEST-GATE implementer.
> Write the tests below ONLY - no implementation. Every test must FAIL RED
> for a missing-implementation reason (undefined symbol, unimplemented
> branch). If a test passes immediately, re-check: it is either already
> covered (drop it and report the duplicate) or it does not test the new
> behavior. Do NOT delete, skip, or weaken any existing test. Do NOT
> commit - the orchestrator handles all git operations after review. Do
> NOT use read_file on existing source files - explore with search_files
> or terminal cat. After writing a file, do NOT read it back.

## Meta

- **Parent:** master.md
- **Scope:** Write the full hermetic e2e suite for requirements R1-R9 of the Sovereign Router parity tree, plus the `e2e/manifest.json` path_map entry. RED today, GREEN after leaves 01-09 land.
- **Dependencies:** none. Tests reference symbols that do not exist yet; that IS the RED state.
- **Estimated Context:** 75K
- **Concurrency Group:** Phase 0 (gate) - runs alone
- **Source references:** `/tmp/SovereignRouter` (reviewed 2026-11-15). Frozen constants are quoted from it, not paraphrased.

## Goal

Produce the failing-test suite that pins all eleven contracts before any implementation dispatches: the selection chain (C1), quality shape and floor (C2), admission verdict (C3), data posture (C4), pool-scoped failure (C5), shadow route (C6), advisory budget (C7), sticky difficulty (C8), streamed tool-call validation (C9), price groups (C10), and the difficulty-derivation source (C11).

## Context

Repo test-placement policy (root AGENTS.md, "e2e Testing Policy"): **all new feature tests go in the hermetic e2e tier; no new unit test files for new features.** Existing unit tests may be updated for bug fixes only. This leaf therefore creates one suite, not unit files.

Suite shape (see `docs/workflows/e2e-testing.md` and an existing suite for the exact harness idioms - `e2e/suites/quota-park/` is the closest precedent because it exercises the resolver and parking surfaces):

```
e2e/suites/sovereign-parity/
  suite_test.go        // //go:build e2e ; TestMain + harness wiring
  quality_test.go      // R1
  admission_test.go    // R2
  posture_test.go      // R3
  shadow_test.go       // R4
  poolfailure_test.go  // R5
  budget_test.go       // R6
  sticky_test.go      // R7
  toolcheck_test.go    // R8
  pricegroup_test.go   // R9
  derivedifficulty_test.go // R7 source (C11)
  flagoff_test.go      // ALL - byte-identical control
```

Manifest (`e2e/manifest.json`): add a `suites` entry

```json
{
  "name": "sovereign-parity",
  "dir": "e2e/suites/sovereign-parity",
  "status": "implemented",
  "diff": "L",
  "scenarios": ["<one line per scenario from the master's Test Gate matrix>"]
}
```

and a `path_map` entry mapping `"internal/llm/"` to the suite list that already covers `internal/llm/` plus `"sovereign-parity"`. Do NOT remove any existing path_map entry.

## Verified Anchors

<!-- Exact, grep-confirmed at authoring time. Do not re-derive; trust and cite. -->

- `internal/llm/resolver.go:528` - `ResolveForAlias(aliasName, sessionID string) (*ModelConfig, error)` - the main resolution entry point
- `internal/llm/resolver.go:500` - `FindCheapest(caps []string) *ModelConfig` - sorts by `TotalCost()` ascending, takes `[0]`
- `internal/llm/resolver.go:324` - `ResolveForSkill(skill *SkillRequirements, currentModel *ModelConfig) (*ModelConfig, error)` - the capability-escalation path that R1 must make quality-aware
- `internal/llm/models.go:410-412` - `func (m *ModelConfig) TotalCost() float64 { return m.CostPerMillionInput + m.CostPerMillionOutput }` - unweighted sum; the current cost model
- `internal/llm/models.go:229-300` - `ModelConfig` struct - gains `Quality`, `DataPosture` fields (leaves 01, 05)
- `internal/llm/providers.go:73-110` - `ModelDef` struct - the `models.json5` per-model schema; gains `quality`, `quality_source`, `quality_verified_at`, `data_posture` (leaves 01, 05)
- `internal/llm/providers.go:737-740` - `ModelConfig` construction from `ModelDef` - the single mapping site
- `internal/llm/errors_tier.go:15-45` - `ErrTierFloor` / `TierFloorError` - the sentinel + `NonRetryable` pattern every new typed error copies
- `internal/llm/budget.go:867-914` - `BudgetExceededError` with `NonRetryable() bool { return true }` and a per-reason `UserMessage()` - R6's default path must stay byte-identical
- `internal/llm/resolver.go:1014-1101` - `resolveStickyCaller` - the function R7 modifies; pin release currently happens only on failure/endpoint-block/quota-block
- `internal/llm/resolver.go:1128-1142` - `recordStickyDecision` - writes `RoutingDecision.Reason`; the reason strings `sticky_request` / `sticky_request_new` already exist
- `internal/llm/routing_log.go:91-105` - `routing_decisions` table: `id, request_id, timestamp, chosen_model_id, chosen_provider_id, alias, reason, skill, employee_id, candidates_json`. NO cost/latency/token columns - R4 must not add any.
- `internal/security/secret_scanner.go:42` - `func (s *SecretScanner) Scan(text string) []string` - R3 reuses this; already tested
- `internal/llm/tier_policy.go:42-47` - `MetaKeyLLMTier` / `MetaKeyLLMTierDegraded` / `MetaKeyLLMTierDepth` - the metadata-only signaling pattern; no new bus topic is allowed
- `e2e/manifest.json` - `version`, `generated`, `comment`, `path_map` (dict, 182 entries), `exempt`, `suites`
- `internal/agent/dispatcher.go:101-140` - `Intent` struct - the classifier output; R7's difficulty must ride alongside it, not replace it

Frozen constants quoted verbatim from `/tmp/SovereignRouter`:

- `src/gateway.py` - `MIN_OUTPUT_TOKENS = 64` (admission reserve)
- `src/gateway.py:706` - `if est_tokens + MIN_OUTPUT_TOKENS > entry.context_tokens: skip("context_too_small")`
- `src/gateway.py:700-703` - `if _selection_quality(entry, task_class, has_tools) < required_quality: skip("quality_below_required")`
- `src/gateway.py:746-759` - `sort_key(e) = (e.estimate_cost(est_tokens, est_out), -_selection_quality(e, task_class, has_tools), e.latency_p50_ms or 99_999)`
- `src/pools.py:176-206` - `classify_failure`: 400/413/422 -> `request_fault`/`request`; 404 -> `entry_missing`/`model`; 401/403 -> `account_dead`/`account`; 402 -> `account_dead` (OpenRouter in-flight subclass first); 429 -> `congested` (pool if `_pool_exhaustion_signal`, else model); 5xx/unmapped -> `transient`/`model`
- `src/validation.py:78-110` - failure codes `empty_response`, `length_no_content`, `unknown_function:<name>`, `tool_args_invalid_json`, `json_mode_parse_fail`
- `src/validation.py:113-126` - `validate_streamed_text_outcome`: after first content commit, only `empty_response` / `length_no_content` may be logged, not escalated

## Interface Contracts (From Parent)

### What This Leaf Exposes

Nothing. Tests only. This leaf exposes the *contract* every later leaf compiles against.

### What This Leaf Consumes

All of Contracts C1-C9 in `master.md`. The tests reference these exact symbols:

```
llm.Candidate{Model, Alias, Capability, EstInput, EstOutput}
llm.FilterCtx{SessionID, Capability, EstInput, EstOutput, Sensitive, Difficulty, Now}
llm.VerdictAllow / llm.VerdictSkip / llm.VerdictFloor
llm.Filter{Name() string; Filter(*Candidate, *FilterCtx) Verdict; Reason() string}
(*Resolver).runSelectionFilters(*Candidate, *FilterCtx) (Verdict, string)

llm.Quality{Chat, Code, Reasoning, Tooling, LongContext int}
llm.QualityChat / QualityCode / QualityReasoning / QualityTooling / QualityLongContext
(Quality).QualityFor(llm.QualityClass) int
llm.QualityFloorFor(difficulty int, capability string) int
ModelConfig.Quality          // llm.Quality
ModelDef.Quality             // llm.Quality, json:"quality"
ModelDef.QualitySource       // string, json:"quality_source"
ModelDef.QualityVerifiedAt   // string, json:"quality_verified_at"

llm.ErrContextTooSmall
llm.ContextTooSmallError{ModelID, ProviderID, EstInput, Reserve, ContextLimit}
(*ContextTooSmallError).NonRetryable() bool

llm.PosturePrivate / PosturePublicOnly / PostureNonSensitive
ModelDef.DataPosture         // llm.Posture, json:"data_posture"
ModelConfig.DataPosture      // llm.Posture

llm.FailureScope / llm.ScopeModel / ScopePool / ScopeAccount / ScopeRequest
llm.FailureClass / ClassAccountDead / ClassCongested / ClassTransient / ClassRequestFault / ClassEntryMissing
llm.ClassifyFailure(providerID, modelID string, status int, bodyExcerpt string) (FailureClass, FailureScope, string)

llm.ShadowRequest / llm.ShadowDecision / llm.ShadowCandidate
(*Resolver).ResolveShadow(ctx, llm.ShadowRequest) (*llm.ShadowDecision, error)

llm.BudgetModeEnforce / llm.BudgetModeSteer
llm.AdvisoryVerdict{OverTarget, Limit, Used, Cap}
(*llm.Budget).Advisory() llm.AdvisoryVerdict

llm.ErrToolCallInvalid
llm.ValidateToolCalls(resp *llm.Response, offered []string) error

llm.PriceGroupFree / PriceGroupPaid / PriceGroupPremium
llm.ParsePriceGroup(s string) llm.PriceGroup
(*llm.Resolver).ResolveSpillway(ctx, []string, *llm.FilterCtx) (*llm.ModelConfig, string, error)
(*llm.Resolver).AliasesByPriceGroup(g llm.PriceGroup) []string
ModelAliasEntry.PriceGroup      // string, json:"price_group,omitempty"

llm.DeriveDifficulty(mode string, requiresPlanning bool, inputLen int) int
```

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Suite scaffold: `//go:build e2e`, `TestMain`, harness boot against a scratch daemon with a fake upstream, temp data dir | Create: `e2e/suites/sovereign-parity/suite_test.go` | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run TestSovereignParity -count=1` | RED (no tests yet) | compiles, runs zero tests | Given the harness; When TestMain runs; Then a daemon boots on a scratch dir and tears down |
| T2 | R1 quality order: a code turn picks the high-`quality.code` model even when a cheaper model exists | Modify: `quality_test.go` | `go test -tags e2e -p 2 ./e2e/suites/sovereign-parity/ -run 'TestQuality_' -count=1` | RED (undefined `llm.Quality`, `ModelDef.Quality`) | GREEN after leaf 01 | Given two providers, cheap/score-1 and dear/score-5; When a code turn resolves; Then the dear model serves and the routing log names it |
| T3 | R1 quality floor: difficulty 5 with only score-2 candidates surfaces `ErrQualityFloor`, never a completed turn | same | `-run TestQuality_FloorBlocks' | RED | GREEN | Given candidates all below the floor; When resolve; Then `errors.Is(err, llm.ErrQualityFloor)` |
| T4 | R1 legacy inertness: models with NO `quality` block keep cost order (score defaults to 0 -> floor 0) | same | `-run TestQuality_AbsentIsInert'` | RED | GREEN | Given two quality-less models; When resolve; Then selection is byte-identical to the pre-change cost order |
| T5 | R2 context gate: `EstInput + 64 > ContextLimit` rejects pre-dispatch with `ErrContextTooSmall` and makes ZERO upstream requests (fake-upstream counter asserts 0) | Create: `admission_test.go` | `-run 'TestAdmission_'` | RED (undefined) | GREEN after leaf 02 | Given a 100k prompt and a 64k-context model; When chat; Then `errors.Is(err, ErrContextTooSmall)` and the fake upstream saw 0 requests |
| T6 | R2 unknown limit: `ContextLimit == 0` allows the request | same | `-run TestAdmission_UnknownContextAllows'` | RED | GREEN | Given a model with `context_limit` absent; When chat; Then the request reaches upstream |
| T7 | R3 sensitive posture: a prompt carrying an AWS-shaped key never routes to `public_only` or `non_sensitive` | Create: `posture_test.go` | `-run 'TestPosture_'` | RED (undefined `llm.Posture*`, `ModelDef.DataPosture`) | GREEN after leaf 05 | Given a `public_only` model first in the alias; When a prompt contains `AKIA` + 16 chars; Then a `private` model serves |
| T8 | R3 posture absent = private: no `data_posture` key anywhere gives byte-identical routing to `main` | same | `-run TestPosture_AbsentIsPrivate'` | RED | GREEN | Given no posture config; When a normal turn; Then routing is unchanged |
| T9 | R3 no secret leakage: the scanner's matched string never appears in any log line or the routing log row | same | `-run TestPosture_NoMatchInLogs'` | RED | GREEN | Given a sensitive prompt; When the turn runs; Then no log line and no DB row contains the matched secret |
| T10 | R4 shadow parity: `ResolveShadow` returns the same model the live path picks, with a populated `Candidates` list carrying skip reasons | Create: `shadow_test.go` | `-run 'TestShadow_'` | RED (undefined) | GREEN after leaf 04 | Given a configured alias; When shadow-resolve; Then `Selected.ModelID` equals the live `ResolveForAlias` result |
| T11 | R4 no side effects: shadow writes ZERO `llm_calls` rows, consumes ZERO budget, mutates no alias health, creates no sticky pin | same | `-run 'TestShadow_NoSideEffects'` | RED | GREEN | Given a fresh daemon; When shadow-resolve twice; Then `llm_calls` count is 0 and a subsequent live resolve picks the same model (no pin was created) |
| T12 | R5 account scope: a 401 on model A blocks the whole account, so sibling model B on the same provider is skipped | Create: `poolfailure_test.go` | `-run 'TestPoolFailure_'` | RED (undefined `llm.ClassifyFailure`, `llm.Scope*`) | GREEN after leaf 06 | Given provider p with models A and B; When A returns 401; Then the next resolve skips B too |
| T13 | R5 request fault: a 400 blocks nothing | same | `-run TestPoolFailure_RequestFaultBlocksNothing'` | RED | GREEN | Given a 400 from model A; When resolve; Then A is still eligible |
| T14 | R5 classification table: assert every `(status, body) -> (class, scope)` pair from `pools.py:176-206` verbatim, incl. 402, 404, 429-with-pool-phrase, 5xx | same | `-run TestPoolFailure_ClassifyTable'` | RED | GREEN | Given the table; When classify; Then each row matches |
| T15 | R6 steer mode: `mode: steer` with the daily cost cap exceeded COMPLETES the turn and emits a `budget_advisory` signal; `BudgetExceededError` is never returned | Create: `budget_test.go` | `-run 'TestBudgetSteer_'` | RED (undefined `llm.BudgetModeSteer`, `(*Budget).Advisory`) | GREEN after leaf 08 | Given a tiny daily cost cap already exceeded; When a turn runs in steer mode; Then the turn completes and the advisory is visible |
| T16 | R6 enforce default: with no `mode` key, `BudgetExceededError` is returned and the turn is refused (byte-identical to `main`) | same | `-run TestBudgetSteer_EnforceIsDefault'` | RED | GREEN | Given the cap exceeded and no mode key; When a turn runs; Then `*llm.BudgetExceededError` and the turn fails |
| T17 | R7 sticky difficulty: a new user message classified ABOVE the pinned level releases the pin and re-pins at the rotation head, reason `sticky_difficulty_increase` | Create: `sticky_test.go` | `-run 'TestSticky_'` | RED (undefined `StickyPin.Difficulty`) | GREEN after leaf 07 | Given a pin created at difficulty 2; When a difficulty-4 turn resolves; Then the pin moves and the routing log reason is `sticky_difficulty_increase` |
| T18 | R7 no false release: a same-or-lower difficulty turn keeps the existing pin | same | `-run TestSticky_SameDifficultyKeepsPin'` | RED | GREEN | Given a pin at difficulty 3; When a difficulty-2 turn resolves; Then the same model serves |
| T19 | R8 unknown function: a tool call naming a function absent from the offered list returns `ErrToolCallInvalid` and the executor is never invoked | Create: `toolcheck_test.go` | `-run 'TestToolCheck_'` | RED (undefined `llm.ValidateToolCalls`, `ErrToolCallInvalid`) | GREEN after leaf 03 | Given tools `[a,b]` offered and a response calling `c`; When validate; Then `err == llm.ErrToolCallInvalid` by pointer identity, executor counter 0 |
| T20 | R8 bad arguments: arguments that are not a JSON object fail validation | same | `-run TestToolCheck_BadArgs'` | RED | GREEN | Given a call with `arguments: "{not json"`; When validate; Then `ErrToolCallInvalid` |
| T21 | R8 stream completion: arguments split across SSE chunks validate only after the stream ends; a mid-stream abort does not produce a false pass | same | `-run TestToolCheck_Streamed'` | RED | GREEN | Given a streamed call split at an argument boundary; When the stream completes; Then validation runs once on the assembled call |
| T22 | R7 difficulty source: `DeriveDifficulty` maps `Intent` signals to 1-5 with no classifier call; and difficulty 0 keeps R7 inert | Create: `derivedifficulty_test.go` | `-run 'TestDeriveDifficulty'` | RED (undefined `llm.DeriveDifficulty`) | GREEN after leaf 07 | Given `(requiresPlanning, "quick_plan", ...)`; When derived; Then the frozen table level; Given no Intent (0); Then non-release |
| T23 | R9 price group: a free-tagged alias and a paid-tagged alias both eligible; the free serves while healthy, the paid spillway serves only on free exhaustion/failure | Create: `pricegroup_test.go` | `-run 'TestPriceGroup_'` | RED (undefined `llm.ResolveSpillway`, `llm.PriceGroup`) | GREEN after leaf 09 | Given free A healthy and paid B; When spillway(["free"]) then spillway(["paid"]); Then A then B |
| T24 | R9 untagged legacy: aliases with no `price_group` keep legacy resolution; spillway is opt-in per-caller and never reorders rotation | same | `-run TestPriceGroup_NoMutation'` | RED | GREEN | Given untagged aliases; When a normal turn; Then the decision equals `main` |
| T25 | ALL flag-off control: with none of the nine features configured, every routing decision is byte-identical to `main` (golden rows) | Create: `flagoff_test.go` | `-run TestFlagOff'` | GREEN (partially - golden rows must be captured from `main`) | GREEN | Given a config with no new keys; When N turns resolve; Then the `routing_decisions` rows match the checked-in golden file |
| T26 | Manifest registration: `sovereign-parity` suite + `internal/llm/` path_map entry; `make e2e-fast-area AREA=sovereign-parity` discovers the suite | Modify: `e2e/manifest.json` | `make e2e-fast-area AREA=sovereign-parity` | n/a (discovery) | suite listed and run | Given the manifest; When the area target runs; Then the suite executes |

## Self-Verification Checklist

- [ ] Every scenario in the master's Test Gate matrix has exactly one test function.
- [ ] Every test is RED for a *missing implementation* reason, not a typo or a compile error in unrelated code.
- [ ] No test passes immediately. If one does, report it as a duplicate rather than deleting it silently.
- [ ] Suite carries `//go:build e2e` and follows the harness idioms in `e2e/suites/quota-park/`.
- [ ] Manifest has both the `suites` entry and the `path_map` entry; no existing entry removed.
- [ ] No production code was modified. `git status` shows only `e2e/` files.
- [ ] No new unit test file under `internal/`.
- [ ] No test hits a real provider endpoint; all upstreams are fakes.
- [ ] No test asserts on a log string that contains prompt or key material.
- [ ] Golden rows for T22 are captured from `main` and checked in as a fixture, not generated at run time.

## Review Checklist

- [ ] Each Test Gate matrix row maps to a named test function.
- [ ] Contract symbols referenced by tests match `master.md` C1-C9 exactly (a test that references a symbol no leaf declares is a plan bug - report it).
- [ ] Tests are hermetic: scratch daemon, temp dirs, fake upstream, no network.
- [ ] Scratch-bin hygiene respected: harness builds into `$TMPDIR/meept-e2e-bin*`; no ownerless `os.MkdirTemp`.
- [ ] No `time.Sleep` used as a synchronisation primitive; poll with a deadline.
- [ ] T22's golden fixture is stable across runs (sorted, no timestamps, no absolute paths).
- [ ] `go vet ./e2e/suites/sovereign-parity/` clean.

## Do NOT commit

The orchestrator commits this leaf after review.