# Verification-Failure Model Escalation - Implementation Orchestrator

> **For the executing agent:** You are the orchestrator for this tree node.
> Your job: (1) dispatch implementation agents, (2) review their work,
> (3) re-dispatch if incomplete, (4) track completion.
> Do NOT implement code yourself. All implementation happens in leaf agents.

## Meta

- **Role:** Root
- **Parent:** none (root of tree 01 in the llm-resilience-forest)
- **Children:** 4 leaf documents under this node
- **Scope:** Per-agent model escalation: when adversarial verification
  exhausts `max_fix_loops`, the agent's next attempt re-resolves through a
  configured smarter model (alias or direct ref) instead of escalating to
  the user.

Read `../SHARED-CONVENTIONS.md` and `../DECISIONS.md` first. Decisions D1,
D2, D3, D14, D16 and audit findings ARCH-AUDIT-2026-09-01 B1/B2/R1 govern
this tree.

## Goal

Today, when verification fails after MaxFixLoops fix attempts, the
verification hook gives up and inserts a "manual review needed" message for
the user (internal/agent/verification_hook.go:119-170). The user wanted the
machine to escalate to a SMARTER MODEL instead: model X fails verification
twice-to-3x → the next attempt runs on the agent's `escalation_model`
(which may be an alias or a direct provider/model ref).

⚠️ **CRITICAL PREREQUISITE (audit B1): the hook pipeline this tree builds
on is INERT.** `HookRegistry.RunPrepareNextTurn` (internal/agent/hooks.go:267)
has zero production callers; loop.go runs only SessionStart/SessionEnd
hooks and reads no `TurnModification`. Leaf 02 wires the pipeline FIRST;
all later leaves assume it.

This tree delivers:

1. **Config surface** — `escalation_model` on AgentSpec (AGENT.md
   frontmatter via the internal/agents → internal/agent two-half
   pipeline, D16), resolvable to an alias or provider/model ref, with an
   optional defaults block for agents that do not set it.
2. **Hook pipeline wiring** — fire `RunPrepareNextTurn` per turn and
   apply returned modifications (ExtraMessages prepend; ModelOverride
   via the existing SetModelOverride seam).
3. **Trigger wiring** — when `handleFail` exhausts fix loops and an
   escalation model is configured, mark the escalation; only when NO
   escalation model is configured does the legacy escalate-to-user
   behavior run (unchanged).
4. **Resolution + lifecycle** — resolve the escalation ref through the
   Resolver (alias semantics, cooldowns, rotation all apply); hold the
   escalated model for the full escalated fix budget (R1: persistent
   override, cleared on fresh-turn detection); restore the base model on
   the next fresh turn; surface the escalation in logs, the routing log,
   and bus events.

## Architecture

Config flows AGENT.md frontmatter → agents.AgentMetadata (yaml parse) →
registry conversion (definitionToSpec + mergeSpec, D16) → AgentSpec → the
verification hook's spawner path. The hook receives `TurnState` (which
carries ModelRef) and calls `h.spawner.SpawnVerifier(ctx, prompt,
modelRef)`. Escalation reuses this seam: on loop exhaustion,
DecideEscalation resolves `EscalationModel` and the hook returns a
modification that the pipeline (leaf 02) applies — switching the fix
loop's model via the loop's existing model-override mechanism. The
override is held persistently for the escalated budget (R1: the loop's
one-shot override clearing would otherwise serve only ONE escalated
turn) and cleared on fresh-turn detection. Resolution failures (fallback
to base model + log) must never crash the loop.

## Interface Contracts

### Contract 1: AgentSpec.EscalationModel

```go
// File: internal/agent/spec.go (modify AgentSpec)
// EscalationModel is the model (alias name or "provider/model" ref) used
// for the next fix attempt when verification exhausts max_fix_loops.
// Empty = escalation disabled; hook falls back to escalate-to-user.
EscalationModel string `json:"escalation_model,omitempty" yaml:"escalation_model,omitempty"`
```

- Owner: 01-spec-config.md
- Consumers: 03-hook-wiring.md, 04-resolution-lifecycle.md

### Contract 2: Hook pipeline (loop-level, no new exported API)

```go
// internal/agent/loop.go (leaf 02 modifications):
// 1. Per-turn: hookRegistry.RunPrepareNextTurn(ctx, turnState) fires
//    once before each LLM call, at a seam documented with file:line
//    evidence in leaf 02's report.
// 2. applyTurnModification(loop, mod):
//    - ExtraMessages → prepend to the next call's messages
//    - ModelOverride → loop.SetModelOverride (existing seam, loop.go:1334;
//      one-shot consumption loop.go:3219-3246)
//    - zero-value mod = no-op; hook panic/error = log + continue
```

