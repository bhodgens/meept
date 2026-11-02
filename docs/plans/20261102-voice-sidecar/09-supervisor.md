# Supervisor gate - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The failing gate test ALREADY EXISTS
> (parked .staged from leaf 00T T5) - make it pass; do NOT modify gate
> tests. Do NOT commit. Do NOT use read_file on existing source files -
> explore with search_files or terminal cat. Write once, do not read back.

## Meta

- **Parent:** master.md
- **Scope:** C6 supervisor: per-sentence allow/deny on a fast endpoint, hard timeout, fallback line, config toggle (assumption A4: voice.supervisor_enabled default true), verdict pipelining seam
- **Dependencies:** leaf 01 (config)
- **Estimated Context:** 70K
- **Concurrency Group:** B
- **Files this leaf owns:** internal/voice/supervisor.go

## Goal

No fact reaches the caller unapproved. The supervisor is a separate
employee (voice.supervisor_employee) or a direct LLM ask on
voice.supervisor_model - implementation: a narrow Chatter interface
injected at wiring (leaf 14 decides employee-vs-direct). Hard timeout
voice.supervisor_timeout_ms; on timeout/error/quota the Verdict is
{Allowed:false, Reason:"timeout"} and the CALLER-FACING behavior is the
fallback line, call continues. A voice call is NEVER quota-parked.

## Context

Sentence splitting: split the employee draft on sentence boundaries
(./!/? + newline), cap sentence length. Pipelining (spec mitigation): the
Check API is per-sentence and stateless so the caller (leaf 14 talk loop)
can check sentence N+1 while Telnyx plays N. The supervisor prompt is
fixed and short: given the day-brief-derived policy block + one sentence,
answer ALLOW or DENY with a reason <= 100 runes. Parse tolerantly
(prefix match), default-deny on unparseable (fail-closed).
Quota errors: import internal/llm ONLY for errors.Is on
llm.QuotaResetError / ErrAllModelsQuotaBlocked classification - a quota
block is a timeout-class outcome (fallback line), never a park, never a
retry loop. Check internal/llm/AGENTS.md quota invariants before writing
the classification.
Toggle: supervisor_enabled=false skips Check (Verdict{Allowed:true,
Reason:"disabled"}) - config-gated, logged once at construction, never
per-sentence.

## Verified Anchors

- master.md C6 - Verdict/Supervisor shapes verbatim
- internal/llm/AGENTS.md - quota error classification (errors.Is, never short-retry)
- internal/employee/constitution.go - NeverRule matching (optional reuse for machine-checked denials BEFORE the LLM ask: a sentence matching a Never pattern is denied without spending a model call)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
type Verdict struct{ Sentence string; Allowed bool; Reason string }
type Supervisor interface{ Check(ctx context.Context, callID, sentence string) (Verdict, error) }
type SupervisorChatter interface{ Ask(ctx context.Context, prompt string) (string, error) }
func NewSupervisor(cfg SupervisorConfig, chatter SupervisorChatter, logger *slog.Logger) *Supervisor
type SupervisorConfig struct{ Enabled bool; Timeout time.Duration; FallbackLine string; NeverPatterns []string }
func SplitSentences(draft string) []string
```
Check never returns a quota/park error to the caller: every failure mode
maps to Verdict{Allowed:false} + nil error (the talk loop then speaks the
fallback line). Transport-level ctx cancellation returns ctx.Err().

### What This Leaf Consumes

- C6 (master.md); leaf 01 config values (injected as SupervisorConfig)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Activate gate test; confirm RED | mv internal/voice/supervisor_test.go.staged -> supervisor_test.go | go test -p 2 ./internal/voice/ -run TestSupervisor | undefined: NewSupervisor | RED confirmed | - |
| T2 | SplitSentences: boundaries, caps, empty-draft no-op | Create: internal/voice/supervisor.go | go test -p 2 ./internal/voice/ -run TestSplitSentences | - | PASS | Given 3-sentence draft; When split; Then 3 sentences, order kept |
| T3 | Check happy path: ALLOW/DENY parse, unparseable = deny (fail-closed) | (same) | go test -p 2 ./internal/voice/ -run TestSupervisorVerdicts | - | PASS | Given chatter returns "DENY: location"; When Check; Then Allowed=false Reason carries deny |
| T4 | Timeout: chatter blocks > Timeout -> Verdict{false,"timeout"} within Timeout+50ms; quota error (fake llm.QuotaResetError) -> same shape, NO park, NO retry | (same) | go test -p 2 ./internal/voice/ -run TestSupervisorTimeout | - | PASS | Given blocking chatter; When Check; Then fallback verdict, call continues |
| T5 | NeverPatterns pre-check: matching sentence denied without a chatter call (assert zero Ask invocations) | (same) | go test -p 2 ./internal/voice/ -run TestSupervisorNever | - | PASS | Given pattern "home address" and matching sentence; When Check; Then denied, Ask not called |
| T6 | Toggle: Enabled=false -> Allowed=true Reason="disabled", one construction log | (same) | go test -p 2 ./internal/voice/ -run TestSupervisorDisabled | - | PASS | Given disabled; When Check; Then allowed without Ask |
| T7 | Format/vet/race | - | gofmt -l internal/voice; go vet ./internal/voice/; go test -p 2 -race ./internal/voice/ | - | clean | - |

## Self-Verification Checklist

- [ ] Gate test passing untouched
- [ ] Fail-closed on unparseable and on every error class except ctx cancel
- [ ] No quota park path (grep for park/Park before reporting)
- [ ] Timeout bounded with context.WithTimeout per Check, not a shared deadline
- [ ] ASCII-only

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** paths
2. **Verify command + output:** last 20 lines
3. **Deviations:** none / list

## Review Checklist (For Review Agent)

- [ ] C6 shapes exact
- [ ] Fail-closed verified by mutation: flip the default-deny branch and watch the gate test go RED, restore
- [ ] Quota classification uses errors.Is on the real llm sentinels
- [ ] Pipelining possible: Check stateless per sentence (no hidden per-call state)

Output: APPROVED or specific gaps.

## Notes

The fallback line is config (filler-class, fact-free). Keep it out of the
supervisor prompt so the model cannot learn to trigger it.
