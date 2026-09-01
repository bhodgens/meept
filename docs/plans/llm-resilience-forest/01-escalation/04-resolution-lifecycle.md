# Escalation Model Resolution + Lifecycle - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Resolve the escalation ref through *llm.Resolver, actually
  switch the fix-loop turn's model, restore the base model afterward, and
  emit bus + routing-log observability.
- **Dependencies:** 01-spec-config.md, 02-hook-wiring.md
- **Estimated Context:** 70K
- **Concurrency Group:** C
- **Decision references:** D1, D2, D3, Q2

## Goal

Make escalation REAL: the 4th verification attempt (after max_fix_loops
exhaustion) executes on the escalation model. This leaf:

1. Implements `ResolveEscalationRef` on `*llm.Resolver` (alias →
   ResolveForAlias path; `provider/model` → direct config lookup),
   satisfying the ModelResolver interface from leaf 03. ⚠️ Name is
   ResolveEscalationRef, NOT ResolveRef: `*llm.Resolver` already defines
   `ResolveRef(ref string) *ModelConfig` (resolver.go:294, callers at
   loop.go:3220, model_parser.go:410, registry.go:1237) — audit B2; the
   planned name would be a duplicate-method compile error.
2. Applies `h.pendingEscalation` to the fix-loop TurnModification so the
   next loop iteration runs on the escalated model ref — set the
   EXISTING `TurnModification.ModelOverride` field (hooks.go:53; no
   struct extension needed). Loop seam: how
   the loop picks its model per turn — follow ModelRef through TurnState /
   WithModelRef, internal/agent/registry.go:375-376 pattern.
3. Holds the override for the FULL escalated budget (audit R1): the
   loop's `SetModelOverride` is one-shot (cleared after first
   application, loop.go:3244-3246), so the escalated model would get
   exactly ONE turn of its max_fix_loops budget. Use the persistent
   variant (`SetPersistentModelOverride`, loop.go:1344) for the
   escalated window and clear it on fresh-turn detection (the next turn
   that arrives WITHOUT a pending escalation modification). Escalation
   is per-fix-loop, not sticky across turns.
4. Emits `agent.model_escalated` on the bus (WS bucket: agent_progress —
   add the topic-prefix case in transformBusEventToWS: today an unknown
   topic falls to eventType "event", server.go:703; verify with
   `make graphs`) and a routing-log entry (internal/llm/routing_log.go)
   with reason "escalation".

## Context

