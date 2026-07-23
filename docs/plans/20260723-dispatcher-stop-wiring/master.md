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