- Owner: 02-hook-pipeline.md
- Consumers: 03-hook-wiring.md, 04-resolution-lifecycle.md

### Contract 3: Escalation decision function

```go
// File: internal/agent/verification_escalation.go (new)
package agent

// ModelResolver resolves an alias name or "provider/model" ref to a
// usable model reference. ⚠️ Satisfied by *llm.Resolver via the method
// ResolveEscalationRef — NOT ResolveRef, which already exists on
// *llm.Resolver with a DIFFERENT signature (resolver.go:294,
// func (r *Resolver) ResolveRef(ref string) *ModelConfig; callers at
// loop.go:3220, model_parser.go:410, registry.go:1237). Audit B2: a
// same-name/different-signature method would not compile.
type ModelResolver interface {
    // ResolveEscalationRef returns a model ref string usable as a loop
    // model ref; error when the ref names no alias/model.
    ResolveEscalationRef(ref string) (string, error)
}

type EscalationDecision struct {
    Escalate   bool   // true = re-run turn on the escalation model
    ModelRef   string // resolved escalation ref; empty when !Escalate
    Reason     string // "fix_loops_exhausted" | "no_escalation_model" | "resolution_failed"
}

// DecideEscalation never errors; resolution failure degrades to
// Escalate=false with Reason "resolution_failed".
func DecideEscalation(spec *AgentSpec, resolver ModelResolver) EscalationDecision
```

- Owner: 03-hook-wiring.md
- Consumers: 04-resolution-lifecycle.md

### Contract 4: Escalation application + observability

```go
// File: internal/agent/verification_escalation.go (leaf 04 adds)
// ApplyEscalation consumes h.pendingEscalation: marks the escalation so
// the fix loop's model override is set PERSISTENTLY (R1: the loop's
// SetModelOverride is one-shot — cleared after first application;
// escalation must use the persistent variant, loop.go:1344
// SetPersistentModelOverride, held for the escalated budget — the
// escalated model gets its own full max_fix_loops attempts per Q2 — and
// cleared on fresh-turn detection), resets h.fixCount (Q2 default:
// reset), and emits:
//   - bus topic "agent.model_escalated" (WS bucket: agent_progress —
//     extend the topic-prefix match in transformBusEventToWS; today an
//     unknown topic falls to eventType "event", server.go:703)
//     payload: {agent_id, from_model, to_model, reason, fix_loops}
//   - routing-log entry (internal/llm/routing_log.go) reason "escalation"
// The escalated ref applies to THIS fix loop only; the next fresh turn
// re-resolves through the agent's base Model.
func ApplyEscalation(h *VerificationAutoTrigger, mod *TurnModification)
```

- Owner: 04-resolution-lifecycle.md
- Consumers: none (terminal)

## Child Document Index

| # | Document | Type | Dependencies | Est. Context | Concurrency |
|---|----------|------|-------------|-------------|-------------|
| 01 | 01-spec-config.md | leaf | none | 45K | A |
| 02 | 02-hook-pipeline.md | leaf | none | 55K | A |
| 03 | 03-hook-wiring.md | leaf | 01, 02 | 60K | B |
| 04 | 04-resolution-lifecycle.md | leaf | 01, 02, 03 | 70K | C |

**Concurrency groups:** A = {01, 02} — disjoint files (frontmatter/
spec vs loop.go/hooks), dispatch in parallel. B = {03} after both.
C = {04} last.

## Dispatch Protocol

### Phase 1: Dispatch leaves 01 + 02 (Group A, parallel)

