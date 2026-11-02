# Remove Dead Scheduler Block - Implementation Leaf

> **For the implementing agent:** Implement ALL tasks below. Do NOT commit
> - the orchestrator handles all git operations after review. Do NOT use
> read_file on existing source files - explore with search_files or
> terminal cat. After writing a file, do NOT read it back. Prefix every go
> command with GOTOOLCHAIN=go1.26.5; run from /Users/caimlas/git/meept.

## Meta

- **Parent:** ../master.md
- **Scope:** Delete the dead early scheduler-Register block inside
  registerBuiltinTools (internal/daemon/components.go; locate by symbol -
  line numbers have drifted). Its `sched` parameter is nil at the only
  call site (NewComponents calls registerBuiltinTools long before
  c.Scheduler is assigned in the deferred block), so the
  `if sched != nil` guard never fires and the eight direct Register calls
  inside are unreachable.
- **Dependencies:** none
- **Estimated Context:** 40K
- **Concurrency Group:** A
- **Audit references:** tool-provider-seam 03-migrate observation

## Goal

One scheduler registration path: the deferred provider block (which calls
builtin.SetSchedulerProvider + toolprov.RegisterAll). The dead direct
block goes away; the sched parameter it consumed goes with it if no other
use remains.

## Context

- internal/daemon/components.go - registerBuiltinTools signature includes
  a sched param; grep `sched != nil` inside it for the dead block; grep
  `c.Scheduler = sched` for the deferred provider block (the live path).
- Call site: grep `registerBuiltinTools(c.ToolRegistry` - ONE call site;
  c.Scheduler is nil there (assigned ~1100 lines later).
- SHARED FILE: components.go carries active sibling WIP. Make ONE surgical
  deletion; re-read the block with terminal cat immediately before
  patching; never write the whole file.

## Verified Anchors

- internal/daemon/components.go - `func registerBuiltinTools(` (grep for
  the current line; signature includes sched)
- internal/daemon/components.go - the dead block: grep
  `// Scheduler tools (only if scheduler is available)` or
  `if sched != nil` inside registerBuiltinTools
- internal/daemon/components.go - the live deferred block: grep
  `SetSchedulerProvider` (only one, in the deferred section near
  `c.Scheduler = sched`)

## Interface Contracts (From Parent)

### What This Leaf Exposes

registerBuiltinTools loses its sched parameter IF the dead block was its
only use (verify: grep `sched` within the function body after deleting the
block - if other uses exist, keep the param and delete only the block).
Call site updated to match.

### What This Leaf Consumes

- The scheduler provider behavior from tool-provider-seam 03 (unchanged).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Delete the dead block + unused param | Modify: internal/daemon/components.go | `GOTOOLCHAIN=go1.26.5 go build ./internal/daemon/ && go test -p 2 -count=1 ./internal/daemon/ -run 'TestProviderWiring|TestScheduler' 2>&1 \| tail -2` | n/a (deletion verified by build + the provider test proving tools still register) | Build green; deferred provider block intact; schedule tools still arrive via provider (the F7-adjacent wiring test stays green) | Given the deletion; When a daemon Components test builds and the scheduler provider test runs; Then green with no direct scheduler Register in registerBuiltinTools |

Post-deletion proof: `grep -c 'NewScheduleCreateTool' internal/daemon/components.go`
must be exactly 1 (the deferred provider block only).

## Self-Verification Checklist

- [ ] Only the dead block removed; deferred provider block untouched
- [ ] sched parameter removed only if truly unused after the deletion
- [ ] Build green; daemon provider tests green
- [ ] gofmt clean; ASCII-only added lines (deletion = mostly removed lines)

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched
2. Verify output (last 20 lines)
3. Deviations

## Review Checklist (For Review Agent)

- [ ] The deleted block is verifiably the nil-param one (cite the nil
      assignment-order evidence: call site line < assignment line)
- [ ] Live provider block untouched
- [ ] No other scheduler paths affected

Output: APPROVED or specific gaps.

## Notes

- If the sibling has moved the block again, locate by symbol and re-verify
  the nil-order evidence before deleting. If c.Scheduler is assigned
  BEFORE the registerBuiltinTools call site now (a sibling changed the
  order), STOP and report - the block may be live and the leaf is
  SUPERSEDED.
