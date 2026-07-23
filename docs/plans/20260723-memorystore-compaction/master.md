---
name: master.md
description: Root orchestrator for MemoryStore compaction implementation
version: 1.0.0
author: Hermes Agent
license: MIT
metadata:
  hermes:
    tags: [memory, compaction, session-management]
---

# MemoryStore Compaction Implementation Plan — Root Orchestrator

## Goal

Implement three missing MemoryStore methods to achieve feature parity with SQLiteStore:
- `NavigateToBranch()` — tree-based message navigation
- `InsertCompaction()` — insert compacted message nodes
- `ReparentAfterCompaction()` — restructure tree after compaction

**Target**: `internal/session/session.go` lines 787, 908, 913  
**Estimated effort**: 6-12 hours total (3 leaves, 2-4 hours each)

## Architecture Overview

MemoryStore currently returns "not implemented" errors for compaction-related operations. These are core to branch navigation and memory optimization. Implementation requires understanding the session tree structure and how compaction transforms it.

## Interface Contracts

### Exposed Methods

This plan exposes three new MemoryStore methods matching the Store interface:

**NavigateToBranch(sessionID, targetID string) (oldLeaf *MessageLeaf, error)**
- Navigates session's current leaf to target message
- Returns previous leaf for potential rollback
- Error if session or target not found

**InsertCompaction(sessionID string, parentID int64, compressedIDs []int64, metadata map[string]any) (int64, error)**
- Inserts compaction entry node into session tree
- Returns new node ID
- Stores compressed message IDs and metadata as JSON

**ReparentAfterCompaction(sessionID string, compactionID int64, childrenToReparent []int64) error**
- Reparents specified child messages to follow compaction entry
- Maintains tree structure after compression
- Error if any IDs invalid

### Tree Structure Contract

All three methods operate on the in-memory session tree with these invariants:
- **Parent-child relationships**: Maintained via `ParentID` field on messages
- **Leaf tracking**: Session tracks current leaf position for navigation
- **Thread safety**: All methods acquire session mutex before modification
- **Error handling**: Return structured errors, never panic

### Parity with SQLiteStore

Implementation must match SQLiteStore behavior exactly:
- Same error types for equivalent failures
- Same tree transformation logic
- Same edge case handling (empty sessions, missing targets, concurrent access)

## Child Index

| ID | Document | Type | Est. Context | Dependencies | Status |
|----|----------|------|--------------|--------------|--------|
| 01 | 01-navigate-to-branch.md | Leaf | ~50K | None | PENDING |
| 02 | 02-insert-compaction.md | Leaf | ~55K | 01 | PENDING |
| 03 | 03-reparent-after-compaction.md | Leaf | ~50K | 01, 02 | PENDING |

**Dependencies**: Sequential (01 → 02 → 03) due to shared tree manipulation logic

## Dispatch Protocol

1. **Dispatch sequentially** (dependencies prevent parallel execution):
   - Start with leaf 01
   - After review passes, dispatch leaf 02
   - After review passes, dispatch leaf 03
   
2. **Review** (main model, in-session):
   - Verify tree manipulation logic is correct
   - Check for edge cases (empty trees, single-node trees, deep nesting)
   - Run `go test ./internal/session/...`

3. **Commit** (after all leaves complete):
   - Stage changed files
   - `git commit -m "feat(session): implement MemoryStore compaction methods"`

## Completion Tracking Table

| Leaf | Status | Iterations | Timestamp | Complete | Notes |
|------|--------|------------|-----------|----------|-------|
| 01-navigate-to-branch | COMPLETE | 1 | 2026-07-23T13:40 | 100% | Orchestrator Wave 0; 6 unit tests pass |
| 02-insert-compaction | COMPLETE | 1 | 2026-07-23T13:40 | 100% | Orchestrator Wave 0; 5 unit tests pass |
| 03-reparent-after-compaction | COMPLETE | 1 | 2026-07-23T13:40 | 100% | Orchestrator Wave 0; 5 unit tests pass |
| Integration tests | COMPLETE | 1 | 2026-07-23T13:40 | 100% | 3 end-to-end workflow tests pass |
| Code review | COMPLETE | 1 | 2026-07-23T13:40 | 100% | Parity verified vs SQLiteStore |

**Execution method**: Single-leaf decomposition (Step 2.5). All 3 methods
co-located in session.go, so orchestrator implemented them in Wave 0 (~90
lines). 5 parallel subagents dispatched for tests (4 files) + code review.
19/19 tests pass with -race. Commit edacab6a.

## Integration Test Plan

After completion:
- [ ] `go build ./internal/session/...` succeeds
- [ ] `go test ./internal/session/...` passes
- [ ] Test compaction workflow end-to-end
- [ ] Verify no regressions in existing session operations

## Open Questions

None — implementation follows SQLiteStore pattern exactly:
- Method signatures match Store interface definition
- Tree manipulation logic copied from proven SQLiteStore implementation
- Test patterns established by existing MemoryStore tests
- No design ambiguities: parity with SQLiteStore is the clear goal

All decisions resolved by referencing existing implementation.

## Coding Conventions

- Follow existing tree manipulation patterns in session.go
- Add comprehensive comments explaining tree transformations
- Handle edge cases explicitly (nil nodes, empty branches, cycles)
- Use existing error types where possible

## Review Checklist

For each leaf:
- [ ] Method signature matches interface
- [ ] Tree manipulation logic is correct
- [ ] Edge cases handled
- [ ] Tests added for new functionality
- [ ] No debug artifacts
- [ ] `go test ./internal/session/...` passes
