# Fix-Loop Exhaustion Escalation Wiring - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** When verification fix loops exhaust, decide escalation
  (DecideEscalation) and route the decision into handleFail so the next
  fix attempt carries the escalation marker.
- **Dependencies:** 01-spec-config.md (AgentSpec.EscalationModel exists)
- **Estimated Context:** 60K
- **Concurrency Group:** B
- **Decision references:** D1, D2, Q2

## Goal

Insert the escalation DECISION at the exact seam where the hook today
gives up to the user: `handleFail` in
internal/agent/verification_hook.go:119-170. Behavior:

- fixCount <= maxLoops → normal fix loop (unchanged).
- fixCount > maxLoops AND spec.EscalationModel != "" →
  DecideEscalation returns Escalate=true; handleFail returns a
  TurnModification whose Reason is "verification model escalation" and
  whose ExtraMessages instruct the agent that this attempt runs on the
  escalation model, with the verifier findings attached. The actual model
  switch is leaf 04's job — this leaf defines and calls the decision seam
  and, when the resolver stub says resolution failed, falls back to the
  legacy user-escalation path.
- fixCount > maxLoops AND no escalation model → TODAY's behavior,
  byte-identical (message text, Reason "verification escalation").

## Context

Key files:
- `internal/agent/verification_hook.go` — VerificationAutoTrigger; handleFail
  at line 119; `h.fixCount` reset to 0 on pass/partial/escalation
  (lines 100-105, 129). The hook must have access to the owning AgentSpec
  (check how the hook is constructed in components wiring; if the spec is
  not currently reachable, add a spec field to the hook struct with a nil
  guard setter — `SetAgentSpec` pattern per repo setter rules).
- `internal/agent/verification_config.go` — VerificationConfig (MaxFixLoops).
- Leaf 01 delivered `AgentSpec.EscalationModel`.

Design constraint: the resolver is `*llm.Resolver` (internal/llm).
AUDIT M1 (2026-09-01): verification_hook.go ALREADY imports llm (line 12),
so the "must not import llm" framing is moot — the `ModelResolver`
interface below is retained anyway because *llm.Resolver's suitable
method would collide by name (audit B2) and a narrow interface keeps the
hook testable with stubs. Satisfied in leaf 04 by the resolver.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// File: internal/agent/verification_escalation.go (new)
package agent

// ModelResolver resolves an alias name or "provider/model" ref to a
// usable model reference. Satisfied by *llm.Resolver (leaf 04) and tests.
type ModelResolver interface {
    // ResolveEscalationRef returns a model ref string usable as a loop
    // model ref. Named ResolveEscalationRef (audit B2): Resolver already
    // has ResolveRef(ref string) *ModelConfig at resolver.go:294 — a
    // same-name/different-signature method would not compile.
    // Returns an error when the ref names no alias/model.
    ResolveEscalationRef(ref string) (string, error)
}

// EscalationDecision is what handleFail should do on loop exhaustion.
type EscalationDecision struct {
    Escalate   bool
    ModelRef   string
    Reason     string // "fix_loops_exhausted" | "no_escalation_model" | "resolution_failed"
}

// DecideEscalation never errors; resolution failure degrades to
// Escalate=false with Reason "resolution_failed".
func DecideEscalation(spec *AgentSpec, resolver ModelResolver) EscalationDecision
```

### What This Leaf Consumes

```go
// From 01-spec-config.md
AgentSpec.EscalationModel string
```

## Tasks

### Task 1: ModelResolver + EscalationDecision + DecideEscalation

**Objective:** Pure decision logic, fully unit-tested with stubs.

**Files:**
- Create: `internal/agent/verification_escalation.go`
- Test: `internal/agent/verification_escalation_test.go`

**Step 1:** Failing tests (table-driven):
- spec empty / nil spec → Escalate=false, Reason "no_escalation_model".
- spec with ref + resolver resolving → Escalate=true, ModelRef=resolved.
- resolver error → Escalate=false, Reason "resolution_failed".

**Step 2:** FAIL (types missing).

**Step 3:** Implement per contract.

**Step 4:** PASS.

### Task 2: Wire the decision into handleFail

**Objective:** On exhaustion, call DecideEscalation; branch on it.

**Files:**
- Modify: `internal/agent/verification_hook.go` (handleFail, lines 127-170)
- Test: `internal/agent/verification_hook_test.go` (extend; follow
  existing hook tests' construction pattern)

**Step 1:** Failing tests:
- exhausted + Escalate=true → TurnModification.Reason ==
  "verification model escalation"; ExtraMessages[0].Content contains both
  the escalation-model instruction and the last verifier output; the
  modification carries the decision (leaf 04 will extend the struct —
  this leaf stores the decision on the hook as `h.pendingEscalation`).
- exhausted + Escalate=false (any Reason) → legacy message EXACTLY:
  "Adversarial verification failed after %d fix attempts. Manual review
  needed." with Reason "verification escalation" — copy the current
  format string verbatim.
- Legacy path regression test: whole flow with EscalationModel empty
  produces identical output to before this leaf (golden-substring
  compare).

**Step 2:** FAIL.

**Step 3:** Implement: hoist the spec (or EscalationModel string) onto
the hook at construction; call DecideEscalation in the exhausted branch;
on Escalate, build the new modification and reset `h.fixCount = 0` with a
comment citing DECISIONS.md Q2 (reset default) — the escalated model gets
its own full max_fix_loops budget.

**Step 4:** PASS; `go test ./internal/agent/ -count=1` fully green.

### Task 3: Setter hygiene

**Objective:** If the hook gained a `SetAgentSpec`-style setter, apply
repo setter rules.

**Files:**
- Modify: `internal/agent/verification_hook.go`
- Test: covered by Task 2 tests + repo setter test convention
  (see internal/tools/builtin/setters_test.go for the pattern).

Nil-guard the setter; two-value assertions; no ignored errors. Run
`go run ./tools/analyzers/fieldguard/... ./internal/agent/` and note any
finding (fix or justify in Deviations).

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Disabled path byte-identical (message text + Reason)
- [ ] DecideEscalation never returns an error and never panics on nil inputs
- [ ] fixCount reset on escalation, commented with Q2 reference
- [ ] gofmt clean; go vet clean; no artifacts

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing
- [ ] Contract signatures match master.md Contract 2 exactly
- [ ] No second frontmatter/model-parsing path introduced
- [ ] Legacy behavior preserved exactly (diff the format strings)
- [ ] No scope creep into model switching (that is leaf 04)

Output: APPROVED or specific gaps with file + line references.

## Notes

- The hook's TurnModification already has a `ModelOverride string` field
  (hooks.go:53) — leaf 04's ApplyEscalation SETS that field; it does not
  extend the struct. This leaf must compile WITHOUT
  touching TurnModification's definition — keep the decision in
  `h.pendingEscalation` so leaf 04 has a clean seam.
- If the hook cannot reach the AgentSpec at its construction site
  without large plumbing changes, accept passing just the
  `EscalationModel` string into the constructor and record that in
  Deviations.
