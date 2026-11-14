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

Notes the leaves must not "improve" away:
- The message type is `llm.ChatMessage` (`internal/llm/models.go:23`), not `Message`.
- There is no `alias` parameter: alias resolution lives at the Resolver/ProviderManager layer. The caller resolves, then passes opts.
- A cache bypass does not exist today and must be ADDED (`client.go:785`/`:977` exact-key, `:383`/`:410` semantic).

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
| 00T-gate-tests | COMPLETE (4 scenarios authored; 2 RED awaiting integration) |
| 01-llm-warm-seam | REVIEWED (19/20 tests green incl. -race) |
| 02-partial-warm-debounce | IMPLEMENTED (all 4 e2e scenarios GREEN with daemon wiring; commit sweep B pending) |

## Test Gate

Suite target: `e2e/suites/context-warm` (hyphen, matching existing suite names) + `e2e/manifest.json` `path_map` entry. Branch-local matrix:

| Feature | Unit test | E2E test | Status |
|---------|-----------|----------|--------|
| Warm payload shape (max_tokens=1, non-streaming, same tools/prefix) | llm warm_test | suite: fake upstream records 2 requests (warm then turn), prefix shared | PENDING |
| Warm cannot pollute a cache | llm warm_test | suite: warm then turn with identical messages reaches upstream (no cache hit) | PENDING |
| Warm never retries | llm warm_test | suite: empty-text fake upstream sees exactly 1 request per warm | PENDING |
| Disabled = byte-identical (opt-in) | config test | suite: flag off implies zero warm requests | PENDING |
| Debounce + latest-wins | routing_test | suite: 4 rapid partials imply at most 2 warms, final turn unaffected | PENDING |
| R8 no-tools/no-effect/no-quota-park | warm_test | suite: warm against 429/quota-blocked fake upstream never delays the real turn | PENDING |
| Non-reuse endpoint no-op | warm_test | suite: endpoint without prefix-reuse support sees zero warms | PENDING |

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