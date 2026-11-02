# BlockedPaths Engine Wiring - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below using TDD. Do
> NOT commit - the orchestrator handles all git operations after review. Do
> NOT use read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back - write once and
> stop. Prefix every go command with GOTOOLCHAIN=go1.26.5; run from
> /Users/caimlas/git/meept.

## Meta

- **Parent:** ../master.md
- **Scope:** Make SecurityConfig.BlockedPaths deny file-category tool calls
  in the security engine. Today the field is validated at config load
  (schema.go:2655 rejects empty strings) and seeded with defaults
  (~/.ssh/*, ~/.gnupg/*, ~/.meept/meept.toml) but NO production code reads
  it.
- **Dependencies:** none
- **Estimated Context:** 60K
- **Concurrency Group:** A
- **Audit references:** tool-provider-seam tracking table follow-up

## Goal

A file-category tool call whose target path matches a BlockedPaths entry
returns Decision{Allowed:false} naming the rule. Matching mirrors the
AllowedPaths logic inverted (same matcher, deny instead of allow).

## Context

- internal/security/engine.go - NewEngine(dbPath, cfg, logger); the Check
  pipeline's file stage already handles path checks for
  ActionFileRead/Write/Delete (find the stage that consults allowed paths
  - grep AllowedPaths in internal/security/).
- internal/config/schema.go:2479 - SecurityConfig.BlockedPaths []string.
- internal/security/seed_rules.go - where defaults land.
- The gate (internal/daemon/toolgate.go) routes file categories to
  action "file_write"/"file_read" through Engine.Check - so wiring here
  covers the live path automatically.

## Verified Anchors

- internal/config/schema.go:2479 - BlockedPaths field
- internal/security/engine.go:62 - NewEngine signature
- internal/security/engine.go:494 - CheckForAgent (the pipeline); the
  file-path stage is inside (grep AllowedPaths / checkPaths nearby)

## Interface Contracts (From Parent)

### What This Leaf Exposes

Engine behavior only - no new exported symbols required. If the existing
path-check stage takes the allowed list from engine config, extend that
stage to consult BlockedPaths after (deny wins over allow).

### What This Leaf Consumes

- C1 (master.md): deny wins; Reason names the rule; RiskLevel high.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Engine consults BlockedPaths | Modify: internal/security/engine.go (path-check stage + NewEngine config capture); Test: internal/security/engine_blocked_paths_test.go | `GOTOOLCHAIN=go1.26.5 go test -count=1 ./internal/security/ -run TestBlockedPaths` | FAIL: denied call succeeds (field unread) | Match => Allowed=false, Reason names blocked-path rule; non-match unaffected; AllowedPaths-allowed + BlockedPaths-matched => DENIED (deny wins) | Given cfg.BlockedPaths ["~/.ssh/*"]; When Check(file_write, path ~/.ssh/id_rsa); Then Allowed=false with the rule named |
| T2 | Glob + exact + ~ expansion parity with AllowedPaths | Same test file | same command, full run | FAIL on missing cases | exact path, glob, and ~-expanded home paths all deny; case documented | Given entries of each shape; When matched paths are checked; Then each denies |

TDD: write the RED tests first (they compile against the existing Check
signature), observe RED, implement, observe GREEN. Also run the FULL
internal/security suite - existing allowed-path tests must stay green.

## Self-Verification Checklist

- [ ] RED observed before implementation; GREEN after
- [ ] Full internal/security suite green (no regression to AllowedPaths)
- [ ] Deny wins over allow (ordering pinned by a test)
- [ ] gofmt clean; ASCII-only added lines
- [ ] No scope creep

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify output (last 20 lines)
3. Deviations

## Review Checklist (For Review Agent)

- [ ] RED was real (tests fail on the unread field, not compile errors)
- [ ] Deny-wins ordering pinned
- [ ] Existing allowed-path behavior byte-identical
- [ ] No gate-test modifications

Output: APPROVED or specific gaps.

## Notes

- If the path stage reads AllowedPaths from a stored config snapshot,
  BlockedPaths rides the same snapshot - do not add a new config plumb.
- The seeded defaults already exist in schema.go; do not duplicate them in
  the engine.
