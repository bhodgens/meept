# Context Isolation - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** ArtifactOnly default for handoff, subagent, and pair. SharedTranscript is opt-in.
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A

## Goal

Fix the design oversight: specialists must not inherit parent tool dumps or chain-of-thought.

## Context

Dispatcher handoff copies AccumulatedContext today (features.md). Pair sessions share transcript. Subagent spawn likely copies messages. Replace with BuildSpawnContext.

Do not touch loop.go in this leaf if possible. Isolation struct + call sites in dispatcher/pair/registry. loop.go speak comes in leaf 11.

Key files: `internal/agent/dispatcher.go`, `internal/agent/orchestrator.go` handoff, `internal/agent/collaboration_pair_driver.go`, `internal/agent/registry.go`.

## Interface Contracts (From Parent)

C4 ContextIsolation + SpawnContext.

Defaults: all three spawn kinds ArtifactOnly. Pair create API/flag `shared_transcript=true` is the only opt-in. Unknown isolation value fail-closed to ArtifactOnly + warn.

### What This Leaf Exposes

`internal/agent/isolation.go` BuildSpawnContext. Call sites set Isolation on spawn.

### What This Leaf Consumes

none.

## Tasks

### Task 1: BuildSpawnContext tests

**Files:** Create `internal/agent/isolation.go`, `isolation_test.go`

ArtifactOnly: Transcript empty, Brief and Artifacts kept. SharedTranscript: parent messages copied. BusMessage: Transcript empty (parent will send on bus — no copy).

### Task 2: Pair default

**Files:** Modify pair driver/create. Test: default spawn has empty Transcript. Flag sets SharedTranscript.

### Task 3: Handoff + subagent

**Files:** Modify handoff path and delegate/subagent spawn. Test: child messages do not contain a parent tool-result dump string used as a canary.

## Self-Verification Checklist

- [ ] Default is ArtifactOnly including pairs
- [ ] Fail-closed unknown enum
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] AccumulatedContext is artifacts/brief, not raw parent messages
- [ ] No silent fallback to full transcript
- [ ] session vs conversation ids unchanged
