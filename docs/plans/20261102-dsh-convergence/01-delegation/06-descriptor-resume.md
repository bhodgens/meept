# 06-descriptor-resume - Durable Child Descriptor + Cold Resume

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The gate tests for this branch
> (00T-tests.md) ALREADY EXIST and are committed — your job is to make them
> pass; do NOT delete, skip, weaken, or rewrite any gate test (report
> suspected-wrong gate tests to the orchestrator instead). Do NOT commit —
> the orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files — explore with search_files or terminal
> cat. After writing a file, do NOT read it back — write once and stop.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** A durable per-child descriptor (mode/provider/model/reasoning/
  seed, versioned) persisted with the child record, and a cold-resume entry
  that re-spawns an interrupted/idle delegation from its descriptor without
  loading a live Agent. Implements B6.
- **Dependencies:** 01-spawn-async (B1 ChildRecord.Descriptor + SetDescriptorFactory/SetPersist), 02-fork-seed (B2 seed durability), 00T-tests (gate F6)
- **Estimated Context:** 60K
- **Concurrency Group:** E (after 01, 02, 03 REVIEWED)
- **Audit references:** dsh-audit GAP 3(+4) (durable descriptor + cold resume), subagent/descriptor + manager.coldResume

## Goal

dsh persists a per-child `descriptor` event (mode/provider/label/
agentProvider/agentModel/reasoning/persona) so a child can be cold-resumed
from its durable record without loading a live Agent
(subagent/descriptor.ts:48-86,230-269; manager.coldResume manager.ts:1349-1404).
meept has no durable child record and no resume path — an interrupted
delegation is simply lost. This leaf adds `ChildDescriptor` (versioned,
JSON-durable), stamps it onto the B1 `ChildRecord.Descriptor` for every
spawn, persists child records through the daemon-owned store, and exposes
`DelegateManager.Resume(childID)` that re-spawns an interrupted/idle child
from its descriptor (re-playing a fork seed, re-pinning the model/route).

## Context

