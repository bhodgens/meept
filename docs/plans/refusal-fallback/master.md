# Refusal Fallback - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root)
- **Children:** 6 leaf documents under this node
- **Scope:** Detect provider model refusals and transparently re-dispatch the turn to a configured refusal fallback model (typically a local uncensored model), with visible observability and one-hop give-up semantics.

## Goal

Frontier providers increasingly ship safety classifiers that refuse or block
requests by content class (cyber, bio/chem). When meept's coder agent hits such
a refusal mid-task, the turn currently fails. This feature adds a
`refusal_model` slot to models.json5. When the serving model refuses, the
agent loop re-dispatches the SAME turn to the refusal model once, publishes a
visible bus event, and surfaces a clear error if the fallback also refuses.

Design principles (from the 2026-09-16 design discussion):

1. **Explicit signals only (phase 1).** Detection keys on provider-declared
   signals: Anthropic `stop_reason: "refusal"`, OpenAI-compatible
   `finish_reason: "content_filter"`, and typed safeguard error bodies.
   NO content sniffing of successful responses — that is the false-positive
   failure mode the Fable rollout demonstrated.
2. **A refusal is not a model-health failure.** The model is fine; it said no.
   Mirror the quota invariant: a refusal must NEVER reach
   `Resolver.RecordAliasFailure`.
3. **Never silent.** The fallback publishes `agent.model_escalated` with
   reason `refusal_fallback` (existing topic, existing WS
   `agent_progress` classification). The step result names the model that
   actually served.
4. **One hop, then give up.** If the refusal model also refuses, surface the
   refusal error to the caller. Never loop between models.

## Architecture

Detection lives in `internal/llm`: every client path (anthropic, openai-
compatible client.go, codex) maps refusal signals onto a new typed
`*RefusalError` implementing `NonRetryable` — the exact pattern of
`QuotaResetError` in `internal/llm/errors_quota.go:17-78`. Config gains a
`refusal_model` slot following the `extract_model` precedent
(`internal/config/config.go:360`, `internal/llm/providers.go:111-114`,
pre-warm in `internal/llm/inuse.go`). The agent loop
(`internal/agent/loop.go:5764` quota branch) gains a sibling refusal branch
that re-dispatches with the refusal model override instead of failing, plus
observability via the existing `agent.model_escalated` topic
(`internal/agent/verification_escalation.go:37`).

## Interface Contracts

### Contract 1: RefusalError type

```go
// File: internal/llm/errors_refusal.go
package llm

// RefusalError is a provider-side policy refusal: the model (or a
// classifier in front of it) declined the request by policy. NOT a
// model-health failure — must never reach Resolver.RecordAliasFailure.
// NonRetryable on the SAME model: retrying cannot change the verdict.
type RefusalError struct {
    ProviderID   string
    ModelID      string
    Source       string // "finish_reason" | "stop_reason" | "error_body"
    FinishReason string // raw signal, e.g. "refusal", "content_filter"
    Message      string // provider detail, truncated to 500 chars
    StatusCode   int    // HTTP status when from an error body; 0 otherwise
    Cause        error
}

func (e *RefusalError) Error() string
func (e *RefusalError) Unwrap() error
func (e *RefusalError) NonRetryable() bool // true
var _ NonRetryableError = (*RefusalError)(nil)

// DetectRefusal maps a provider signal onto *RefusalError.
// finishReason: "refusal" (Anthropic stop_reason) or "content_filter"
// (OpenAI-compatible finish_reason) => RefusalError. Empty => nil.
func DetectRefusal(providerID, modelID, finishReason string) *RefusalError

// DetectRefusalFromBody inspects a non-2xx error body for typed
// safeguard/refusal markers (conservative keyword list pinned in
// errors_refusal.go: e.g. "safeguard", "refusal", "content policy").
// Returns nil when the body is not a refusal.
func DetectRefusalFromBody(providerID, modelID string, statusCode int, body string) *RefusalError
```

