# Phase 2 - Partial Context Warm (B-lite) - Branch Orchestrator

> **For the executing agent:** You are the orchestrator for this branch.
> Dispatch implementation agents, review in-session, re-dispatch on gaps,
> commit per leaf after review. Do NOT implement code yourself.
> Full operational detail: skill `hierarchical-plan-execution`.

## Meta

- **Role:** Branch
- **Parent:** ../master.md (root: Voice Latency tree)
- **Children:** 3 leaves (`00T-gate-tests.md`, `01-llm-warm-seam.md`, `02-partial-warm-debounce.md`)
- **Scope:** Warm the provider-side LLM prefix cache from STT partials before the final turn dispatch, so the real turn pays decode, not prefill. Requirements R7, R8, R9 in the root.

## Goal

Today `last:false` partial prompts (`Last *bool`, `internal/voiceframe/frame.go:29`; emitted by `consumeTranscripts` in `internal/voicechild/pipeline.go`) reach the daemon and only `last:true` starts a turn: `internal/daemon/voice_wiring.go:1154` and `:1192` act only on `last:true`, so partials fall through to the preview path. The employee turn's first token therefore waits for the full prompt prefill. Fix without touching turn semantics: on a debounced partial, issue a cache-warming request (max_tokens=1) over the same prefix the real turn will use, under the same session scope (affinity header plus `prompt_cache_key`, `internal/llm/client.go:725`, default on). A llama.cpp-family server started with `--cache-reuse` (`config/models.json5:259`, `:318`) then reuses the cached prefix on the final turn. No speculative drafts: the warm's completion is discarded; no tools; no effects; no visible output.

Scope limit: MLX servers (`mlx_lm server`, `config/models.json5:185`) and cloud providers do not reuse prompt prefixes for this shape. Warm is a silent no-op there. Do not attempt provider-specific cache APIs.

## Architecture

- Leaf 01 (`internal/llm`): the `Warm` seam (root Contract C4). Same non-streaming payload builder as the turn (including `tools`), max_tokens=1, response discarded, errors swallowed at DEBUG. The three hard constraints: NO response-cache traffic, NO short-retry loop, NO Resolver/identity side effects. Each is a pinned test.
- Leaf 02 (`internal/daemon/voice_warm.go`, new file, plus one hook line in `VoiceWiring.routeFrame`): debounce per (session, employee). Fire at most one warm per `voice.turn_confirm_ms` window; a newer partial replaces pending text (latest wins); skip entirely when the estimated prompt is under `voice.context_warm_min_tokens` (default 60) or no voice call is live for the ccid.
- **Do not cancel an in-flight warm when `last:true` arrives.** Let it finish under its own deadline; only stop scheduling new ones. Cancelling mid-flight aborts the server's prefill and can leave a partial slot, and the real turn is already queueing behind it.
- File ownership: 01 -> `internal/llm/client.go`, `internal/llm/warm.go` (new). 02 -> `internal/daemon/voice_warm.go` (new), `internal/daemon/voice_wiring.go` (ONE hook line + the cancel-on-end lookup), config keys. Disjoint from 01.
- **Cross-branch collision:** Phase 1 leaf 02 also edits `internal/daemon/voice_wiring.go`. Run Phase 1 to COMPLETE first, or land this leaf after Phase 1 leaf 02's commit. Never dispatch the two concurrently.

## Interface Contracts

C4 (frozen in root master.md, reproduced here for dispatch contexts):

```go
// Warm pre-issues a cache-warming request for the given rendered messages
// and caller opts: non-streaming, max_tokens=1, the SAME payload builder and
// session scope the real turn uses. It NEVER returns a completion, NEVER
// consumes turn budget semantics, NEVER starts tools or an agent turn, and
// NEVER reads or writes any response cache.
//
// Failures (any kind) are logged at DEBUG and swallowed by design.
func (c *Client) Warm(ctx context.Context, messages []ChatMessage, opts ...ChatOption) error
```

