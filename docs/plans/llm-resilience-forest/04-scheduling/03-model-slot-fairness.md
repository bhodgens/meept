# Model-Slot Fairness Audit (or Two-Lane Acquire) - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit — the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files — explore with search_files or terminal cat. After writing
> a file, do NOT read it back to verify — write once and stop. After
> completing, report what you built, what files you touched, and any
> deviations from the spec.

## Meta

- **Parent:** ../master.md
- **Scope:** Audit whether model-concurrency acquisition has a wait list
  that interactive acquires can jump; implement two-lane acquire if so,
  document the queue-layer-only gap honestly if not.
- **Dependencies:** 02-queue-priority.md
- **Estimated Context:** 60K
- **Concurrency Group:** C
- **Decision references:** D11

## Goal

The user asked for interactive priority "vis-a-vis concurrent connection
per model". MaxConcurrency is enforced by a raw channel semaphore
(internal/llm/client.go:269-270: `c.concurrencySemaphore = make(chan
struct{}, config.MaxConcurrency)`). A channel semaphore has NO
addressable wait list — waiters queue in Go runtime FIFO order and
cannot be reordered. This leaf RESOLVES the question honestly:

**Phase A — audit (mandatory, produces the decision):**
1. Map every acquire/release site of concurrencySemaphore (and any
   other model-concurrency gate: in-use tracking in internal/llm/inuse.go,
   provider-level limits in providers.go:54 MaxConcurrency).
2. Determine: is there ANY structure where waiting requests are
   enumerable (a slice/queue of waiters, a priority-ready select loop)?
   Almost certainly no (raw channel) — but PROVE it from source, not
   assumption.

**Phase B — implement ONE outcome:**
- **Outcome 1 (wait list exists somewhere):** implement two-lane
  acquire: `Acquire(ctx, cfg, priority bool)`; interactive acquires
  dequeue before background ones; starvation guard: after 3 consecutive
  interactive grants, one background grant releases (bounded jump).
- **Outcome 2 (raw channel, no wait list):** implement a small
  `slotGate` replacement for the semaphore that HAS a wait list
  (mutex + two FIFO lanes + condition broadcast), same
  acquire/release semantics, zero overhead when uncontended, and
  interactive priority. This is ~80 lines and is the RECOMMENDED
  outcome — queue-layer priority alone does not deliver the user's
  "concurrent connection per model" requirement.
- Either way: document in docs/workflows/llm-management.md what interactive
  priority means at each layer (queue ordering from leaf 02; slot
  fairness from this leaf; chat turns are never queued).

## Context

Key files:
- `internal/llm/client.go:265-275` — semaphore construction; the
  request-path gate is `acquireConcurrencyLimit` (client.go:1760,
  acquired at :912 and :1374), fed by `MaxConcurrency` (client.go:249
  WithConcurrencyLimit, :268-269 config path); find every
  `<-c.concurrencySemaphore` acquire and `c.concurrencySemaphore <-`
  release (grep the channel name). providers.go:54 MaxConcurrency is
  CONFIG ONLY — no gate exists there today.
- `internal/llm/inuse.go` — AUDIT FACT (2026-09-01): this is
  `BuildModelsInUse` — a BOOT-TIME startup gate (computes the
  provider/model set RuntimeManager uses to skip starting unused local
  endpoints; runtime_manager_test.go:429+). It has NO acquire/release
  and NO request-path role. RULE IT OUT as a gate site in the audit
  report; the only request-path concurrency gate is the channel
  semaphore (see below).
- `internal/llm/providers.go:54` — provider MaxConcurrency; the same
  gate may exist per provider — audit covers both scopes (model +
  provider).
- How the interactive bit reaches the client: the Chat call chain.
  The call must carry priority — likely a ChatOption
  (`llm.WithPriority(...)`) or a field on the resolved request context.
  Trace ONE chat turn from the agent loop to doRequest and pick the
  seam with the least surface change; document the choice.

## Interface Contracts (From Parent)

### What This Leaf Exposes

Exactly master Contract 3, outcome-dependent:

```go
// Outcome 2 (recommended) — internal/llm/slot_gate.go:
type slotGate struct{ /* mutex, lanes [2][]chan struct{}, held int, cap int */ }
func newSlotGate(cap int) *slotGate
func (g *slotGate) acquire(ctx context.Context, priority bool) error // nil on grant; ctx.Err on cancel
func (g *slotGate) release()
// Client call seam: the acquire site passes priority from the request's
// interactive flag (plumbed via ChatOption or request-context field —
// the seam audit picks ONE; frozen in the leaf report).
// Outcome 1: Acquire(ctx, cfg, priority bool) on the existing structure.
```

