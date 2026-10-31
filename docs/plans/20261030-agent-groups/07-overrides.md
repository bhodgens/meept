# 07: Time-Boxed Overrides - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks via TDD. Do NOT commit.
> No read_file on source; explore with search_files/cat. Write once, stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Operator-managed time-boxed grants (C8): the overrides FILE,
  reader with expiry, and CLI add/list/prune. Re-invention of the dead
  engine's overrides table as a reviewable config artifact.
- **Dependencies:** 02-groups-model (REVIEWED); consumes 04's override seam
  (T5) if REVIEWED - if not yet merged, the reader still lands and wiring
  waits on 04 (note in report).
- **Estimated Context:** 45K
- **Concurrency Group:** D

## Goal

An operator can grant `git_push` to a group for a deployment window with one
reviewable file entry, and expired entries never apply.

## Interface Contracts (From Parent)

C8 (verbatim): $MEEPT_HOME/group_overrides.json5; entries
{subject, allow[], reason, expires RFC3339}; expired = skipped + one Warn at
load; overrides ADD allows ONLY (never deny, never widen memory/network
ceilings); file gitignore-able; CLI `meept groups override add|list|prune`.
Clock injection REQUIRED for tests (no time.Now in the reader's decision path
- accept a now func).

## Verified Anchors

- File loading precedent: $MEEPT_HOME JSON5 load (internal/config home.go
  MeeptPath resolution; users.json5 store load pattern in internal/auth).
- CLI subcommand registration: cmd/meept/ root + groups.go from leaf 06
  (coordinate: register as `meept groups override` subcommand under 06's
  command tree; if 06 not yet REVIEWED, add to root and note the move).
- Resolver seam: internal/groups opts from 04 T5.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state |
|---------|-----------|-------|----------------|------------|------------|
| T1 | Override types + reader (parse, expiry filter with injected clock, Warn on expired, malformed entry = named error not silent skip) | internal/groups/overrides.go, +test | `go test -p 2 ./internal/groups/ -run TestOverrides` | absent | expired skipped+Warn; malformed errors; add-only semantics pinned (deny impossible by type shape) |
| T2 | Wire reader into resolution at loop creation (04's seam; refresh on config reload) | internal/agent/registry.go or daemon wiring site, +test | `go test -p 2 ./internal/agent/ -run TestOverrideSeam` | seam unfilled | override allow rescues denied tool mid-window; expires => no effect |
| T3 | CLI: `meept groups override add <subject> --allow t1,t2 --reason r --expires RFC3339 | list [--all] | prune` | cmd/meept/ (groups override), +test | `go build -o bin/meept ./cmd/meept` | subcommand absent | add writes file atomically (tmp+rename); list shows active/expired; prune removes expired |
| T4 | groups check integration: report active + expired override counts (coordinate with 06's service - call its interface if REVIEWED, else expose the counts for it) | service/cli test | `go test -p 2 ./internal/services/ -run TestGroups` (if present) | n/a | counts reported |

## Self-Verification Checklist

- [ ] No time.Now in decision path (injected clock; tests prove expiry)
- [ ] Overrides cannot express deny or scope/class widening (type-level)
- [ ] Atomic file writes; no ignored errors
- [ ] Gate tests GREEN and unmodified; gofmt clean

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched: [paths]
2. Verify output: [last 20 lines each]
3. Coordination notes: [06/04 interplay status]

## Review Checklist (For Review Agent)

- [ ] Add-only enforced structurally, not by convention
- [ ] Expiry proven by test with injected clock (RED-verified by mutation if needed)
- [ ] Atomic write + prune safe under concurrent readers