> **AMENDMENT 2026-11-16 (mirrors root master.md C4 amendment; doc-only;
> audit BUG #1, owner default applied, veto pending):** The as-built,
> shipped signature (merged 648eb6eb) is RATIFIED:
>
> ```go
> func (c *Client) Warm(ctx context.Context, alias string, messages []ChatMessage, sessionScope string)
> ```
>
> (`internal/llm/warm.go:109`; mirrored by this branch's seams
> `internal/daemon/voice_wiring_contextwarm.go:58` `warmDispatcher` and
> `internal/voice/contextwarm.go:68` `WarmFn`). Rationale and the two
> further as-built hard-rule notes (no-tools payload; 10 s ceiling) live in
> the root master.md amendment — this block reproduces the verdict for
> dispatch contexts. Behavior unchanged; doc-only delta. Hard rule 3 (no
> cache traffic) verified SATISFIED at HEAD 2026-11-16 (root Delta Log).

Notes the leaves must not "improve" away:
- The message type is `llm.ChatMessage` (`internal/llm/models.go:23`), not `Message`.
- ~~There is no `alias` parameter~~ (SUPERSEDED by the 2026-11-16 amendment:
  `alias` IS a parameter, as a DEBUG-attribution label only — Warm performs
  no alias resolution; resolution stays at the Resolver/ProviderManager
  layer, which is the intent the original note protected).
- The response-cache bypass was added as a dedicated transport, not an
  option flag: `sendWarmRequest` (`internal/llm/warm.go:190`) never enters
  `doRequest`, whose cache block is `client.go:785`/`:977` exact-key and
  `:383`/`:410` semantic (HEAD line numbers). Verified no-cache at HEAD
  2026-11-16.

C6 (new, this branch):
```
config voice.context_warm            bool  default false (opt-in per docs/agents/opt-in-defaults.md)
config voice.context_warm_min_tokens int   default 60
```

Wire/behavior contract with leaf 01: `Warm` is synchronous from the caller's
viewpoint but runs on the leaf 02 goroutine; it owns its own deadline
(default 1200 ms) and returns `error` for the caller to log at DEBUG.

## Child Index

| Leaf | Type | Est. ctx | Depends on | Wave |
|------|------|----------|------------|------|
| 00T-gate-tests.md | leaf(test) | 50K | none | 0 |
| 01-llm-warm-seam.md | leaf | 55K | 00T | A |
| 02-partial-warm-debounce.md | leaf | 60K | 00T | A |

## Coding Conventions

Root master.md conventions verbatim, plus: opt-in defaults MUST default false (byte-identical disabled behavior is a pinned test, not a hope); warm requests never log completion text; the session scope must be the SAME value the real turn uses (an empty scope sends no header - client rule; warm inherits it); `priority=false` on every warm (llm AGENTS.md D11 two-lane slot gate).

## Completion Tracking Table

| Leaf | Status |
|------|--------|
| 00T-gate-tests | VALIDATED (4 scenarios authored; all GREEN in e2e/suites/context_warm at HEAD 5f64419a, run 2026-11-16: TestContextWarmPartialThenFinal, TestContextWarmDisabledZeroWarms, TestContextWarmTokenFloorSkips, TestContextWarmQuotaUpstreamTurnUnaffected — 4/4 PASS) |
| 01-llm-warm-seam | VALIDATED (merged 30e2f6ed, ancestor of 648eb6eb on main; unit suite internal/llm ok 2026-11-16 incl. -race earlier at leaf time: 19/20 green then, full package green now; compile-time signature pin TestWarmSignature_C4 live at warm_test.go:162) |
| 02-partial-warm-debounce | VALIDATED (merged 48cfae06 + 30e2f6ed wiring, ancestors of 648eb6eb on main; commit sweep B landed via the merge; unit ok internal/voice + internal/daemon contextwarm tests 2026-11-16) |

Delta log (branch):

- 2026-11-16 (reconciliation wave, doc-only): leaf statuses reconciled
  against disk + git (all three merged on main via 648eb6eb/18538f29, both
  verified ancestors of HEAD). Test Gate matrix below reconciled PENDING ->
  VALIDATED against the suites run green 2026-11-16. C4 amended to ratify
  the as-built signature (root master.md amendment applies here verbatim).
  Hard rule 3 verified SATISFIED at HEAD (root Delta Log: warm.go zero
  tokenCache/semantic references; dedicated sendWarmRequest transport never
  enters doRequest's cache block client.go:783-796).

## Test Gate

Suite target: `e2e/suites/context-warm` (hyphen, matching existing suite names) + `e2e/manifest.json` `path_map` entry. Branch-local matrix:

| Feature | Unit test | E2E test | Status |
|---------|-----------|----------|--------|
| Warm payload shape (max_tokens=1, non-streaming, same tools/prefix) | llm warm_test: TestWarm_PayloadShape, TestWarm_PrefixIdentity | suite TestContextWarmPartialThenFinal | VALIDATED (unit + e2e green 2026-11-16; as-built note: warm payload carries NO tools — root C4 amendment) |
| Warm cannot pollute a cache | llm warm_test: structural (warm.go has zero tokenCache/semantic references; dedicated sendWarmRequest transport, warm.go:190) | suite TestContextWarmPartialThenFinal (warm then turn both reach upstream) | VALIDATED (verified mechanically + e2e green 2026-11-16; see root Delta Log) |
| Warm never retries | llm warm_test: TestWarm_QuotaUpstream_QuietNoRetry, TestWarm_TransportErrorQuiet | suite TestContextWarmQuotaUpstreamTurnUnaffected | VALIDATED (unit + e2e green 2026-11-16) |
| Disabled = byte-identical (opt-in) | config test: TestContextWarmDisabledIsNoOp; TestContextWarmDisabledNoWarms | suite TestContextWarmDisabledZeroWarms | VALIDATED (unit + e2e green 2026-11-16) |
| Debounce + latest-wins | routing_test: TestContextWarmDebounceLatestWins, TestContextWarmThrottleSpacing, TestContextWarmSubFloorSkips | suite TestContextWarmTokenFloorSkips (sub-floor skip covered; the 4-partials-≤2-warms scenario has no direct e2e — unit pins only) | VALIDATED (unit + e2e green 2026-11-16; e2e debounce-count gap noted honestly) |
| R8 no-tools/no-effect/no-quota-park | warm_test: TestWarm_NoLLMCallsRow, TestWarm_BudgetExhaustedStillWarms, TestWarm_NeverQueuesBehindRealTurn, TestWarm_ReturnedTextNeverSurfaces | suite TestContextWarmQuotaUpstreamTurnUnaffected | VALIDATED (unit + e2e green 2026-11-16) |
| Non-reuse endpoint no-op | warm_test: TestWarm_AnthropicCodexNoop (client-class no-op twins); MLX/no-predicate case is covered by the disabled-flag pin, not a dedicated endpoint predicate | suite: none (no dedicated suite scenario) | PARTIALLY VERIFIED (unit twins green 2026-11-16; the single-predicate endpoint gate was NOT shipped — the as-built seam is silent on Anthropic/Codex client classes and gated by the opt-in flag; flagged in root Delta Log) |

Dispatch 00T -> validate in-session -> commit -> wave A: 01 || 02 -> then the branch suite-green run.

## Integration Test Plan

Branch-complete when: leaves REVIEWED, `go test -p 2 ./internal/llm/... ./internal/daemon/... -count=1` green, `make e2e-fast-area AREA=context-warm` green, `internal/llm/AGENTS.md` carries the warm paragraph (warm traffic rules) in the integration commit. Report branch COMPLETE to root.

## Review Checklist

- [ ] Warm never produces a user-visible artifact (no WS relay, no session message row, no auditlog entry beyond a DEBUG metric).
- [ ] Warm cannot write `tokenCache` or the semantic tier, and cannot READ them (see the warm-then-turn test).
- [ ] Warm cannot re-enter the short-retry loop (`client.go:817`) and cannot record an alias failure or a sticky endpoint.
- [ ] Budget/quota: a warm never blocks in `budget.WaitForRateLimit` and its errors never park, retry, or short-circuit a real turn (llm AGENTS.md invariants).
- [ ] `priority=false` on every warm (background slot lane).
- [ ] Default-off flag implies disabled behavior byte-identical (diff test).
- [ ] Teardown: no warm is scheduled after call end; an in-flight warm is allowed to finish under its own deadline and is never cancelled mid-flight.

## Dispatch Protocol

Dispatch contexts include root master.md Contract C4 + branch C6 verbatim, root Coding Conventions, worktree path, "Do NOT commit".

1. Wave 0: `00T-gate-tests.md`; validate RED in-session; commit; mark TESTS_VALIDATED.
2. Wave A: `01-llm-warm-seam.md` || `02-partial-warm-debounce.md` (file-disjoint; 02 codes against the C4 seam var).
3. Sequence the branch suite-green run after BOTH are REVIEWED (02's tests need 01 linked).
4. Per leaf: in-session review -> `git commit -- <declared paths>` -> table bump. Re-dispatch on gaps (max 3 loops).