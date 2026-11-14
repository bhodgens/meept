# Phase 2 — Partial Context Warm (B-lite) — Branch Orchestrator

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

Today `last:false` partial prompts (voiceframe `Last *bool`, frame.go:29; emitted at pipeline.go:1069) reach the daemon and only `last:true` starts a turn — partials otherwise inform barge-in/aggregation, not the LLM. The employee turn's first token therefore waits for full-prompt prefill. Fix without touching turn semantics: on a debounced partial, issue a cache-warming request (max_tokens=1) over the EXACT prefix the real turn will use (system prompt, persona, history, partial user text), under the same session-affinity scope (`internal/llm/client.go:725` `prompt_cache_key`, default-on per internal/llm/AGENTS.md). llama.cpp-family servers then hit their prompt cache on the final turn. No speculative drafts: the warm's completion is discarded; no tools; no effects; no visible output.

## Architecture

- Leaf 01 (`internal/llm`): `Warm` seam (root Contract C4) on the OpenAI-compatible client — request shape identical to a normal turn's payload build, max_tokens=1, non-streaming, `Warm: true` opts marker, response discarded, errors swallowed at DEBUG. Quota/budget posture per internal/llm/AGENTS.md: a warm is traffic — it rides the budget gate but must NEVER park/queue a real turn behind itself (separate in-flight cap) and never consumes the turn's short-retry budget.
- Leaf 02 (`internal/voice` or `internal/daemon` — decide at dispatch: the partial ingress is in the voice wiring path; the turn builder is in agent/employee): debounce per (session, employee): fire at most one warm per `voice.turn_confirm_ms` window, replace-pending on newer partial (latest text only), cancel pending warm when last:true arrives (the real turn supersedes), skip entirely when the prompt is < N tokens (default 60; warming a trivial prefix is pointless traffic) or when no voice call is live.
- File ownership: 01 → `internal/llm/client.go` (+ new `warm.go`), 02 → the voice partial routing file + config keys. Disjoint.

## Interface Contracts

C4 (Warm signature) frozen in root master.md. C6 (new, this branch):
```
config voice.context_warm        bool   default false (opt-in per docs/agents/opt-in-defaults.md rule)
config voice.context_warm_min_tokens int default 60
```
Wire/behavior contract with leaf 01: `Warm` is fire-and-forget from the
caller's perspective (internal goroutine + ctx), idempotent per prefix, and
returns nothing.

## Child Index

| Leaf | Type | Est. ctx | Depends on | Wave |
|------|------|----------|------------|------|
| 00T-gate-tests.md | leaf(test) | 50K | none | 0 |
| 01-llm-warm-seam.md | leaf | 55K | 00T | A |
| 02-partial-warm-debounce.md | leaf | 60K | 00T | A |

## Coding Conventions

Root master.md conventions verbatim, plus: opt-in defaults MUST default
false (byte-identical disabled behavior is a pinned test, not a hope); warm
requests never log completion text; session-affinity header scope must be
the SAME value the real turn uses (an empty scope sends no header — client
rule; warm inherits it).

## Completion Tracking Table

| Leaf | Status |
|------|--------|
| 00T-gate-tests | PENDING |
| 01-llm-warm-seam | PENDING |
| 02-partial-warm-debounce | PENDING |

## Test Gate

Suite target: `e2e/suites/context-warm` + manifest entry. Branch-local
matrix:

| Feature | Unit test | E2E test | Status |
|---------|-----------|----------|--------|
| Warm payload shape (max_tokens=1, discarded, same prefix) | llm warm_test | suite: fake upstream records 2 requests (warm then turn), prefix shared | PENDING |
| Disabled = byte-identical (opt-in) | config test | suite: flag off ⇒ zero warm requests seen | PENDING |
| Debounce + supersede on last:true | routing_test | suite: 4 rapid partials ⇒ ≤2 warms, final turn unaffected | PENDING |
| R8 no-tools/no-effect/no-quota-park | warm_test | suite: warm against 429/quota-blocked fake upstream never delays the real turn | PENDING |

Dispatch 00T → validate in-session → commit → wave A: 01 ∥ 02 → wave B: 02
integration pass. (02 codes against C4 signature pinned in 00T; it does not
import 01's internals, only the seam.)

## Integration Test Plan

Branch-complete when: leaves REVIEWED, `go test -p 2 ./internal/llm/... ./internal/voice/... -count=1` green, `make e2e-fast-area AREA=context-warm` green, llm AGENTS.md warmed (a C4 paragraph added in the integration commit: warm traffic rules). Report branch COMPLETE to root.

## Review Checklist

- [ ] Warm never produces a user-visible artifact (no WS relay, no session
      message row, no auditlog entry beyond a DEBUG metric).
- [ ] Budget/quota: warm rides gates but cannot park or short-retry-consume
      a real turn (llm AGENTS.md invariants).
- [ ] Default-off flag ⇒ disabled behavior byte-identical (diff test).
- [ ] Teardown: pending warm cancelled on call end / hangup — no zombie
      requests after `end`.

## Dispatch Protocol

Dispatch contexts include root master.md Contract C4 + branch C6 verbatim,
root Coding Conventions, worktree path, "Do NOT commit".

1. Wave 0: `00T-gate-tests.md`; validate RED in-session; commit; mark
   TESTS_VALIDATED.
2. Wave A: `01-llm-warm-seam.md` ∥ `02-partial-warm-debounce.md`
   (file-disjoint; 02 codes against the C4 seam var).
3. Sequence the branch suite-green run after BOTH are REVIEWED (02's tests
   need 01 linked).
4. Per leaf: in-session review → `git commit -- <declared paths>` → table
   bump. Re-dispatch on gaps (max 3 loops).
