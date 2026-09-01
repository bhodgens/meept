# Approval actuator: approved plans → applyProposal - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Bridge approved skill-evolver plans to the evolver's apply
  path: `ApplyApprovedPlan` dispatching by action, wired into the existing
  plan approval flow.
- **Dependencies:** 01-evolver-plan-sink.md (provenance), 02-skill-root-resolution.md
  (tier-resolved Writer) — both must be REVIEWED before dispatch.
- **Estimated Context:** 70K
- **Concurrency Group:** B

## Goal

`ApprovePlan` (internal/plan/manager.go:156) only records a signoff and
triggers generic plan synthesis. Nothing bridges an approved skill-evolution
plan back to the evolver's `applyProposal`
(internal/skills/lifecycle/evolver.go:697) or `Writer.ArchiveSkill`
(internal/skills/lifecycle/writer.go:309). Grep confirms **zero consumers
of approved evolver plans** — the human gate has no actuator. This leaf
adds it: an approved plan carrying evolver provenance (leaf 01) triggers
`ApplyApprovedPlan`, which dispatches by action to the tier-resolved Writer
(leaf 02) or the applyProposal path.

## Context

Verified facts (2026-08-31, do not re-derive):

- `internal/plan/manager.go:156` — `ApprovePlan` records signoff + generic
  synthesis; no evolver dispatch.
- `internal/skills/lifecycle/evolver.go:697` — `applyProposal` exists but
  is only reached via auto_apply (currently false) — if your exploration
  shows it is reached some other way, note it.
- `internal/skills/lifecycle/writer.go:309` — `ArchiveSkill`, now
  tier-resolved per leaf 02.
- Provenance from leaf 01: evolver plans carry origin "skill-evolver" +
  proposal_id + action, readable via leaf 01's accessors.
- Runtime: `auto_apply=false` stays. This leaf adds the actuator BEHIND the
  human gate — it does not weaken or bypass the gate.

Key files to understand before implementing:

- `internal/plan/manager.go` — ApprovePlan, its callers (search for who
  invokes approval — CLI/TUI/HTTP handler), and what state a plan carries
  after approval (signoff recorded where? status field?).
- `internal/skills/lifecycle/evolver.go` — CreatePlan site (:604),
  applyProposal (:697), existing proposal-state tracking (is there an
  "applied" marker? idempotency material?), and the Evolver struct's
  dependencies (does it already hold the PlanManager or plan dir?).
- Leaf 01/02 diffs (git diff HEAD if not yet committed, else the commits) —
  exact accessor names and Writer resolution semantics to build against.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// internal/skills/lifecycle/evolver.go
// ApplyApprovedPlan applies an approved evolver-origin plan:
//   func (e *Evolver) ApplyApprovedPlan(planPath string) error
//   - requires provenance origin == "skill-evolver" (leaf 01 accessors);
//     non-evolver plans are rejected, not applied
//   - dispatch by action:
//       archive -> Writer.ArchiveSkill (tier-resolved per leaf 02)
//       refine  -> the applyProposal path (evolver.go:697)
//   - idempotent: an already-applied proposal is a no-op (verify existing
//     proposal state tracking before coding)

// Wiring (AGENTS.md wiring requirement): after a successful ApprovePlan
// (internal/plan/manager.go:156), a plan with evolver provenance triggers
// ApplyApprovedPlan. Choose the seam after reading how approval is
// surfaced today (ApprovePlan caller path, or a plan-approved
// callback/event if one exists). The actuator failure MUST NOT corrupt
// plan approval state: log + record the failure on the plan, do not
// rollback the signoff.