### What This Leaf Consumes

```go
// From 02: nothing directly (the Interactive job flag is queue-side);
// this leaf's priority input comes from the CALLING session/turn, not
// the job — state the source explicitly in the leaf report.
```

## Tasks

### Task 1: Acquisition audit

**Objective:** The evidence base for the outcome decision.

**Files:**
- Create: `docs/plans/llm-resilience-forest/04-scheduling/audit-slot-fairness.md`
  (the leaf's written report — this is a deliverable, not prose drift)

**Step 1:** Grep all semaphore/in-use acquisition sites; for each: file
+ line, blocking or try-acquire, wait-list structure (yes/no), scope
(model/provider). **Step 2:** Write the report with the OUTCOME
decision (1 or 2) and the chosen priority-input seam. **Step 3:** The
orchestrator reviews the report BEFORE Phase B dispatch continues (this
leaf pauses here for review if dispatched as one agent — if the
implementing agent is confident in Outcome 2 per the master's
recommendation, it may proceed and note the audit confirmation).

### Task 2: slotGate implementation (Outcome 2 path)

**Objective:** A fair, interruptible, two-lane semaphore.

**Files:**
- Create: `internal/llm/slot_gate.go`
- Test: `internal/llm/slot_gate_test.go`

**Step 1:** Failing tests (concurrency-safe): grant up to cap; block at
cap; release wakes ONE waiter; interactive waiter granted before
background waiters that arrived earlier; starvation guard (3
interactive → 1 background); ctx cancel while waiting returns error
and dequeues; uncontended acquire/release has no allocation.
Race-test: `go test -race -run TestSlotGate`. **Step 2:** FAIL.
**Step 3:** Implement. **Step 4:** PASS including -race.

### Task 3: Client integration

**Objective:** The semaphore swaps for the gate; priority flows.

**Files:**
- Modify: `internal/llm/client.go` (construction + acquire/release sites)
- Modify: the Chat seam chosen in Task 1 (ChatOption or context field)
- Test: `internal/llm/client_test.go` — MaxConcurrency=1, background
  request in flight, interactive + background queued → interactive
  acquires next; priority-less callers (default false) behave as today.

**Step 1:** Failing tests. **Step 2:** FAIL. **Step 3:** Implement —
NO behavior change for existing callers that never pass priority.
**Step 4:** PASS + `go test ./internal/llm/... -count=1 -race`.

### Task 4: Docs

**Files:**
- Modify: `docs/workflows/llm-management.md` — the two-layer priority story
  (queue + slots) and what chat turns do (never queued; slots only).
- Modify: `AGENTS.md` ONLY if a new convention emerged (slot gate) —
  same-commit rule.

## Self-Verification Checklist

- [ ] Audit report exists with per-site evidence and the outcome decision
- [ ] -race clean on gate + client tests
- [ ] Priority-less path byte-identical to today (regression test)
- [ ] Starvation guard tested; ctx-cancel dequeues cleanly (no leaked waiters)
- [ ] gofmt/vet/analyzers clean

**DO NOT COMMIT.**

**Deviations from spec:** [none / list any with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task implemented; tests present and passing (including -race)
- [ ] Contract matches master Contract 3 (chosen outcome documented)
- [ ] No second concurrency mechanism left alongside the gate (old channel removed or repurposed, not duplicated)
- [ ] Priority input seam is ONE place, documented

Output: APPROVED or specific gaps with file + line references.

## Notes

- The agent loop's chat turns are the interactive callers;
  specialist/goal loops are background. If the seam makes
  per-turn priority explicit, wire chat=true, everything else=false —
  resist inventing a third priority tier (D11 scope).
- CROSS-LAYER DIVERGENCE (state this explicitly in Task 4's docs,
  audit 2026-09-01): the two priority layers read DIFFERENT signals —
  the queue flag is stamped at enqueue from the originating SESSION
  (tree 04 leaf 02), while the slot gate reads the CALLING TURN
  (chat=true, queue work=false). Consequence: an Interactive=true
  planner job that wins its claim still acquires slots with
  priority=false (it is a queue turn, not chat) — interactive queue
  work can still wait behind background chat turns at MaxConcurrency.
  Document this as intentional scope (queue-jobs are not
  slot-prioritized in this tree), not an oversight.
- If the audit finds provider-level AND model-level gates, gate ONLY at
  the model level for this leaf; provider-level priority is a follow-up
  (note it in the audit report).