Owner: 01-llm-refusal-error.md
Consumers: 03-loop-refusal-branch.md, 04-refusal-observability.md

### Contract 2: refusal_model config slot

```go
// internal/config/config.go (ModelsConfig) AND internal/llm/providers.go
// (ProvidersConfig) — both add:
RefusalModel string `json:"refusal_model"` // provider/id or alias name the
// loop re-dispatches to on refusal. Empty string = feature disabled.
// Overlay merge in providers.go applyOverlay follows the ExtractModel
// pattern (providers.go:296-298).

// internal/llm/inuse.go — ModelSlots gains RefusalModel; BuildModelsInUse
// adds it to the boot pre-warm set (inuse.go:90 sibling add() call).
```

Value forms: `provider/model-id` OR a bare alias name. The loop resolves
bare names through the existing `Resolver.ResolveEscalationRef` seam
(`internal/agent/verification_escalation.go:20-27`).

Owner: 02-refusal-model-slot.md
Consumers: 03-loop-refusal-branch.md, 06-docs-and-config-templates.md

### Contract 3: loop refusal branch

```go
// File: internal/agent/loop_refusal.go (new; the loop.go hook site is a
// small errors.As branch inserted BEFORE the generic non-rate-limit
// RecordAliasFailure branch at loop.go:5825, AFTER the quota branch).
package agent

// handleRefusal decides and executes the fallback:
//   - refusalModelRef from spec/config: ModelsConfig.RefusalModel (slot)
//     or AgentSpec.EscalationModel when spec declares override_refusal.
//   - already serving the refusal model => give up: return the original
//     *llm.RefusalError (one-hop rule).
//   - slot empty => feature off: return the original error unchanged.
//   - otherwise: switch to the refusal model for this turn
//     (llm.WithResolvedModel / SwitchModel — the SAME request-scoped seam
//     verification escalation uses via SetOverrideApplier), publish the
//     event (Contract 4), and retry the current step once.
// NO RecordAliasFailure. NO resolver block. Bounded: single retry.
func (l *AgentLoop) handleRefusal(err *llm.RefusalError) (retry bool, err error)
```

Placement rule: the refusal check must precede the generic
`RecordAliasFailure` branch so the refusal never mutates alias health.

Owner: 03-loop-refusal-branch.md
Consumers: 04-refusal-observability.md, 05-e2e-and-ws-verification.md

### Contract 4: observability event (REUSE existing topic)

```go
// Topic: internal/agent/verification_escalation.go:37
const TopicAgentModelEscalated = "agent.model_escalated" // REUSED, no new topic
// Payload keys (superset of the existing publisher — same keys, reason differs):
// {agent_id, from_model, to_model, reason: "refusal_fallback", fix_loops: 0}
// WS classification: existing HasPrefix(agent.model_escalated) =>
// agent_progress in internal/comm/http/server.go. NO server.go change.
// Ledger: the retry's llm_calls row already names the serving provider/model
// (existing resolved-model identity invariant). No metrics.db schema change.
```

Owner: 04-refusal-observability.md

### Contract 5: e2e stub refusal endpoint

```go
// File: tests/refusal_fallback_test.go (httptest server speaking the
// OpenAI-compatible wire format).
// Scenario A: first call returns finish_reason "content_filter", second
// call (expected: refusal model id) returns 200 "done".
// Scenario B: both calls refuse => surface RefusalError, exactly 2 calls.
// Scenario C: slot empty => single call, original error surfaces, no retry.
```

Owner: 05-e2e-and-ws-verification.md

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-llm-refusal-error.md | leaf | none | 55K | A |
| 02 | 02-refusal-model-slot.md | leaf | none | 45K | A |
| 03 | 03-loop-refusal-branch.md | leaf | 01, 02 | 70K | B |
| 04 | 04-refusal-observability.md | leaf | 03 | 40K | B |
| 05 | 05-e2e-and-ws-verification.md | leaf | 03, 04 | 50K | C |
| 06 | 06-docs-and-config-templates.md | leaf | 02 | 35K | C |

