# Doctor Memory Pipeline Check - Implementation Leaf

> **For the implementing agent:** You are the implementer for this leaf.
> Implement ALL tasks below using TDD. The parent gate tests (00T) ALREADY
> EXIST - your job includes making the F7 assertion GREEN. Do NOT delete,
> skip, weaken, or rewrite any gate test. Do NOT commit. Do NOT use read_file
> on existing source files. Write once and stop.

## Meta

- **Parent:** ../master.md
- **Scope:** Add a memory-pipeline check to `meept doctor` reporting l0 capture, distill queue depth/dead-letters, recall telemetry, and knowledge/bridges arm state.
- **Dependencies:** 02-defaults-on.md (reports the new defaults)
- **Estimated Context:** 45K
- **Concurrency Group:** D
- **Audit references:** F7 of master Test Gate; R7

## Goal

Operators cannot see whether the (now default-on) memory pipeline actually
works on their install. `meept doctor` gains one check block, report-only
(user normative rule: doctor NEVER installs or mutates):

- `memory-capture`: l0_enabled + latest l0 file date + line count
- `memory-distill`: pending l1/l2 job counts + dead-letter count (from queue
  stats), model-slot resolvability (memory_model -> fallback chain)
- `memory-recall`: recall_outcomes.jsonl presence + last-row age (telemetry
  armed)
- `memory-extras`: knowledge enabled + page count; bridges enabled

Warn (not fail) on: jobs dead-lettered, model unresolvable, capture armed but
no l0 file written today while turns ran.

## Context

- Doctor plumbing: cmd/meept/doctor.go (doctorCheck struct, runDoctor check
  list, printDoctorChecks) - report-only, doctor --fix only runs opt-in
  install hints (unchanged).
- Queue stats surface: queue.stats RPC / meept queue surfaces - doctor reads
  the SAME store directly or via the stats API; prefer the stats path to
  avoid a second store handle.
- User normative rule (memory): "meept UX: doctor report-only; opt-in
  installs" - this check NEVER writes.

## Verified Anchors

- cmd/meept/doctor.go:84 (runDoctor) - check list assembly.
- cmd/meept/doctor.go:33 (doctorCheck struct) - name/ok/warn/detail shape.
- internal/daemon/health.go:69 (RunHealthChecks) - daemon-side health check
  pattern (this leaf extends the CLI doctor, not the daemon health set -
  keep the surfaces separate unless trivial).

## Interface Contracts (From Parent)

### What This Leaf Exposes

```
// doctor checks named (exact names - the gate asserts "memory"):
//   memory-capture | memory-distill | memory-recall | memory-extras
// All report-only; warn-level findings use doctorCheck.warn=true.
```

### What This Leaf Consumes

- Existing doctorCheck plumbing; queue stats surface; memory data-dir
  resolution (config.MeeptPath family).

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|---------|----------------|------------|------------|-----------------|
| T1 | memory-capture + memory-recall checks | Modify: cmd/meept/doctor.go (+ cmd/meept/doctor_memory.go if split) | `go test -p 2 ./cmd/meept/ -run TestDoctor` (existing doctor tests + new pins) | checks undefined | on a scratch MEEPT_HOME with l0 data: dates/counts reported; with outcomes file: last-row age | Given l0 dir with yesterday+today files; When doctor runs; Then latest=today, line count correct |
| T2 | memory-distill check via queue stats | Modify: same | same | no queue read | pending/dead-letter counts render; unresolvable memory_model -> warn detail | Given queue with 2 pending, 1 dead l1 job; When doctor; Then counts shown, dead-letter warns |
| T3 | memory-extras + warn semantics | Modify: same | same | - | knowledge page count; bridges state; everything report-only | Given knowledge enabled with 5 pages; When doctor; Then count 5 shown, no mutation (verify mtimes unchanged) |
| T4 | Gate F7 (suite assertion) | verify only | `go test -p 2 ./e2e/suites/memory-defaults/` | F7 RED before this leaf | F7 GREEN: doctor output contains memory check on default boot | Given default scratch boot; When meept doctor; Then memory checks present |

## Self-Verification Checklist

- [ ] All four checks present; report-only (no writes anywhere in the path)
- [ ] Warn (not fail) semantics for degraded pipeline states
- [ ] Works on models-less hosts (model unresolvable = warn, not error)
- [ ] Existing doctor checks untouched and green
- [ ] gofmt clean; no scope creep

**DO NOT COMMIT.** The orchestrator handles all git operations after review.

## Completion Report Format

1. **Files touched**
2. **Verify command + output** [doctor run on scratch home, full output]
3. **Deviations:** [none / list]

## Review Checklist (For Review Agent)

- [ ] Report-only verified (no install/mutation in the memory path)
- [ ] Check names exact per contract
- [ ] No second store handle opened (stats path reused)
- [ ] No debug artifacts

Output: APPROVED or specific gaps.

## Notes

- Daemon-side health (internal/daemon/health.go RunHealthChecks) is a
  DIFFERENT surface - do not extend it in this leaf; a bus-level runtime
  health event already exists for hang detection.
- gendoc: doctor flags unchanged; no docs-generate needed unless help text
  changes (it will - update `meept doctor` help line + docs/reference if the
  reference page lists checks).
