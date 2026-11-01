# Spill Sweep (TTL + Size Cap) - Implementation Leaf

> **For the implementing agent:** You are the implementer. TDD; gate tests
> exist. Do NOT commit. Do NOT read back files after writing. Do NOT use
> read_file on source - explore with search_files/terminal.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** TTL/size sweep for $MEEPT_HOME/spill/tools per contract C2
- **Dependencies:** none (parallel with leaf 01; disjoint files)
- **Estimated Context:** 70K
- **Concurrency Group:** parallel with 01-shell-tail-spill

## Goal

Tool-layer spills accumulate. Add pure sweep functions to internal/llm/spill.go
and wire them from the daemon: run at startup and every 6h; remove files older
than 24h; if the tools spill tree still exceeds 512MB, evict oldest-first
until under the cap. Never delete files newer than 1h (in-flight protection).

## Context

- internal/llm/spill.go:46 - SpillService (root resolution MEEPT_HOME-aware)
- internal/daemon - lifecycle wiring (see internal/daemon/AGENTS.md; follow
  existing ticker patterns, e.g. Components.startGitHubReArmTicker 30s)
- Pinned signature (gate T4):
  `SweepToolSpills(root string, maxAge time.Duration, maxBytes int64, now time.Time) (removed int, err error)`
  Behavior: walk root recursively; skip files with mtime within 1h of now;
  delete mtime < now-maxAge; then if sum(size) > maxBytes, delete oldest
  mtimes first until <= maxBytes. Deterministic order: sort by (mtime, path).

## Verified Anchors

- internal/llm/spill.go:46-61 - SpillService struct + constructor
- internal/daemon/components.go - ticker wiring precedent (startGitHubReArmTicker)

## Interface Contracts (From Parent)

### What This Leaf Exposes

```go
// internal/llm/spill.go
func SweepToolSpills(root string, maxAge time.Duration, maxBytes int64, now time.Time) (removed int, err error)
func DefaultToolSpillRoot() string // $MEEPT_HOME/spill/tools (MEEPT_HOME-aware, mirrors existing resolution)
// internal/daemon wiring: startup call + 6h ticker; logs removed count at info
```

### What This Leaf Consumes

- C2 path convention: `$MEEPT_HOME/spill/tools/<session_id>/<ulid>.txt`

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | SweepToolSpills pure fn | Modify: internal/llm/spill.go; Test: internal/llm/spill_test.go | `go test -p 2 -run TestSpillSweep ./internal/llm/` | gate RED (or fn absent) | age + size bounds honored, 1h in-flight guard, deterministic order | Given a fixture tree with mixed mtimes/sizes; When swept; Then removed set matches expectations exactly |
| T2 | Daemon wiring | Modify: internal/daemon/components.go (or the lifecycle file the AGENTS.md invariants point to); Test: e2e or existing daemon test if bug-fix-adjacent | `go build ./... && go test -p 2 ./internal/daemon/ -run TestSpill` | no sweep today | startup sweep + 6h ticker registered; graceful stop | Given a daemon start with stale spill files; When startup completes; Then files >24h are gone and a log line reports the count |
| T3 | Size cap eviction | Test: spill_test.go | `go test -p 2 -run TestSpillSweepEvicts ./internal/llm/` | gate T4 covers | oldest-first eviction to <=512MB | Given 600MB of fresh-but-stale files; When swept; Then total <=512MB and the newest survivors are the most recent |

## Self-Verification Checklist

- [ ] Pure function has zero daemon imports (testable in isolation)
- [ ] Ticker stops on daemon shutdown (no goroutine leak; context-cancelled)
- [ ] No deletion of files <1h old under any branch
- [ ] `go test -p 2` clean for both packages

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. verify output tails; 3. deviations.

## Review Checklist (For Review Agent)

- [ ] Signature matches pinned contract exactly
- [ ] Wiring follows internal/daemon/AGENTS.md lifecycle rules
- [ ] Sweep errors are logged, never fatal to startup