**Concurrency groups:** A runs first (01, 02 parallel — disjoint files).
B runs after A is REVIEWED (03 then 04 — both touch `internal/agent/`,
serialized to avoid same-package churn; 03 touches loop.go, 04 is a new
file + verification_escalation.go comment). C runs after B (05, 06 parallel).

## Dispatch Protocol

For each concurrency group, in dependency order:

### Phase 1: Dispatch Concurrency Group [A]

Dispatch these children simultaneously (disjoint files):

1. **Read** 01-llm-refusal-error.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 01-llm-refusal-error.md"
   - Context: full leaf text + Contracts 1 + coding conventions + INLINED
     snippet of `internal/llm/errors_quota.go:1-78` (the pattern to mirror)
     + `NonRetryableError` definition location.
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests,
     report results only."
   - Include: "Do NOT use read_file on existing source files — explore with
     search_files or terminal cat. Never feed read_file output into write_file."

2. **Read** 02-refusal-model-slot.md and dispatch via `delegate_task`:
   - Goal: "Implement all tasks from 02-refusal-model-slot.md"
   - Context: full leaf text + Contract 2 + conventions + INLINED snippets
     of `internal/config/config.go:350-365`, `internal/llm/providers.go:100-130`
     and `:280-310`, `internal/llm/inuse.go:1-100`.
   - Include the do-not-commit and no-read_file rules above.

### Phase 2: Review and Commit Each Child

After each implementation agent returns, the orchestrator reviews in-session
(main model reviews directly — NOT a delegated subagent):

1. Read the changed files; check against leaf spec + contracts.
2. Run: `go build ./... && go test -p 2 ./internal/llm/ ./internal/config/ -short`
3. Gaps => re-dispatch with specific feedback (max 3 cycles).
4. Pass => `git add <exact leaf paths> && git commit -m "feat(llm): <leaf name>"`,
   update the tracking table to REVIEWED.

### Phase 3: Dispatch Concurrency Group [B]

Sequential — 04 depends on 03's branch:

1. Dispatch 03-loop-refusal-branch.md (context: leaf + Contracts 1-4 +
   INLINED `internal/agent/loop.go:5752-5830` quota branch + 5825-5880
   generic branch + `verification_escalation.go:20-110`).
2. Review in-session: `go test -p 2 ./internal/agent/ -short -run Refusal`.
3. Commit, then dispatch 04-refusal-observability.md.
4. Review, commit.

### Phase 4: Dispatch Concurrency Group [C]

1. Dispatch 05-e2e-and-ws-verification.md and 06-docs-and-config-templates.md
   in parallel.
2. Review: run the full e2e test from leaf 05; verify docs cross-references
   resolve; run `make graphs-check` (no bus-topic change expected — confirm
   no diff) and the AGENTS.md freshness items from leaf 06.

### Phase 5: Integration Review

After ALL children reach REVIEWED:

1. `go build ./... && go test -p 2 ./... -short` (full suite; meept needs
   `-p 2` — unbounded parallelism exhausts ephemeral ports on macOS).
2. Verify contracts end-to-end: grep that `RecordAliasFailure` has no
   refusal-branch caller; grep that no new bus topic was added.
3. `gofmt -l` on changed files; corruption check
   `grep -rcE '^\s+[0-9]+\|' --include='*.go' internal/ tests/` returns zero.
4. Commit any integration fixes; update tracking table to COMPLETE.

## Review Checklist

The orchestrator (main model) verifies each child in-session:

- [ ] All tasks from the leaf document are implemented
- [ ] Interface contracts from this orchestrator are satisfied
- [ ] All specified files created/modified at exact paths
- [ ] Tests written and passing (TDD followed)
- [ ] Code follows project conventions (see Coding Conventions below)
- [ ] No scope creep (nothing beyond spec)
- [ ] No obvious bugs or security issues
- [ ] No debug artifacts: no print debugging, no TODOs, no placeholder values
- [ ] No line-number corruption: no `     N|` prefixes baked into source files

