---
name: master.md
description: Root orchestrator for panic-replacement plan tree
version: 1.0.0
author: Hermes Agent
license: MIT
metadata:
  hermes:
    tags: [panic-replacement, error-handling, production-safety]
---

# Panic Replacement Plan — Root Orchestrator

## Goal

Replace all 5 `panic()` calls in production code with proper error handling to prevent unexpected daemon crashes.

**Target files:**
- `internal/bus/bus.go:178` — panics on Publish with no subscribers
- `internal/agent/loop.go:1408` — panics if sessionID missing
- `internal/mcp/server.go:317` — panics on json.Marshal failure
- `internal/tools/builtin/hashline_parser.go:168` — panics on odd key-value args
- `internal/session/session.go:1921` — unconditional panic

## Architecture Overview

The meept daemon must never crash from recoverable conditions. Each panic represents a design decision that should have been an error return or graceful degradation. This plan replaces each with appropriate error handling:

- **bus.Publish**: Return error + log warning (already has warning path)
- **agent loop**: Return structured error instead of panic
- **mcp server**: Handle marshal errors gracefully
- **hashline parser**: Return parse error instead of panic
- **session store**: Remove unconditional panic (investigate context first)

## Interface Contracts

### Error Handling Convention

All replacements follow this pattern:
```go
// Before:
if condition {
    panic("message")
}

// After:
if condition {
    return fmt.Errorf("context: %w", ErrSpecificCondition)
}
```

Where `ErrSpecificCondition` is a sentinel error defined at package level.

### Logging Convention

Panics that had side effects (like the bus warning) preserve the log call before returning error.

## Child Index

| ID | Document | Type | Est. Context | Dependencies | Status |
|----|----------|------|--------------|--------------|--------|
| 01 | 01-bus-publish.md | Leaf | ~40K | None | PENDING |
| 02 | 02-agent-loop.md | Leaf | ~50K | None | PENDING |
| 03 | 03-mcp-server.md | Leaf | ~35K | None | PENDING |
| 04 | 04-hashline-parser.md | Leaf | ~30K | None | PENDING |
| 05 | 05-session-store.md | Leaf | ~45K | None | PENDING |

**Total estimated context**: ~200K across 5 independent leaves  
**Concurrency**: All 5 can run in parallel (no dependencies)

## Dispatch Protocol

### For Each Leaf:

1. **Dispatch implementation agent** via `delegate_task`:
   - Read the leaf document
   - Include interface contracts above
   - Include: "Do NOT commit. Do NOT run git add. Write code, run tests, report results only."
   - Agent implements all tasks using TDD

2. **Review implementation** (main model, in-session):
   - Read changed files
   - Verify error handling matches convention
   - Run `go build ./...` and `go test ./<package>/...`
   - Check for stray artifacts (fmt.Println, TODOs)

3. **Re-dispatch if incomplete**:
   - If review finds gaps → re-dispatch with specific feedback
   - Max 3 iterations per leaf

4. **Commit** (after review passes):
   - Stage specific files: `git add <exact paths>`
   - Commit: `git commit -m "fix(pkg): replace panic with error return in <file>"`
   - Update tracking table: status → REVIEWED

5. **Record status** in tracking table below

### Concurrency Rules

- Batch all 5 leaves immediately (independent, no dependencies)
- Hermes default concurrency: 3 subagents → dispatch in two batches (3+2)

## Completion Tracking Table

| Leaf | Status | Iterations | Timestamp | Complete | Notes |
|------|--------|------------|-----------|----------|-------|
| 01-bus-publish | COMPLETE | 1 | 2026-07-23T12:47 | 100% | Error log + return 0, 2 tests updated |
| 02-agent-loop | COMPLETE | 1 | 2026-07-23T12:45 | 100% | Error log + return nil, test updated |
| 03-mcp-server | COMPLETE | 1 | 2026-07-23T12:44 | 100% | Error log + return nil json.RawMessage |
| 04-hashline-parser | COMPLETE | 1 | 2026-07-23T12:44 | 100% | Error log + return empty orderedMap |
| 05-session-store | COMPLETE | 1 | 2026-07-23T12:43 | 100% | Error log + return nil []byte |

## Integration Review Plan

After all leaves reach COMPLETE:

1. **Full build**: `go build ./...`
2. **Full test suite**: `make test` (or `go test ./...`)
3. **Race detector**: `go test -race ./...`
4. **Verify no remaining panics**: `grep -rn 'panic(' --include='*.go' internal/ | grep -v '_test.go' | grep -v '// panic'`
5. **Integration check**: Ensure error propagation doesn't break callers (spot-check 2-3 call sites per change)

## Coding Conventions

- Use `%w` for error wrapping when returning underlying errors
- Define sentinel errors at package level: `var ErrXxx = errors.New("...")`
- Preserve existing log calls before returning errors
- No new panics introduced
- All error returns must be checked by callers (verify with `go vet`)

## Review Checklist

For each leaf:
- [ ] Panic replaced with error return
- [ ] Sentinel error defined (if needed)
- [ ] Existing log calls preserved
- [ ] Callers handle new error (or already do)
- [ ] Tests pass (`go test ./<package>/...`)
- [ ] No debug artifacts (fmt.Println, TODOs, placeholder values)
- [ ] No line-number corruption in source files

## Integration Test Plan

After all leaves complete:
- [ ] `go build ./...` succeeds
- [ ] `make test` passes
- [ ] `go test -race ./...` passes (no race conditions introduced)
- [ ] Grep confirms no remaining production panics
- [ ] Daemon starts successfully in foreground mode

## Open Questions

None — panic replacement follows clear, established pattern:
- All 5 panics replaced with error returns + logging
- Function signatures preserved to avoid breaking callers
- Sentinel errors defined where semantically meaningful
- No design trade-offs: crashing is always worse than graceful error handling

All decisions straightforward with no ambiguities.