1. **Read** 01-spec-config.md and 02-hook-pipeline.md; dispatch each via
   `delegate_task` with: full leaf text + its contract above +
   SHARED-CONVENTIONS §1, §3, §6 + relevant source INLINED (AgentSpec
   struct spec.go:74-119 for 01; the hook registry + loop call sites
   noted in 02's Context for 02).
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests,
     report results only."
   - Include: "Do NOT use read_file on existing source files — explore
     with search_files or terminal cat."

2. Review each in-session (main model, never a delegated subagent):
   - 01: `go build ./... && go test ./internal/agents/... ./internal/agent/ -run 'TestAgentSpec|TestEscalationModel' -v`
   - 02: `go test ./internal/agent/ -count=1` (full suite) + the new
     loop-hooks tests; confirm the seam documentation exists.
3. Commit on pass:
   - `feat(agent): escalation_model config surface (tree 01 leaf 01)`
   - `feat(agent): wire PrepareNextTurn hook pipeline into loop (tree 01 leaf 02)`

### Phase 2: Dispatch leaf 03 (Group B)

Dependencies: leaves 01+02 COMPLETE. Verify:
`go test ./internal/agent/ -run TestEscalation -v`.
Commit: `feat(agent): verification fix-loop exhaustion escalates to escalation_model (tree 01 leaf 03)`.

### Phase 3: Dispatch leaf 04 (Group C)

Dependencies: leaves 01+02+03. Verify:
`go test ./internal/agent/ -run TestEscalation -v`,
`go test ./internal/llm/ -run 'TestResolver|TestResolveEscalationRef' -v`,
`go run ./tools/analyzers/mutexio/... ./internal/agent/`,
`go run ./tools/analyzers/predid/... ./internal/agent/`.
Commit: `feat(agent): escalation model resolution, restoration, and observability (tree 01 leaf 04)`.

### Phase 4: Integration review

- End-to-end test: agent with `escalation_model` set, verifier fails 3x →
  the escalated budget (max_fix_loops more attempts) runs on the
  escalation ref; fresh turn restores base model; agent WITHOUT the
  field → today's user-escalation message, byte-identical.
- `make graphs` regenerates cleanly (new bus topic classified
  agent_progress).
- AGENTS.md reviewed for staleness (new field/convention) in the final
  commit.

## Review Checklist

- [ ] All tasks from each leaf document are implemented
- [ ] Interface contracts above satisfied exactly (field names, tags, signatures — including ResolveEscalationRef NOT ResolveRef)
- [ ] Tests written and passing; TDD followed
- [ ] Escalation disabled path is byte-identical to current behavior
- [ ] No scope creep; no debug artifacts; no line-number corruption
- [ ] `gofmt` clean; mutexio/predid analyzers pass on touched packages

Output: APPROVED or list of specific gaps.

## Coding Conventions

See `../SHARED-CONVENTIONS.md` §1 and §3 — pass the relevant sections in
every dispatch context. Summary: Go stdlib testing, in-package tests,
wrap errors with %w, comments cite DECISIONS.md IDs, no new deps.

## Completion Tracking Table

| Child | Status | Iterations | Review Notes |
|-------|--------|------------|-------------|
| 01-spec-config.md | PENDING | 0 | |
| 02-hook-pipeline.md | PENDING | 0 | |
| 03-hook-wiring.md | PENDING | 0 | |
| 04-resolution-lifecycle.md | PENDING | 0 | |

Status values: PENDING | IN_PROGRESS | IMPLEMENTED | REVIEWED | COMPLETE | BLOCKED

## Integration Test Plan

1. `go build ./...` clean.
2. `go test ./internal/agent/... ./internal/llm/... -count=1` green.
3. Adversarial harness: fake verifier always VerdictFail; spec with
   `escalation_model: "testalias"`; stub resolver. Assert: hook pipeline
   delivers the fix-loop modifications (leaf 02), the 4th attempt
   escalates, the escalated budget runs max_fix_loops attempts on the
   escalated ref (persistent override, R1), and a bus event fired.
4. Repeat with `escalation_model` empty; assert the legacy
   "Manual review needed" message and Reason "verification escalation" are
   unchanged.
5. `make analyzers` passes on touched packages.

## Structural Completeness Check (Before Dispatch)

Run after authoring all leaves:
`python3 ~/.hermes/skills/software-development/hierarchical-planning/scripts/check_template_compliance.py docs/plans --strict-leaves | grep 01-escalation`
Fix any reported gaps before dispatching.

## Notes

- D14: no roster AGENT.md sets `model:` today. Leaf 01 MAY add an
  employees-style defaults block only if the config plumbing is trivial;
  otherwise per-agent `escalation_model` alone satisfies this tree. State
  the choice in the leaf's Deviations.
- The verification hook's `h.fixCount` reset semantics (Q2) default to
  RESET on escalation — record as a comment citing DECISIONS.md Q2.
- R1 constraint (frozen): the escalated model must get a FULL budget of
  fix attempts. Leaf 04 uses the loop's persistent-override variant and
  clears it on fresh-turn detection — one-shot overrides are
  insufficient; do not downgrade to SetModelOverride.
- The hook pipeline (leaf 02) will also activate the currently-dead
  security/secret hooks registered in components.go (:1245, :1263). This
  is a deliberate, flagged side effect: leaf 02 notes it in Deviations;
  if those hooks misbehave when activated, that is THEIR ticket, not a
  revert reason for the pipeline.
- Do not touch the Ralph loop (`ralph_loop.go`) in this tree; Ralph
  replan-iteration escalation is out of scope (D2 scoped the trigger to
  verification.max_fix_loops).
