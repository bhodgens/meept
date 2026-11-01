# Codemode E2E Suite Finalization - Implementation Leaf

> **For the implementing agent:** Extend/finish the gate e2e suite to GREEN.
> Do NOT commit. Do NOT weaken gate cases - fix implementation or report.

## Meta

- **Parent:** ./orchestrator.md
- **Scope:** e2e/suites/codemode GREEN + token-savings smoke measurement
- **Dependencies:** 02-codemode-tool COMPLETE
- **Estimated Context:** 60K
- **Concurrency Group:** solo

## Goal

All five pinned gate cases pass hermetically; add the nested-call context
measurement (deterministic, no LLM): 5 scripted file_read calls via codemode
produce exactly ONE tool result in the recorded outbound request; the same 5
calls as direct tool calls produce FIVE. Record byte counts both ways in the
completion report (evidence for the master Integration Test Plan).

## Context

- Gate suite: e2e/suites/codemode/ (leaf 00T authored it)
- Harness payload capture: e2e/harness (how outbound LLM requests are recorded -
  follow existing suites that assert on request payloads)

## Verified Anchors

- e2e/suites/codemode/codemode_test.go - the five pinned cases
- e2e/manifest.json - path_map entry (already added by gate leaf)

## Interface Contracts (From Parent)

### What This Leaf Exposes

`make e2e-fast-area AREA=codemode` GREEN; measurement table in the completion
report (context bytes: codemode path vs direct path).

### What This Leaf Consumes

- codemode tool + gate (leaf 02)

## Tasks

| Task ID | Objective | Files | Verify command | RED reason | PASS state | Given/When/Then |
|---------|-----------|-------|----------------|------------|------------|-----------------|
| T1 | Gate cases GREEN | Modify (only if harness glue needed): e2e/suites/codemode/*.go | `make e2e-fast-area AREA=codemode` | suite RED until impl complete | all five pass | per gate GWTs |
| T2 | Nested-call measurement case | Create: e2e/suites/codemode/measure_test.go | `make e2e-fast-area AREA=codemode` | RED | asserts 1-vs-5 tool results; prints byte counts | Given a fixed 5-read script; When run; Then outbound request holds exactly one codemode result |
| T3 | Flakiness check | none | `make e2e-fast-area AREA=codemode` twice + `go test -p 2 -tags e2e -count=3 ./e2e/suites/codemode/` | n/a | stable | Given repeated runs; When executed; Then identical verdicts |

## Self-Verification Checklist

- [ ] No sleeps-as-sync; hermetic; scratch-bin hygiene respected (no raw os.MkdirTemp "meept-e2e-bin")
- [ ] Gate cases unmodified (diff shows additions only)
- [ ] Byte counts recorded with the measurement method named

**DO NOT COMMIT.**

## Completion Report Format

1. Files touched; 2. suite output tails; 3. measurement table; 4. deviations.

## Review Checklist (For Review Agent)

- [ ] Five gate cases + measurement GREEN; -count=3 stable
- [ ] Manifest path_map correct (e2e-affected picks the suite up on internal/codemode changes)
- [ ] Measurement methodology deterministic (fixed script, no model variance)