// Audit: every application logs one line:
//   applied evolver plan <file> action=<a> proposal=<id> result=<ok|err>
```

### What This Leaf Consumes

```
// Leaf 01: provenance accessors (origin/proposal_id/action on a plan)
// Leaf 02: tier-resolved Writer.ArchiveSkill
// internal/plan/manager.go:156 — ApprovePlan + its callers
// internal/skills/lifecycle/evolver.go:604, :697 — CreatePlan, applyProposal
```

## Tasks

### Task 1: ApplyApprovedPlan dispatch

**Objective:** The actuator function with provenance check + action
dispatch.

**Files:**
- Modify: internal/skills/lifecycle/evolver.go
- Test: `internal/skills/lifecycle/evolver_apply_test.go` (new)

**Step 1: Write failing test**

Temp-HOME + temp-repo fixtures:

- Non-evolver plan (no provenance) → rejected with a clear error, nothing
  applied (skill untouched, no Writer calls).
- Archive action → ArchiveSkill invoked; fixture skill in a non-default
  tier gets archived (uses leaf 02 semantics).
- Refine action → applyProposal path exercised (use whatever seam
  applyProposal offers for testing; if it is unexported and hard to
  observe, assert on its observable side effects — skill content change).
- Create action → if applyProposal handles create, same treatment; if the
  create path needs inputs the plan doesn't carry, REJECT with a clear
  error and record the gap in deviations (do not invent creation inputs).
- Already-applied proposal → no-op, nil error.
- Plan file missing/unreadable → error naming the path.

**Step 2: Run test to verify failure**
Run: `go test ./internal/skills/lifecycle/ -run TestApplyApprovedPlan -v`
Expected: FAIL (undefined)

**Step 3: Write minimal implementation**

Per the contract. Idempotency: if the evolver already tracks applied
proposals (search for applied/processed state), reuse it; otherwise record
application on the plan file itself (a status the plan format supports —
coordinate with leaf 01's provenance encoding, do not invent a parallel
marker file).

**Step 4: Run test to verify pass**
Expected: PASS

### Task 2: Approval wiring

**Objective:** ApprovePlan on an evolver plan triggers the actuator.

**Files:**
- Modify: the ApprovePlan caller seam (locate it; candidates: the
  manager's approval method gaining an optional post-approve hook, or the
  daemon/CLI approval handler dispatching). Choose the MINIMAL seam that
  avoids an import cycle (internal/plan must not import
  internal/skills/lifecycle — if a hook/callback is needed, define it in
  internal/plan and inject from the daemon).
- Test: wiring-level test in the seam's package

**Step 1: Write failing test**

- Approving an evolver-provenance plan via the real approval path invokes
  ApplyApprovedPlan (assert via the applied side effect or an injected
  spy hook).
- Approving a human-authored plan (no provenance) → generic approval
  behavior ONLY, no actuator call.
- Actuator error → approval still succeeds (signoff recorded), failure is
  logged + recorded on the plan.

**Step 2: Run test to verify failure**

**Step 3: Write minimal implementation**

Wire through the daemon construction path (internal/daemon/components.go)
if injection is needed. No import cycles. auto_apply stays false; this
path fires only on explicit approval.

**Step 4: Run test to verify pass**
Run: `go test ./internal/plan/ ./internal/daemon/ -run TestApprovalWiring -v`

### Task 3: Audit line

**Objective:** One audit log line per application attempt.

**Files:**
- Modify: same file as Task 1
- Test: covered in Task 1's tests (assert the log line via the project's
  logging capture pattern; if there is no capture pattern, assert on the
  logger call seam or skip with a deviation note)

Format exactly:
`applied evolver plan <file> action=<a> proposal=<id> result=<ok|err>`

## Self-Verification Checklist

Before reporting completion, verify:

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts (above) satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations from spec (or deviations documented below)
- [ ] No scope creep — only what the tasks specify
- [ ] No import cycle introduced (internal/plan does not import
      internal/skills/lifecycle)
- [ ] Human-authored plan approval behavior unchanged (regression green)
- [ ] auto_apply semantics untouched — actuator fires only on approval
- [ ] Non-evolver plans can never trigger application

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

The review agent will verify against this leaf document:

- [ ] Every Task above is implemented
- [ ] Every test in the task is present and passing
- [ ] Interface contracts match (ApplyApprovedPlan signature, dispatch by
      action, idempotency, provenance guard)
- [ ] Wiring reaches the actuator from the real approval path (not just
      unit-invoked)
- [ ] Actuator failure cannot corrupt approval state
- [ ] Audit line format exact
- [ ] No scope creep beyond specified tasks

Output: APPROVED or list of specific gaps with file + line references.

## Notes

- The wiring seam decision (hook vs caller-side dispatch) is this leaf's
  main design risk. Read the approval callers FIRST; pick the seam with
  the smallest blast radius. Report the choice and rationale.
- If approval is only reachable via code/API today (no TUI/CLI surface for
  plan approval), wire the code path and report the missing surface as a
  gap — per master.md Open Questions, do NOT build a new UI in this tree.
- Idempotency matters because approval may be re-invoked (retries, repeat
  clicks). Double-application of an archive would be destructive to a
  live skill dir — this is the one place to be conservative.