- `internal/agent/delegate.go` (01) — `ChildRecord.Descriptor ChildDescriptor` (empty field today), `SetDescriptorFactory(func(ChildRecord) ChildDescriptor)`, `SetPersist(func(ChildRecord) error)`, `Lookup`.
- `internal/agent/delegate_fork.go` (02) — `completedTurnPrefix`; the fork seed is captured once and must ride the descriptor (B2).
- `internal/session/` — the daemon-owned store child records may persist into (Open Question 2: prefer the durable storage record-change events / session store seam branch 03 supplies — or the session store's existing table).
- `internal/agent/delegate_descriptor.go` (NEW, this leaf).
- dsh reference: `/tmp/deepseek-harness/packages/subagent/subagent/descriptor.ts` (descriptor shape + version) and `manager.ts:1349-1404` (coldResume).

## Verified Anchors (grep-confirmed at authoring time)

- internal/agent/delegate.go (01) — `ChildRecord.Descriptor`, `SetDescriptorFactory`, `SetPersist`, `Lookup`, `Spawn`
- internal/agent/delegate_fork.go (02) — `completedTurnPrefix` (seed rides the descriptor)
- internal/session/store.go:93 — `Store` interface (GetMessages etc.); persistence target for child records
- internal/agent/registry.go:940-947 — `RunAgent` (resume re-runs the child through this)
- dsh descriptor.ts:48-86,230-269 — descriptor fields + version

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/agent/delegate_descriptor.go  (NEW)
type ChildDescriptor struct {
    Version          int       `json:"version"`             // 1
    Mode             string    `json:"mode"`
    Provider         string    `json:"provider,omitempty"`
    Label            string    `json:"label,omitempty"`
    AgentProvider    string    `json:"agent_provider,omitempty"` // "local"|"acp"
    AgentModel       string    `json:"agent_model,omitempty"`
    Reasoning        string    `json:"reasoning,omitempty"`
    ParentSessionID  string    `json:"parent_session_id"`
    SeedPayload      string    `json:"seed_payload,omitempty"`   // fork seed (B2), captured once
    CreatedAt        time.Time `json:"created_at"`
}
func (d ChildDescriptor) Validate() error          // version==1 required; mode/provider sanity
func MarshalChildDescriptor(d ChildDescriptor) ([]byte, error)
func UnmarshalChildDescriptor(b []byte) (ChildDescriptor, error)  // unknown/newer version -> error
// DelegateManager.Resume(ctx, childID) (DelegateResult, error):
//   re-spawns an interrupted/idle child from its descriptor (re-runs the
//   seed / fresh spawn, re-pins model/route), updates the record to running,
//   and republishes the settlement on completion.
```

### What This Leaf Consumes

- B1 (01) — `ChildRecord.Descriptor`, `SetDescriptorFactory`, `SetPersist`, `Lookup`
- B2 (02) — the fork seed snapshot (`SeedPayload` on the descriptor)
- B4 (04) — `PreflightRoute`/route resolution re-applied on resume
- daemon-owned store — the persistence target for child records

## Tasks

### T1: Versioned descriptor type + JSON round-trip

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T1 | Define `ChildDescriptor` (versioned, JSON-durable) with Validate/Marshal/Unmarshal; reject a wrong/newer version | Create: internal/agent/delegate_descriptor.go; Test: internal/agent/delegate_descriptor_test.go (gate) | `go test -p 2 ./internal/agent/ -run TestChildDescriptor -count=1` | type/Marshal undefined | a descriptor JSON round-trips with Version=1; an unknown/newer version errors | Given a ChildDescriptor{V1}; When Marshaled then Unmarshaled; Then fields are identical and Validate passes; a version 2 payload errors |

T1 (expand):
1. Confirm the gate `TestChildDescriptor` REDs.
2. Define `ChildDescriptor` exactly per B6. `MarshalChildDescriptor`/`UnmarshalChildDescriptor` are strict (unknown/newer version => error; do not silently downgrade). `Validate()` enforces `Version==1` and non-empty `Mode`/`AgentProvider`.
3. Verify `go test -p 2 ./internal/agent/ -run TestChildDescriptor -count=1`.

### T2: Stamp + persist the descriptor on every spawn

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T2 | Every `Spawn` stamps a `ChildDescriptor` onto the record and persists it through the daemon store | Modify: internal/agent/delegate.go (SetDescriptorFactory + SetPersist wiring), internal/agent/delegate_descriptor.go; Test: e2e delegate-resume | `make e2e-fast-area AREA=delegate-seam` + `go test -p 2 ./internal/agent/ -run TestChildDescriptor -count=1` | descriptor not populated / not persisted | a spawned child's record carries a persisted Version-1 descriptor with mode/model/provider/seed | Given a `Spawn` that creates a child; When it settles; Then the record's Descriptor is populated and persisted (survives a re-read from the store) |

T2 (expand):
1. Wire `SetDescriptorFactory` in 01's manager construction: build a
   `ChildDescriptor` from the child's mode/model/provider/reasoning/seed
   (the fork seed via `SeedPayload` — 02 guarantees a snapshot copy).
2. Wire `SetPersist` to insert/upsert the child record into the daemon-owned
   store. Reuse the session store's durable table (or the storage
   record-change events seam branch 03 supplies — Open Question 2). Do NOT
   build a parallel persistence mechanism; if branch 03's SQL is not yet
   available, persist through the session store's existing table and note the
   coupling for integration review.
3. Make `persist` best-effort on the settle path (like the teardown ledger):
   a failed persist must NOT fail the completed child turn (root AGENTS.md
   invariants) — log + continue.
4. Verify via the resume e2e and the unit round-trip.

### T3: Cold resume — DelegateManager.Resume

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T3 | `Resume(childID)` re-spawns an interrupted/idle child from its descriptor (seed/model/route) and republishes settlement | Create: internal/agent/delegate_descriptor.go (Resume impl); Modify: internal/agent/delegate.go (add Resume method); Test: e2e delegate-seam (`TestDelegate_ColdResume`) | `make e2e-fast-area AREA=delegate-seam` | resume path undefined / doesn't re-run | an interrupted child `Resume(childID)` re-runs from its descriptor and settles completed; the fork seed replays (not the parent's newer history) | Given an interrupted child with a persisted descriptor (seed + model); When Resume(childID) runs; Then the child re-spawns with that seed/model, runs, and its settlement is published as completed |

T3 (expand): implement `DelegateManager.Resume(ctx, childID)`:
- `Lookup(childID)`; State must be Interrupted/Idle (resuming a Running child is a guard error).
- Build the spawn brief from the descriptor: fork-mode children re-apply
  their `SeedPayload` (02 snapshot) — a fork resume replays the ORIGINAL seed,
  NOT a re-fork of the parent's newer history (manager.coldResume semantics,
  Subagent 1b). Re-pin `AgentModel`/`Provider` (re-run 04 `PreflightRoute`).
- Re-`Spawn` under the descriptor's mode (local re-run via RunAgent, or acp
  re-`GetOrCreatePersona`), update State back to running, and on settle
  publish the settlement + update State completed.
- Auth: only the parent (record.ParentSessionID == caller) may resume.
- Verify via the `TestDelegate_ColdResume` e2e scenario.

### T4: Surface + doc

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|----------|-------|----------------|------------|------------|
| T4 | User-facing resume surface (CLI/TUI/HTTP) + docs/workflows/delegation.md (resume section) | Modify: cmd/meept/ or internal/tui/ or internal/comm/http/; docs/workflows/delegation.md | `make graphs-check` + `make docs-generate` (if gendoc drifted) | resume has no user path | `meept delegate resume <child_id>` (or equivalent) re-runs a persisted child; doc explains descriptor + cold resume | Given an interrupted child persisted by a prior daemon run; When the user resumes it; Then it re-spawns from its descriptor and completes |

Root AGENTS.md wiring requirement: ship a `meept delegate resume <child_id>`
(CLI) or TUI/HTTP equivalent calling `DelegateManager.Resume`, and document
the descriptor + cold resume in `docs/workflows/delegation.md`. Defer a rich
GUI panel with an explicit note if time-bounded; the CLI + doc must land.

## Self-Verification Checklist

- [ ] ChildDescriptor versioned + strict round-trip (unknown/newer version errors); gate T1 GREEN
- [ ] Every Spawn stamps + persists a descriptor; persist is best-effort, never fails the child turn
- [ ] Resume re-runs from the descriptor (fork seed snapshot, model/route re-pinned); gate T3 e2e GREEN
- [ ] Auth: only parent resumes a child
- [ ] No scope creep; no control-tool/workflow logic here (leaves 03/05)
- [ ] `go build ./...` clean; no import cycles (descriptor pkg has no cycle with delegate.go)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** every created/modified path.
2. **Verify command + output:** `go test -p 2 ./internal/agent/ -run TestChildDescriptor -count=1`; `go build ./...`; `make e2e-fast-area AREA=delegate-seam`; last ~20 lines.
3. **Deviations:** none / list (esp. the persistence target if branch 03's SQL was unavailable and the exact store table/coupling).

## Review Checklist (For Review Agent)

- [ ] Descriptor type matches B6 exactly; version enforced strictly (no silent downgrade)
- [ ] Descriptor stamped + persisted on every spawn; persist best-effort (never fails a completed turn)
- [ ] Resume re-runs a child from its descriptor (fork seed snapshot, not a re-fork; model/route re-pinned; auth = parent only)
- [ ] No parallel persistence mechanism; the store reuse is clean
- [ ] Conventions; no debug artifacts / line-number corruption

Output: APPROVED or specific gaps (file + line).

## Notes

- `Resume` must replay a FROZEN fork seed, not re-fork the parent's newer
  history — that is the whole point of the durable descriptor (dsh
  fork-contract, subagent 1b). Keep `SeedPayload` authoritative on resume.
- Persistence reuse decision (Orchestrator Open Question 2): prefer branch
  03's durable storage record-change events / session-store table. If the
  exact SQL is not yet available at integration, persist through the session
  store's existing table now (additive, forward-compatible) and reconcile at
  master Phase 3. Either way, best-effort on the settle path.
- The supervisor (delegation parent) owns the decision to resume; the child
  record + descriptor is what survives — the resumer (user/model) must still
  exist. If the parent session is gone, `Resume` errors with a clear message.
