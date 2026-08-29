# Shadow Capture - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Specialist loops get shadowMgr. Tool turns persist. Do not add a second capture API.
- **Dependencies:** 05-verify-breaker.md (loop.go serial)
- **Estimated Context:** 55K
- **Concurrency Group:** C

## Goal

Learning data includes tool-use trajectories and specialist work. docs/implementation-gaps.md items 1–2.

## Context

`CaptureToolInteraction` already exists and is called from `internal/agent/loop.go` ~3199. `CaptureInteraction` fires on final text ~3589. Survey with cat/grep first. Remaining hole: AgentRegistry-created loops constructed without shadowMgr.

Key files: `internal/agent/loop.go`, `internal/agent/registry.go`, `internal/daemon/components.go` (where NewAgentLoop vs registry loops are built), `internal/shadow/manager.go`.

## Interface Contracts (From Parent)

C7. Same CaptureToolInteraction signature. Registry loops: pass shadowMgr in RegistryConfig / definitionToSpec / NewAgentLoop options — match however the primary loop receives it.

### What This Leaf Exposes

Specialist loops capture tool turns. Test that constructing a registry loop with a fake shadow sink records a tool call.

### What This Leaf Consumes

Existing CaptureToolInteraction.

## Tasks

### Task 1: Prove current specialist gap with a failing test

**Files:** Create `internal/agent/shadow_specialist_test.go`

Registry-built loop, tool call, assert sink.Count==0 today then 1 after fix.

### Task 2: Thread shadowMgr into registry loops

**Files:** Modify `internal/agent/registry.go` and the daemon constructor site that builds the registry. Minimal field plumb.

### Task 3: Confirm tool-turn persist

**Files:** Test in `internal/shadow/` or agent: CaptureToolInteraction with a tool-call response is stored (not dropped). If already true, assert it and stop. If store ignores tool rows, fix the store.

## Self-Verification Checklist

- [ ] No second capture API
- [ ] Primary loop still captures
- [ ] components.go actually assigns the field (not a local var)
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] grep shadowMgr on registry vs primary loop
- [ ] No dead SetShadow that is never called
- [ ] mutexio: no I/O under lock in new code
