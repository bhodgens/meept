# 06-wiring-delivery/02-e2e-suite

## DISPATCH INSTRUCTION
Do NOT commit. Do NOT run git add.

## Parent and Dependencies
Parent: 06-wiring-delivery
Dependencies: all (master.md)

## Scope
Implement and verify the hermetic e2e test suite for the Kanban board, covering all critical integration scenarios.

## Estimated Context
85K

## Interface Contract
(See master.md Integration Test Plan - Cases (a)-(f))
- (a) Rebuild equality
- (b) Lease refusal (ErrLeaseHeld)
- (c) Takeover audit sequence
- (d) WIP counter accuracy
- (e) Sidecar auth/bind failure
- (f) Parity snapshot (GUI)

## Tasks
- [ ] Add the kanban suite entry (path->suite mapping) to the repo-root `e2e/manifest.json`, same commit as the new suite (pre-commit gate [18]).
- [ ] Implement `e2e/suites/kanban/test_suite.go`: Implement test logic for cases (a)-(f).
- [ ] Implement `e2e/suites/kanban/test_suite_test.go`: Use `e2e` build tag to execute the suite.
- [ ] Run `make e2e-fast` to verify all cases pass in the hermetic environment.

## Self-Verification Checklist
- [ ] No `os.Getwd()` used.
- [ ] No new `internal/` files (e2e logic only in `e2e/`).
- [ ] Build tag `e2e` applied to all new test files.
- [ ] `e2e/manifest.json` updated for the new suite.

## Review Checklist
- [ ] No leaf owns >3 changed files
- [ ] Contract IDs C1-C10 used verbatim
- [ ] `make graphs` re-run after topic/RPC/WS change
- [ ] `e2e/manifest.json` updated for new suites
- [ ] `AGENTS.md` package table updated
