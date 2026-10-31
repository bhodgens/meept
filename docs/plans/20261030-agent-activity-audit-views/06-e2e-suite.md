# E2E Suite: activity-views - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. Do NOT commit - the orchestrator
> handles all git operations after review. Do NOT use read_file on existing
> source files - explore with search_files or terminal cat. After writing a
> file, do NOT read it back - write once and stop. Report what you built,
> files touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** A hermetic e2e suite proving the activity surface end to end on
  a scratch daemon: seeded stores -> RPC activity.query/recent/overview/
  findings/agents.review with correct sorting, live ring, drift.
- **Dependencies:** 01-aggregator.md, 02-rpc.md (both REVIEWED before dispatch)
- **Estimated Context:** 75K
- **Concurrency Group:** D (dispatch after 02 reaches REVIEWED)

## Goal

Unit tests prove components; this suite proves the WIRING: a real
meept-daemon boot with the aggregator + ring + RPC closures registered, real
seeded sqlite stores on disk, and real RPC calls returning correctly sorted,
filtered, live data. This is the live-gate evidence for the whole tree.

## Context

Meept's e2e tier is hermetic: suites under e2e/suites/<name>/ with the
`e2e` build tag, a harness that builds the daemon into a scratch dir, boots
it with a scratch MEEPT_HOME, and talks to it over its real RPC socket. The
manifest (e2e/manifest.json) needs a `suites[]` entry AND `path_map`
entries for every newly touched package dir - the fail-closed pre-commit
gate blocks edits to unmapped dirs. A stores-on-disk seeding pattern exists
(harness pre-boot hook opens the REAL store at the REAL path, writes
fixtures, closes before boot). Use the scratch-home rules: models-less boot
needs orchestrator.classifier_boot_fail_fast=false and a remapped
transport.http.addr; assert config arming via the boot log, not file location.

## Verified Anchors

Anchors grep-confirmed 2026-10-30. Re-grep before editing.

- e2e/suites/audit-chain/audit-chain_test.go - closest template: sandbox sqlite store, real writes, tamper detection.
- e2e/manifest.json - suites[] entries {name, dir, status:"implemented"} + path_map (e.g. "internal/rpc/": [...]); add "internal/activity/" mapping.
- e2e/harness/ - daemon build/boot helpers (scratch bin dir, MEEPT_HOME).
- internal/daemon/verdict_audit.go:83 - audit_chain.db path (memory data dir) - seed target for chain records.
- internal/task/step.go:419-465 + internal/task/store.go:79-100 - tasks.db schema for fixtures.
- internal/employee/enforcement.go:1657-1676 - employee_audit_findings schema for fixtures.
- meept skill note: scratch MEEPT_HOME at the HOME ROOT; classifier_boot_fail_fast=false; remapped ports.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// e2e/suites/activity-views/activity-views_test.go (//go:build e2e)
// Assertions over the REAL RPC surface (method names from C2/C4):
//   activity.query sort=chrono|task|plan -> expected orderings
//   activity.recent -> reflects seeded/produced bus events
//   activity.overview -> drift_score present per seeded employee
//   activity.findings ?severity= filters correctly
//   agents.review -> drift in [0,1]
// Plus manifest entries; no exported Go symbols.
```

### What This Leaf Consumes

- C2/C4 RPC shapes (leaf 02); C1 records (leaf 01); C3 ring (leaf 05)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Suite skeleton + manifest entries + scratch-daemon boot with seeded stores | Create: e2e/suites/activity-views/activity-views_test.go; Modify: e2e/manifest.json (suites[] + path_map for internal/activity/) | `go test -p 2 -tags e2e ./e2e/suites/activity-views/` | suite dir absent / methods unregistered | daemon boots (classifier gate disabled, ports remapped), fixtures seeded via pre-boot hook, RPC responds to activity.query with empty result | Given scratch home with seeded tasks.db; When boot + activity.query limit=1; Then 1 record |
| T2 | Sort-mode assertions (chrono/task/plan) + filters over the wire | Modify: e2e/suites/activity-views/activity-views_test.go | `go test -p 2 -tags e2e ./e2e/suites/activity-views/` | sort cases fail | plan sort matches the inverted linkage (plan->tasks->steps); agent/kind filters hold | Given 3 tasks under 2 plans; When activity.query sort=plan; Then groups ordered by PlanID with per-group time order |
| T3 | Live-ring + overview + findings + review assertions | Modify: e2e/suites/activity-views/activity-views_test.go | `go test -p 2 -tags e2e ./e2e/suites/activity-views/ && make e2e-fast-area AREA=activity-views` | recent/review cases fail | driving one real chat turn (local models only; models-less boot: seed a synthetic bus event instead) populates activity.recent; overview carries drift; severity filter works | Given seeded finding severity=critical; When activity.findings severity=critical; Then only that finding returns |

## Self-Verification Checklist

- [ ] All tasks implemented and tests passing
- [ ] Interface contracts satisfied exactly
- [ ] All files at exact specified paths
- [ ] No deviations (or deviations documented in the report)
- [ ] No scope creep - only what the tasks specify
- [ ] Suite is hermetic: scratch MEEPT_HOME, no cloud endpoints, remapped ports
- [ ] Manifest path_map covers internal/activity/ (pre-commit gate stays green)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [verify commands; last 20 lines of output]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Every task row implemented; suite green via make e2e-fast-area
- [ ] Manifest entries present (suites[] + path_map) in same change
- [ ] Fixtures seeded through the REAL store paths the boot resolves
- [ ] No cloud calls; classifier boot gate disabled in scratch config
- [ ] No scope creep; no debug artifacts or line-number corruption

Output: APPROVED or specific gaps with file + line references.

## Notes

- If a live chat turn is impractical in the models-less scratch rig, seed
  bus events synthetically through the queue/task APIs and say so - the
  suite must still prove ring wiring through a REAL daemon process.
- The suite runs in CI (make e2e-fast); keep boot time bounded and skip
  nothing silently - a skip must assert its reason.
- Under full parallel-suite load, fixed sleeps are a defect class: poll the
  RPC endpoint with a deadline instead of sleeping.
