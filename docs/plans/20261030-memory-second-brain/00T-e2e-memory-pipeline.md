# Memory Pipeline Test Gate - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below. Tests ONLY - the features they cover DO NOT
> EXIST YET. Every test must fail RED for a missing-implementation reason,
> never for a syntax/config error. Do NOT commit - the orchestrator handles
> all git operations after review. Do NOT use read_file on existing source
> files - explore with search_files or terminal cat. After writing a file,
> do NOT read it back - write once and stop. Report what you built, files
> touched, and any deviations.

## Meta

- **Parent:** ../master.md
- **Scope:** Hermetic e2e suites proving default-on memory pipeline, scoping isolation, stable/volatile recall split, recall telemetry, and doctor visibility - before any implementation lands.
- **Dependencies:** none (this gate runs first)
- **Estimated Context:** 70K
- **Concurrency Group:** T
- **Audit references:** F1-F5, F7 of master Test Gate matrix

## Goal

Write the RED e2e suites this tree's implementation leaves must turn GREEN.
Per repo policy, ALL new feature tests are hermetic e2e suites; there are no
new unit test files for features. The suites boot a scratch daemon (scratch
MEEPT_HOME, remapped ports, local models only) and assert through live
surfaces (files on disk, RPC responses, CLI output), never store internals.

Suites that depend on not-yet-existing config keys or file shapes compile and
run; their assertions fail RED with clear "not implemented" messages.

## Context

- e2e/suites/<name>/ - suite layout, `e2e` build tag; e2e/manifest.json -
  path_map + suite registry (root AGENTS.md; docs/workflows/e2e-testing.md).
- e2e/harness/ - scratch daemon harness: WithPreBootHook opens the REAL
  on-disk store at the boot wiring's exact path; Stack.HealthOK is the
  readiness poll pattern (never fixed sleeps).
- internal/daemon/memory_capture.go - the turn.terminal capture subscriber
  (WireMemoryCapture) the memory-defaults suite exercises.
- config defaults today are FALSE for every memory knob
  (internal/config/schema.go:1511-1655) - after leaf 02 they are TRUE; the
  defaults suite asserts the TRUE world.
- L0 files land at <memory.data_dir>/l0/YYYY-MM-DD.jsonl; recall telemetry
  (leaf 04) adds <memory.data_dir>/recall_outcomes.jsonl (shape in contract
  C5 of master.md).

## Verified Anchors

- e2e/manifest.json - suite registry to extend (path_map entries REQUIRED in
  the same commit or the pre-commit e2e hook blocks the next edit).
- e2e/harness/daemon.go - scratch-daemon spawn + watchdog (reuse, do not
  duplicate).
- internal/daemon/memory_capture.go:21 (memoryCapturePage) - capture
  subscriber lives here; default-on makes it record every completed turn.
- cmd/meept/doctor.go:84 (runDoctor) - doctor check list the doctor suite
  exercises via `meept doctor` output.

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
e2e/suites/memory-defaults/  - suite proving F1, F4, F5, F7
e2e/suites/memory-scoping/   - suite proving F2, F3 (gates branch 01)
+ manifest.json path_map + suite entries for both
```

### What This Leaf Consumes

- C4 (defaults) - the TRUE-default world the memory-defaults suite asserts.
- C1/C2/C3 (scope) - shapes the memory-scoping suite's plant/recall calls.
- C5 (recall_outcomes.jsonl row shape) - verbatim JSON keys asserted by F5.
- C6 (stable section title "Memory (standing)") - asserted by F4.

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | memory-defaults suite skeleton + manifest entry | Create: e2e/suites/memory-defaults/main_test.go; Modify: e2e/manifest.json | `go test -tags e2e ./e2e/suites/memory-defaults/` | suite fails: memory knobs are false by default today | suite boots scratch daemon with DEFAULT config and all assertions hold | Given default config; When daemon boots and one chat turn completes; Then l0 file gained lines, l1 job enqueued, knowledge tool in tool list |
| T2 | F4/F5 assertions in memory-defaults | Modify: e2e/suites/memory-defaults/main_test.go | same as T1 | fails: no stable-section render; no recall_outcomes.jsonl | stable item renders under "Memory (standing)"; recall_outcomes.jsonl row has all C5 keys after a turn whose reply cites an injected memory | Given a stored stable memory and a volatile one; When a turn runs; Then prompt sections split correctly AND outcomes row written |
| T3 | memory-scoping suite: isolation | Create: e2e/suites/memory-scoping/main_test.go; Modify: e2e/manifest.json | `go test -tags e2e ./e2e/suites/memory-scoping/` | fails: WithScopeFilter/C1 Scope do not exist | project A fact not recalled in project B session; global-scope fact recalled in both | Given fact stored in project A session; When project B session recalls; Then A-fact absent, global fact present |
| T4 | memory-scoping suite: chief-of-staff | Modify: e2e/suites/memory-scoping/main_test.go | same as T3 | fails: MemoryMaxScope field does not exist | agent with memory_max_scope=global recalls project A fact; default (project) agent does not | Given two agents over projects A+B; When each recalls; Then only the global-max agent sees A-fact |
| T5 | doctor assertion | Modify: e2e/suites/memory-defaults/main_test.go | `go test -tags e2e ./e2e/suites/memory-defaults/` | fails: doctor has no memory check today | `meept doctor` output contains a memory-pipeline check naming l0/distill status | Given default boot; When doctor runs; Then memory check present with armed detail |

Worked example for T1 (canonical expansion):
1. Write the suite test that boots the scratch daemon with default config and
   asserts the l0 file exists after one turn. Run:
   `go test -tags e2e ./e2e/suites/memory-defaults/`
   Expected: FAIL - l0 file absent (l0_enabled=false by default today).
2. Do NOT implement the default flip. Stop at RED with the expected reason.

## Self-Verification Checklist

- [ ] All tasks implemented; suites compile under the `e2e` build tag
- [ ] Every suite fails RED for a missing-implementation reason (capture the
      failure output in the report)
- [ ] manifest.json entries land in the same change (path_map per dir)
- [ ] Interface contracts satisfied exactly (C5 JSON keys, C6 section title)
- [ ] No fixed sleeps - bounded polls on real readiness (Stack.HealthOK)
- [ ] All files at exact specified paths; no scope creep
- [ ] No deviations (or deviations documented in the report)

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched:** [every created/modified path]
2. **Verify command + output:** [RED failure output per suite, last 20 lines]
3. **Deviations:** [none / list with rationale]

## Review Checklist (For Review Agent)

- [ ] Suites fail RED for the RIGHT reasons (not compile errors, not port
      collisions, not config-parse failures)
- [ ] Contracts C1-C6 shapes asserted verbatim
- [ ] manifest entries complete; harness reuse not duplication
- [ ] No fixed sleeps; no debug artifacts; no scope creep

Output: APPROVED or specific gaps with file + line references.

## Notes

- The scratch daemon needs orchestrator.classifier_boot_fail_fast=false and a
  remapped transport.http.addr when the host has a live daemon (repo-proven
  scratch-boot recipe in the meept-development skill).
- The memory-scoping suite gates branch 01's implementation, but branch 01's
  own unit-level pins live in 01-scoping/00T-tests.md - this gate covers only
  the live e2e surface.
- Default-on boot on a models-less host: the harness pattern already sets
  classifier_boot_fail_fast=false; do the same here or the F-D8 gate kills
  the scratch daemon seconds in.