Key files:
- `internal/llm/resolver.go` — ResolveForAlias (resolver.go:352;
  timeout defaulting at :84-89), RecordAliasFailure/Success, capability
  escalation precedent at resolver.go:269 ("Escalated to model for
  skill", Reason "capability_escalation") — mirror its log shape.
  NOTE: Resolver.ResolveRef (resolver.go:294) ALREADY EXISTS with a
  different signature (returns *ModelConfig, no error) — the leaf's
  new accessor must not collide with it; name accordingly.
- `internal/llm/routing_log.go` — existing routing-log entries; add the
  escalation reason without changing existing reasons' semantics.
- `internal/agent/verification_hook.go` — h.pendingEscalation from leaf
// 03
  02; the SpawnVerifier call site (line ~84) receives modelRef via
  `h.config.EffectiveModel(state.ModelRef)` — escalation overrides the
  ref passed to the NEXT SpawnVerifier and to the agent's own next fix
  turn (both, consistently: the whole fix loop runs escalated).
- `internal/comm/http/server.go` — transformBusEventToWS topic bucketing.
- `internal/bus/` — publish pattern (collect-under-lock not needed here;
  follow existing hook publishes if any exist).

Alias semantics: escalation through an alias INHERITS alias rotation,
cooldowns, and quota blocks. If the escalation alias is fully
quota-blocked (ErrAllModelsQuotaBlocked), the fix attempt fails with that
error and the loop's EXISTING quota handling takes over — do NOT add a
second handling path (SHARED-CONVENTIONS §2).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/llm/resolver.go (add)
// ResolveEscalationRef resolves an alias name or "provider/model" ref to a model
// ref string. Implements agent.ModelResolver. Name avoids the collision
// with the existing Resolver.ResolveRef (resolver.go:294) — audit B2.
func (r *Resolver) ResolveEscalationRef(ref string) (string, error)

// File: internal/agent/verification_escalation.go (add, from leaf 03 contract)
// ApplyEscalation consumes h.pendingEscalation: swaps the model ref for
// the pending fix iteration, emits bus + routing-log, clears the pending
// decision. Idempotent when pending is empty (no-op).
func ApplyEscalation(h *VerificationAutoTrigger, mod *TurnModification)

// Bus topic (string constant in this leaf):
const TopicAgentModelEscalated = "agent.model_escalated"
// payload: {agent_id, from_model, to_model, reason, fix_loops}
// WS classification: agent_progress (NEVER chat_message).
```

### What This Leaf Consumes

```go
// From 02-hook-wiring.md
type ModelResolver interface { ResolveEscalationRef(ref string) (string, error) }
// h.pendingEscalation Escalation Decision stored by leaf 03
```

## Tasks

### Task 1: Resolver.ResolveEscalationRef

**Objective:** Single resolution entry point for both ref forms.

**Files:**
- Modify: `internal/llm/resolver.go`
- Test: `internal/llm/resolver_escalation_test.go` (new file — the name
  does not exist in the repo today)

**Step 1:** Failing tests: alias name resolves through registered alias
(use existing resolver test fixtures); "provider/model" resolves a
direct config entry; unknown alias → error; unknown direct ref → error;
mutexio-safe (no I/O under lock — resolution paths already comply; run
the analyzer).

**Step 2:** FAIL. **Step 3:** Implement (alias branch delegates to the
existing alias lookup; direct branch validates against the model
registry/config). **Step 4:** PASS + analyzers clean.

### Task 2: Wire resolver into the hook + model switch

**Objective:** The escalated fix iteration actually runs on ModelRef.

**Files:**
- Modify: `internal/agent/verification_hook.go` (+ construction site in
  the daemon components wiring — search_files for
  `NewVerificationAutoTrigger` / hook construction to find the wiring).
- Modify: `internal/agent/verification_escalation.go` (ApplyEscalation)
- Test: `internal/agent/verification_hook_test.go`

**Step 1:** Failing test: with pendingEscalation set and a stub resolver
satisfying the interface, the next SpawnVerifier receives the ESCALATED
ref (assert on the ref passed to a stub spawner) and
`h.pendingEscalation` clears after application; second call is a no-op.

**Step 2:** FAIL. **Step 3:** Implement ApplyEscalation + hook plumbing;
base-model restoration is implicit — the override applies only to the
pending iteration's ref construction, and fresh turns rebuild ModelRef
from the spec (assert this in the test: post-escalation fresh state
resolves to the BASE ref). **Step 4:** PASS.

### Task 3: Observability (bus + routing log)

**Objective:** Escalations are visible and auditable.

**Files:**
- Modify: `internal/agent/verification_escalation.go` (publish + log)
- Modify: `internal/llm/routing_log.go` (accept reason "escalation")
- Modify: `internal/comm/http/server.go` ONLY if the default topic
  bucketing misclassifies the new topic (verify first).
- Test: `internal/agent/verification_escalation_test.go` (bus stub
  capturing publishes), `internal/llm/routing_log_test.go` (new reason).

**Step 1:** Failing tests: publish payload keys exactly
{agent_id, from_model, to_model, reason, fix_loops}; routing log row with
reason "escalation". **Step 2:** FAIL. **Step 3:** Implement.
**Step 4:** PASS.

### Task 4: Graph + docs sync

**Objective:** Generated artifacts and docs reflect the new topic.

**Files:**
- Run: `make graphs` (regenerates docs/generated/*; commit result).
- Modify: `docs/workflows/agent.md` or the verification workflow doc —
  locate via search_files for the verification-hook workflow page; add an
  escalation section (trigger: max_fix_loops exhausted; target:
  escalation_model; observability).
- Modify: `AGENTS.md` — review the Critical Invariants for any needed
  line (new bus topic classification), per the repo's same-commit rule.

**Verify:** `make graphs-check` passes; docs name the topic and bucket.

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Escalation alias inherits rotation/cooldown/quota semantics (no second handling path; ErrAllModelsQuotaBlocked propagates)
- [ ] Base model restored for fresh turns (tested)
- [ ] Bus topic lands in agent_progress; `make graphs` clean
- [ ] Routing-log reason "escalation" present; existing reasons untouched
- [ ] gofmt/vet/analyzers clean

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contracts match master.md Contract 3 exactly (topic name, payload keys, WS bucket)
- [ ] No sticky escalation across turns (tested)
- [ ] No duplicated quota handling
- [ ] `make graphs` output committed; AGENTS.md reviewed in-scope

Output: APPROVED or specific gaps with file + line references.

## Notes

- The Resolver holds alias health under one mutex; ResolveEscalationRef must not
  call out to I/O while holding it (SHARED-CONVENTIONS §2 mutex scope).
  Existing resolution helpers already follow this — reuse, don't fork.
- SpawnVerifier receives `h.config.EffectiveModel(state.ModelRef)` — if
  overriding the ref for the pending iteration is cleaner at THAT call
  site than inside ApplyEscalation, either placement is acceptable;
  document the choice in code with a comment citing D3.
