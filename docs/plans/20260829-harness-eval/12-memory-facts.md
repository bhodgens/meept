# Memory Facts - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> Do NOT use read_file on existing source. After writing, do not read back.

## Meta

- **Parent:** ../master.md
- **Scope:** Typed MemoryFact store with conflict+time. Retrieve-as-tool (agentic RAG lite). Not a new vector DB.
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A

## Goal

User memory is a model of the user, not a transcript dump. Preference/restriction/account/temporal facts persist across sessions.

## Context

personality.md + episodic FTS5 exist. Do not replace them. FactStore is beside them on Manager.

Key files: `internal/memory/manager.go`, `personality.go`, `internal/memory/staleness.go` (reuse freshness text if injecting).

IDs: pkg/id.Generate. Multiuser off: OwnerID empty means daemon owner. Multiuser on: set owner_id from identity; do not change the disabled-path.

## Interface Contracts (From Parent)

C6 MemoryFact + Upsert/GetActive. Conflict last-write-wins with ValidUntil on the old row.

Tool `memory_fact_search{query, kind?}` returns matching active facts. Do not always-inject the full transcript. Optional cap of 20 active facts in the unstable prompt section if already in the injection path — prefer the tool.

### What This Leaf Exposes

`internal/memory/fact.go`, SQLite table `memory_facts`, builtin tool, Manager methods.

### What This Leaf Consumes

none.

## Tasks

### Task 1: Store + conflict + temporal

**Files:** Create `internal/memory/fact.go`, `fact_store.go`, `fact_store_test.go`

GetActive at time T excludes ValidUntil<=T. Upsert same key closes previous.

### Task 2: Wire Manager

**Files:** Modify `internal/memory/manager.go` with nil-safe FactStore.

### Task 3: Tool

**Files:** Create `internal/tools/builtin/memory_fact_search.go` + register. Test empty store returns empty list not error.

### Task 4: Extract hook (minimal)

**Files:** After session close, enqueue extract is enough: a function `ExtractFactsFromMessages([]Message) []MemoryFact` with a test on a canned dialogue (window seat + vegetarian). Real LLM call may be stubbed behind an interface; default stub is regex/heuristic only if you cannot inject an LLM in unit tests. Document the interface for later.

## Self-Verification Checklist

- [ ] -race internal/memory
- [ ] No new vector engine
- [ ] Multiuser-off OwnerID empty still works
- [ ] Do NOT commit

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

**Deviations from spec:** none

## Review Checklist (For Review Agent)

- [ ] SQL parameterized
- [ ] Tool registered in production registry
- [ ] personality.md still loads
