# Status Bar - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Deterministic agent status block. Indexed tool schemas stay in the stable prompt prefix.
- **Dependencies:** 10-isolation.md, 11-speak-router.md
- **Estimated Context:** 50K
- **Concurrency Group:** E

## Goal

The model sees machine-maintained turn state, not a story. Cache-stable tool list when schema_mode=indexed.

## Context

`internal/agent/prompt.go` AssembleOrdered + PromptSection Stable flag already exist. Add an unstable `status` section. Indexed schemas: tool definition bytes that do not change mid-session belong in a Stable section.

Key files: `internal/agent/prompt.go`, loop that calls AssembleOrdered / AddSection.

## Interface Contracts (From Parent)

C8 StatusBar. Fail closed to `unknown` on missing fields. Never copy model prose into the bar.

When schema_mode=indexed, the compact tool list is Stable=true. Full schemas from tool_view are not in the prefix.

### What This Leaf Exposes

`StatusBar(TurnStatus) string` and a PromptSection from the loop.

### What This Leaf Consumes

C3 SpeakKind, C4 isolation mode, gate result from leaf 04 if present (n/a otherwise).

## Tasks

### Task 1: StatusBar golden tests

**Files:** Create `internal/agent/status_bar.go`, `status_bar_test.go`

Fixed fields, lowercase labels, no timestamps that bust cache... wait: status is Unstable so timestamps are ok but skip clock; use turn index only.

### Task 2: Inject unstable section

**Files:** Modify prompt assembly call site (prompt.go and/or loop). Keep identity/rules Stable.

### Task 3: Indexed tool list Stable

**Files:** Where tool defs are added to the prompt, mark Stable=true if schema_mode=indexed. Test: two turns, same hash of stable prefix if only status changed.

## Self-Verification Checklist

- [ ] Status errors fail closed
- [ ] Stable prefix hash ignores status body
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] No model text in the bar
- [ ] AssembleOrdered still stable-first
- [ ] schema_mode=full does not claim tool list is stable
