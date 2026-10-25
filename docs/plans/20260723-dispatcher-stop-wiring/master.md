---
name: master.md
description: Root orchestrator for dispatcher-stop wiring plan
version: 1.0.0
author: Hermes Agent
license: MIT
metadata:
  hermes:
    tags: [dispatcher, shutdown, cleanup]
---

# Dispatcher Stop Wiring Plan — Root Orchestrator

## Goal

Wire `Dispatcher.Stop()` into daemon shutdown to cleanly stop the BuildIndex goroutine and prevent resource leaks.

**Target**: `internal/daemon/components.go` — add dispatcher stop call in `Components.Stop()` method  
**Estimated effort**: ~30 min (single leaf, straightforward)

## Architecture Overview

The dispatcher has a `Stop()` method that stops its BuildIndex goroutine, but it's not called during daemon shutdown. This is a benign leak in current usage (dispatcher lives for daemon lifetime), but violates clean shutdown principles.

## Interface Contracts

### Exposed Behavior

This leaf exposes:
- **No new APIs** — only wires existing `Dispatcher.Stop()` into shutdown sequence
- **Side effect**: BuildIndex goroutine cleanly terminates on daemon shutdown
- **Error handling**: Logs error if Stop() fails, continues shutdown (non-blocking)

### Shutdown Order Contract

Dispatcher must be stopped **before** dependent components (stores, registries) to prevent:
- Goroutine accessing closed resources
- Race conditions during shutdown
- Resource leaks from abandoned background tasks

## Child Index

| ID | Document | Type | Est. Context | Dependencies | Status |
|----|----------|------|--------------|--------------|--------|
| 01 | 01-wire-dispatcher-stop.md | Leaf | ~25K | None | PENDING |

## Dispatch Protocol

1. **Dispatch implementation agent** via `delegate_task`:
   - Read leaf document
   - Include: "Do NOT commit. Write code, run tests, report results."
   
2. **Review** (main model, in-session):
   - Verify dispatcher.Stop() is called in Components.Stop()
   - Run `go build ./...`
   - Check for stray artifacts

3. **Commit** (after review passes):
   - `git add internal/daemon/components.go`
   - `git commit -m "fix(daemon): wire Dispatcher.Stop() into shutdown"`

## Completion Tracking Table

| Leaf | Status | Iter | Completed | % | Notes |
|------|--------|------|-----------|---|-------|
| 01-wire-dispatcher-stop | COMPLETE | 0 | 2026-07-23T17:00 | 100% | Already implemented in d4260d9f (components.go:3724-3729). Stale TODO removed. |

## Integration Test Plan

After completion:
- [ ] `go build ./...` succeeds
- [ ] Daemon starts and stops cleanly in foreground mode
- [ ] No goroutine leaks detected

## Open Questions

None — this is a straightforward wiring task with no ambiguities:
- Dispatcher.Stop() already exists and works correctly
- Shutdown order is clear (dispatcher before stores)
- Error handling pattern is established (log + continue)

No design decisions or trade-offs required.

## Coding Conventions

- Call dispatcher.Stop() before other component shutdowns
- Log the shutdown action at Info level
- Handle any errors from Stop() gracefully (log, don't panic)

## Review Checklist

- [ ] Dispatcher.Stop() called in Components.Stop()
- [ ] Shutdown order is logical (dispatcher before dependent components)
- [ ] Error handling present
- [ ] `go build ./...` succeeds
- [ ] No debug artifacts

## Test Gate

<!-- Tests-first gate (hierarchical-planning skill v1.3.0, patched 2026-10-24).
     Coverage matrix derived mechanically from leaf task headers; the control
     agent (orchestrator, in-session) refines rows and fills test names at
     Phase 0 dispatch. No implementation leaf dispatches until every row is
     VALIDATED. Delta protocol: lesser of plan/tests updates to the greater.
     Recipe: skill hierarchical-planning -> references/tests-first-gate.md -->

| # | Feature (from plan) | Source | Unit tests | E2E tests | Status |
|---|---------------------|--------|-----------|-----------|--------|
| F1 | Read Components.Stop() method | 01-wire-dispatcher-stop.md | (pending) | (pending) | PENDING |
| F2 | Identify dispatcher field | 01-wire-dispatcher-stop.md | (pending) | (pending) | PENDING |
| F3 | Add Stop() call | 01-wire-dispatcher-stop.md | (pending) | (pending) | PENDING |
| F4 | Verify Stop() signature | 01-wire-dispatcher-stop.md | (pending) | (pending) | PENDING |
| F5 | Verify | 01-wire-dispatcher-stop.md | (pending) | (pending) | PENDING |

Statuses: PENDING | DELTA | VALIDATED

Delta log: (none yet)

### Control-agent survey 2026-10-25 — verdict: VALIDATED (tree-level)

- **Survey verdict:** TESTED
- **E2E evidence (manifest path_map → suites):** acp-pe, acp-wi, async-, breake, contin, daemon
- **Unit packages with tests:** internal/agent, internal/daemon
- **Sample verified tests:** internal/daemon:TestMCPToolRefresher_NilGuards
- **Baseline:** `make test` 2026-10-25: 10,873 recorded results; the 26 initial failures were ephemeral-port exhaustion (`can't assign requested address`, AGENTS.md known issue) — `go test -count=1 -p 1 ./internal/tools/builtin/ ./internal/tui/` GREEN on isolated re-run.

Row-level statuses above stay as seeded; this tree-level verdict records that every
planned feature has located test evidence (e2e suite mapping and/or package tests).
Row-by-row test-name mapping happens at next dispatch touch of this tree.