Output: APPROVED or list of specific gaps.

## Coding Conventions

- **Language:** Go (module std: see go.mod; match surrounding files).
- **Naming:** exported PascalCase, unexported camelCase; error types end in
  `Error`; constructors `NewX`.
- **Error handling:** wrap with `%w`; sentinels checked with `errors.As` /
  `errors.Is`; never `_ = f()` (pre-commit blocks it); never bare `panic`.
- **No mutex across I/O** (mutexio analyzer); collect under lock, operate after.
- **IDs:** never `time.Now().UnixNano()` / `math/rand` (predid analyzer).
- **Setter methods:** every `Set*` gets a nil guard (setters_test.go contract).
- **Testing:** table-driven where natural, plain `testing` + hand checks
  (match internal/llm test style); `_test.go` alongside; run with `-p 2`.
- **Formatting:** gofmt before reporting completion.
- **Comments:** full-sentence rationale, cite file:line of patterns mirrored.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-llm-refusal-error | PENDING | 0 | |
| 02-refusal-model-slot | PENDING | 0 | |
| 03-loop-refusal-branch | PENDING | 0 | |
| 04-refusal-observability | PENDING | 0 | |
| 05-e2e-and-ws-verification | PENDING | 0 | |
| 06-docs-and-config-templates | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. Unit: `go test -p 2 ./internal/llm/ ./internal/config/ ./internal/agent/ -short`
2. E2E: `go test -p 2 ./tests/ -run TestRefusalFallback -v` — Scenario A/B/C
   from Contract 5 must all pass against the stub endpoint.
3. Behavioral greps (must ALL hold):
   - `grep -n "RefusalError" internal/agent/loop.go` — branch precedes the
     RecordAliasFailure branch (line order check).
   - `grep -rn "agent.refusal" internal/` — ZERO hits (no new topic).
   - `git grep -n "RecordAliasFailure" internal/agent/loop_refusal.go` —
     zero hits (refusal never mutates alias health).
4. Config round-trip: `./bin/meept config get models.refusal_model` returns
   the configured value after `config set` (needs build from `make build`).

## Structural Completeness Check (Before Dispatch)

Run after authoring ALL documents and BEFORE any dispatch:

```
python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans/refusal-fallback --strict-leaves
```

Required orchestrator sections: Dispatch Protocol, Interface Contracts,
Review Checklist, Coding Conventions, Completion Tracking Table,
Integration Test Plan. Re-run until `ALL TREES COMPLIANT: True`.

## Open Questions (defaults chosen; user may override before dispatch)

1. **Slot scope** — global `refusal_model` slot (chosen) vs per-alias
   `refusal_fallback` field. Global matches extract_model precedent and
   ships in one config line; per-alias allows different fallbacks per role
   but adds resolver surface. Default: global.
2. **Interactive turns** — step/background turns fall back silently-with-
   event (chosen). Interactive chat turns: same behavior in phase 1 (the
   event reaches the client as agent_progress). A follow-up could surface
   a user-visible note in the reply text. Default: event-only.
3. **AgentSpec override** — whether employees can declare their own refusal
   target overriding the global slot. Deferred; Contract 3 reserves the
   spec seam. Default: global slot only.

## Notes

- The quota-resilience invariant set in AGENTS.md is the governing precedent.
  Read `internal/agent/loop.go:5752-5830` before touching the error path.
- `make graphs` regenerates line-offset-embedded graphs; any loop.go edit
  requires `make graphs` in the integration commit (CI gate graphs-check).
- Local uncensored fallback models are MLX/llama.cpp endpoints already
  configured in models.json5 providers — no provider-layer work needed.
- Phase 2 (content sniffing via classifier_model) is deliberately OUT of
  scope for this tree.
